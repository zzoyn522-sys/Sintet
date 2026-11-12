#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <winrt/Windows.Foundation.h>
#include <winrt/Windows.Foundation.Collections.h>
#include <winrt/Windows.Graphics.Imaging.h>
#include <winrt/Windows.Media.Control.h>
#include <winrt/Windows.Storage.Streams.h>
#include <algorithm>
#include <cmath>
#include <cstring>
#include <mutex>
#include <optional>
#include "media.h"

namespace SintetMedia {
namespace {
using namespace winrt;
using namespace Windows::Media::Control;
using Windows::Foundation::AsyncStatus;
using Windows::Media::MediaPlaybackType;
using Session=GlobalSystemMediaTransportControlsSession;
using Manager=GlobalSystemMediaTransportControlsSessionManager;
using Playback=GlobalSystemMediaTransportControlsSessionPlaybackStatus;
using PlaybackInfo=GlobalSystemMediaTransportControlsSessionPlaybackInfo;
using Properties=GlobalSystemMediaTransportControlsSessionMediaProperties;

std::mutex mutex;
Track track;
struct Command {std::uint64_t generation=0;Action kind=Action::Toggle;double seconds=0;};
Command pending;
HANDLE stopEvent=nullptr, worker=nullptr;

// All WinRT calls live on this MTA thread. Slow or hung media providers never
// block Minecraft's rendering or input threads.
template<class T> std::optional<T> waitFor(Windows::Foundation::IAsyncOperation<T> operation,ULONGLONG deadline=GetTickCount64()+1800) {
 while(operation.Status()==AsyncStatus::Started){
  if(WaitForSingleObject(stopEvent,25)==WAIT_OBJECT_0||GetTickCount64()>=deadline){
   operation.Cancel();
   return std::nullopt;
  }
 }
 if(operation.Status()!=AsyncStatus::Completed)return std::nullopt;
 return operation.GetResults();
}

bool musicWithStatus(const PlaybackInfo& info,Playback status) {
 if(!info||info.PlaybackStatus()!=status)return false;
 const auto type=info.PlaybackType();
 return !type||type.Value()!=MediaPlaybackType::Video;
}

std::string singleLine(const hstring& value) {
 auto text=to_string(value);
 for(char& ch:text)if(static_cast<unsigned char>(ch)<32)ch=' ';
 return text;
}

std::shared_ptr<const Cover> readCover(const Properties& properties) {
 using namespace Windows::Graphics::Imaging;
 const auto reference=properties.Thumbnail();
 if(!reference)return {};
 const auto deadline=GetTickCount64()+850;
 const auto stream=waitFor(reference.OpenReadAsync(),deadline);
 if(!stream||!*stream||stream->Size()==0||stream->Size()>8*1024*1024)return {};
 const auto bitmap=waitFor(BitmapDecoder::CreateAsync(*stream),deadline);
 if(!bitmap||!*bitmap)return {};
 const auto width=bitmap->PixelWidth(),height=bitmap->PixelHeight();
 if(!width||!height||width>4096||height>4096)return {};
 const auto longEdge=std::max(width,height);
 const auto scaledWidth=std::max(1u,96*width/longEdge);
 const auto scaledHeight=std::max(1u,96*height/longEdge);
 BitmapTransform transform;
 transform.ScaledWidth(scaledWidth);
 transform.ScaledHeight(scaledHeight);
 transform.InterpolationMode(BitmapInterpolationMode::Fant);
 const auto provider=waitFor(bitmap->GetPixelDataAsync(BitmapPixelFormat::Rgba8,BitmapAlphaMode::Straight,
     transform,ExifOrientationMode::IgnoreExifOrientation,ColorManagementMode::ColorManageToSRgb),deadline);
 if(!provider||!*provider)return {};
 const auto data=provider->DetachPixelData();
 if(data.size()!=scaledWidth*scaledHeight*4)return {};
 return std::make_shared<Cover>(Cover{scaledWidth,scaledHeight,{data.begin(),data.end()}});
}

void update(const Manager& manager,Session& previous,std::uint64_t& generation,ULONGLONG& coverRetryAt) {
 const auto current=manager.GetCurrentSession();
 const auto currentInfo=current?current.GetPlaybackInfo():nullptr;
 Session session=nullptr, paused=nullptr;
 PlaybackInfo info=nullptr, pausedInfo=nullptr;
 const auto select=[&](const Session& candidate,const PlaybackInfo& candidateInfo){
  if(musicWithStatus(candidateInfo,Playback::Playing)){
   session=candidate;info=candidateInfo;
  }else if(!paused&&musicWithStatus(candidateInfo,Playback::Paused)){
   paused=candidate;pausedInfo=candidateInfo;
  }
 };
 if(musicWithStatus(currentInfo,Playback::Playing)){
  session=current;info=currentInfo;
 }else{
  if(previous&&previous!=current){
   try{select(previous,previous.GetPlaybackInfo());}catch(const hresult_error&){}
  }
  if(!session&&current)select(current,currentInfo);
 }
 if(!session){
  for(const auto& candidate:manager.GetSessions()){
   if(candidate==current||candidate==previous)continue;
   select(candidate,candidate.GetPlaybackInfo());
   if(session)break;
  }
 }
 if(!session&&paused){session=paused;info=pausedInfo;}

 Track next;
 Properties metadata=nullptr;
 bool play=false,pause=false;
 if(session){
  bool video=false;
  try{
   const auto properties=waitFor(session.TryGetMediaPropertiesAsync());
   if(properties&&*properties){
    const auto type=properties->PlaybackType();
    video=type&&type.Value()==MediaPlaybackType::Video;
    if(!video){
     metadata=*properties;
     next.title=singleLine(properties->Title());
     next.artist=singleLine(properties->Artist());
    }
   }
  }catch(const hresult_error&){}
  if(next.title.empty()&&!video&&session==previous&&musicWithStatus(info,Playback::Paused)){
   std::lock_guard<std::mutex> lock(mutex);
   next.title=track.title;
   next.artist=track.artist;
  }
  if(!next.title.empty()){
   next.playing=info.PlaybackStatus()==Playback::Playing;
   const auto controls=info.Controls();
   play=controls.IsPlayEnabled();pause=controls.IsPauseEnabled();
   next.canPrevious=controls.IsPreviousEnabled();next.canNext=controls.IsNextEnabled();
   next.canToggle=controls.IsPlayPauseToggleEnabled()||(next.playing?pause:play);
   const auto timeline=session.GetTimelineProperties();
   next.start=std::chrono::duration<double>(timeline.StartTime()).count();
   next.end=std::chrono::duration<double>(timeline.EndTime()).count();
   next.seekMin=std::max(next.start,std::chrono::duration<double>(timeline.MinSeekTime()).count());
   next.seekMax=std::min(next.end,std::chrono::duration<double>(timeline.MaxSeekTime()).count());
   next.canSeek=controls.IsPlaybackPositionEnabled()&&next.seekMax>next.seekMin;
   next.position=std::chrono::duration<double>(timeline.Position()).count();
   next.measuredAt=GetTickCount64();
   if(next.playing){
    const auto rate=info.PlaybackRate();
    next.rate=rate&&std::isfinite(rate.Value())?rate.Value():1;
    const double elapsed=std::chrono::duration<double>(winrt::clock::now()-timeline.LastUpdatedTime()).count();
    next.position+=std::max(elapsed,0.0)*next.rate;
   }
  }
 }

 Command action;
 {
  std::lock_guard<std::mutex> lock(mutex);
  if(session!=previous||track.title!=next.title||track.artist!=next.artist){++generation;coverRetryAt=0;}
  else next.cover=track.cover;
  next.generation=generation;
  track=next;
  action=pending;
  pending={};
 }
 previous=next.title.empty()?nullptr:session;
 if(!previous)return;
 if(action.generation==generation){
  switch(action.kind){
  case Action::Previous:
   if(next.canPrevious)waitFor(session.TrySkipPreviousAsync());
   break;
  case Action::Next:
   if(next.canNext)waitFor(session.TrySkipNextAsync());
   break;
  case Action::Seek:
   if(next.canSeek){
    const auto seconds=std::clamp(action.seconds,next.seekMin,next.seekMax);
    const auto ticks=std::chrono::duration_cast<Windows::Foundation::TimeSpan>(std::chrono::duration<double>(seconds)).count();
    waitFor(session.TryChangePlaybackPositionAsync(ticks));
   }
   break;
  case Action::Toggle:
   if(!next.canToggle)break;
   if(next.playing&&pause)waitFor(session.TryPauseAsync());
   else if(!next.playing&&play)waitFor(session.TryPlayAsync());
   else waitFor(session.TryTogglePlayPauseAsync());
   break;
  }
  return;
 }
 if(!next.cover&&GetTickCount64()>=coverRetryAt){
  coverRetryAt=GetTickCount64()+3000;
  try{
   if(auto cover=readCover(metadata)){
    std::lock_guard<std::mutex> lock(mutex);
    track.cover=std::move(cover);
   }
  }catch(const hresult_error&){/* An unavailable cover leaves the music icon in place. */}
 }
}

DWORD WINAPI run(void*) {
 try{init_apartment(apartment_type::multi_threaded);}catch(...){return 0;}
 {
  Manager manager=nullptr;
  Session previous=nullptr;
  std::uint64_t generation=0;
  ULONGLONG coverRetryAt=0;
  while(WaitForSingleObject(stopEvent,0)==WAIT_TIMEOUT){
   try{
    if(!manager){if(auto requested=waitFor(Manager::RequestAsync()))manager=*requested;}
    if(manager)update(manager,previous,generation,coverRetryAt);
   }catch(...){
    std::lock_guard<std::mutex> lock(mutex);
    track={};pending={};previous=nullptr;manager=nullptr;
   }
   if(WaitForSingleObject(stopEvent,manager?450:2500)!=WAIT_TIMEOUT)break;
  }
 }
 uninit_apartment();
 return 0;
}
}

void Start(){
 if(worker)return;
 stopEvent=CreateEventW(nullptr,TRUE,FALSE,nullptr);
 if(!stopEvent)return;
 worker=CreateThread(nullptr,0,run,nullptr,0,nullptr);
 if(!worker){CloseHandle(stopEvent);stopEvent=nullptr;}
}

void Stop(){
 if(!worker)return;
 SetEvent(stopEvent);
 if(WaitForSingleObject(worker,2200)!=WAIT_OBJECT_0)return;
 CloseHandle(worker);CloseHandle(stopEvent);
 worker=nullptr;stopEvent=nullptr;
 std::lock_guard<std::mutex> lock(mutex);
 track={};pending={};
}

Track Snapshot(){std::lock_guard<std::mutex> lock(mutex);return track;}
void Control(std::uint64_t generation,Action action,double seconds){std::lock_guard<std::mutex> lock(mutex);pending={generation,action,seconds};}
}

struct SintetPresenceSample {
 std::uint64_t generation;
 std::uint32_t width,height;
 char title[384],artist[384];
 std::uint8_t rgba[96*96*4];
};

extern "C" __declspec(dllexport) void WINAPI SintetPresenceMediaEnable(unsigned int enabled) {
 if(!enabled){SintetMedia::Stop();return;}
 HMODULE pinned;
 if(GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,
                       reinterpret_cast<LPCWSTR>(&SintetPresenceMediaEnable),&pinned))SintetMedia::Start();
}

extern "C" __declspec(dllexport) unsigned int WINAPI SintetPresenceMediaRead(SintetPresenceSample* sample,unsigned int size) {
 if(!sample||size!=sizeof(SintetPresenceSample))return 0;
 try{
  const auto track=SintetMedia::Snapshot();
  if(!track.playing||track.title.empty())return 0;
  sample->generation=track.generation;
  sample->width=0;sample->height=0;
  strncpy_s(sample->title,sizeof(sample->title),track.title.c_str(),_TRUNCATE);
  strncpy_s(sample->artist,sizeof(sample->artist),track.artist.c_str(),_TRUNCATE);
  const auto& cover=track.cover;
  if(cover&&cover->width<=96&&cover->height<=96&&cover->rgba.size()==cover->width*cover->height*4){
   sample->width=cover->width;sample->height=cover->height;
   std::memcpy(sample->rgba,cover->rgba.data(),cover->rgba.size());
  }
  return 1;
 }catch(...){return 0;}
}
