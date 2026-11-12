#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <audiopolicy.h>
#include <endpointvolume.h>
#include <mmdeviceapi.h>
#include <wrl/client.h>
#include <algorithm>
#include <atomic>
#include <mutex>
#include <optional>
#include <vector>
#include "audio.h"

namespace SintetAudio {
namespace {
using Microsoft::WRL::ComPtr;
struct Change {
 std::optional<float> level;
 std::optional<bool> muted;
 bool pending() const {return level.has_value()||muted.has_value();}
};
struct Controls {
 ComPtr<IAudioEndpointVolume> output;
 ComPtr<IAudioEndpointVolume> mic;
 std::vector<ComPtr<ISimpleAudioVolume>> game;
};

std::mutex mutex;
std::array<Volume,3> volumes;
std::array<Change,3> changes;
std::atomic<bool> watching{false},watchMixer{false};
HANDLE stopEvent=nullptr,wakeEvent=nullptr,worker=nullptr;

Controls findControls(IMMDeviceEnumerator* enumerator,bool mixer){
 Controls controls;
 ComPtr<IMMDevice> input;
 if(SUCCEEDED(enumerator->GetDefaultAudioEndpoint(eCapture,eCommunications,&input))||
    SUCCEEDED(enumerator->GetDefaultAudioEndpoint(eCapture,eConsole,&input)))
  input->Activate(__uuidof(IAudioEndpointVolume),CLSCTX_ALL,nullptr,reinterpret_cast<void**>(controls.mic.GetAddressOf()));
 if(!mixer)return controls;
 ComPtr<IMMDevice> output;
 if(SUCCEEDED(enumerator->GetDefaultAudioEndpoint(eRender,eMultimedia,&output)))
  output->Activate(__uuidof(IAudioEndpointVolume),CLSCTX_ALL,nullptr,reinterpret_cast<void**>(controls.output.GetAddressOf()));

 ComPtr<IMMDeviceCollection> devices;
 UINT count=0;
 if(FAILED(enumerator->EnumAudioEndpoints(eRender,DEVICE_STATE_ACTIVE,&devices))||FAILED(devices->GetCount(&count)))return controls;
 const DWORD ownProcess=GetCurrentProcessId();
 for(UINT i=0;i<count;++i){
  ComPtr<IMMDevice> device;
  ComPtr<IAudioSessionManager2> manager;
  ComPtr<IAudioSessionEnumerator> sessions;
  int sessionCount=0;
  if(FAILED(devices->Item(i,&device))||
     FAILED(device->Activate(__uuidof(IAudioSessionManager2),CLSCTX_ALL,nullptr,reinterpret_cast<void**>(manager.GetAddressOf())))||
     FAILED(manager->GetSessionEnumerator(&sessions))||FAILED(sessions->GetCount(&sessionCount)))continue;
  for(int j=0;j<sessionCount;++j){
   ComPtr<IAudioSessionControl> session;
   ComPtr<IAudioSessionControl2> identity;
   ComPtr<ISimpleAudioVolume> volume;
   DWORD pid=0;
   AudioSessionState state;
   if(FAILED(sessions->GetSession(j,&session))||FAILED(session.As(&identity))||identity->GetProcessId(&pid)!=S_OK||pid!=ownProcess||
      FAILED(session->GetState(&state))||state==AudioSessionStateExpired||FAILED(session.As(&volume)))continue;
   if(state==AudioSessionStateActive)controls.game.insert(controls.game.begin(),volume);
   else controls.game.push_back(volume);
  }
 }
 return controls;
}

std::array<Volume,3> readVolumes(const Controls& controls){
 std::array<Volume,3> result{};
 BOOL muted=FALSE;
 if(controls.mic&&SUCCEEDED(controls.mic->GetMute(&muted)))result[2]={true,muted!=FALSE,0};
 if(controls.output&&SUCCEEDED(controls.output->GetMasterVolumeLevelScalar(&result[0].level))&&SUCCEEDED(controls.output->GetMute(&muted)))
  result[0]={true,muted!=FALSE,result[0].level};
 for(const auto& session:controls.game){
  if(SUCCEEDED(session->GetMasterVolume(&result[1].level))&&SUCCEEDED(session->GetMute(&muted))){
   result[1]={true,muted!=FALSE,result[1].level};
   break;
  }
 }
 return result;
}

void applyVolumeChanges(const Controls& controls,const std::array<Change,3>& pending){
 if(controls.mic&&pending[2].muted)controls.mic->SetMute(*pending[2].muted,nullptr);
 if(controls.output){
  if(pending[0].level)controls.output->SetMasterVolumeLevelScalar(*pending[0].level,nullptr);
  if(pending[0].muted)controls.output->SetMute(*pending[0].muted,nullptr);
 }
 for(const auto& session:controls.game){
  if(pending[1].level)session->SetMasterVolume(*pending[1].level,nullptr);
  if(pending[1].muted)session->SetMute(*pending[1].muted,nullptr);
 }
}

DWORD WINAPI run(void*){
 if(FAILED(CoInitializeEx(nullptr,COINIT_MULTITHREADED)))return 0;
 try{
  ComPtr<IMMDeviceEnumerator> enumerator;
  Controls controls;
  ULONGLONG refreshAt=0,readAt=0;
  bool currentMixer=false;
  HANDLE events[]={stopEvent,wakeEvent};
  for(;;){
   const auto signal=WaitForMultipleObjects(2,events,FALSE,watching?55:INFINITE);
   if(signal==WAIT_OBJECT_0||signal==WAIT_FAILED)break;
   std::array<Change,3> pending;
   {
    std::lock_guard<std::mutex> lock(mutex);
    pending=changes;
    changes={};
   }
   const bool changed=pending[0].pending()||pending[1].pending()||pending[2].pending();
   if(!watching&&!changed){controls={};refreshAt=readAt=0;continue;}
   const auto now=GetTickCount64();
   const bool mixer=watchMixer||pending[0].pending()||pending[1].pending();
   if(now>=refreshAt||mixer!=currentMixer){
    if(!enumerator)CoCreateInstance(__uuidof(MMDeviceEnumerator),nullptr,CLSCTX_ALL,IID_PPV_ARGS(&enumerator));
    if(enumerator)controls=findControls(enumerator.Get(),mixer);
    currentMixer=mixer;
    refreshAt=now+850;
    readAt=0;
   }
   if(changed)applyVolumeChanges(controls,pending);
   if(changed||now>=readAt){
    const auto actual=readVolumes(controls);
    std::lock_guard<std::mutex> lock(mutex);
    volumes=actual;
    readAt=now+240;
   }
  }
 }catch(...){std::lock_guard<std::mutex> lock(mutex);volumes={};changes={};}
 CoUninitialize();
 return 0;
}
}

void Start(){
 if(worker)return;
 stopEvent=CreateEventW(nullptr,TRUE,FALSE,nullptr);
 wakeEvent=CreateEventW(nullptr,FALSE,FALSE,nullptr);
 if(stopEvent&&wakeEvent)worker=CreateThread(nullptr,0,run,nullptr,0,nullptr);
 if(!worker){if(stopEvent)CloseHandle(stopEvent);if(wakeEvent)CloseHandle(wakeEvent);stopEvent=wakeEvent=nullptr;}
}

void Stop(){
 if(!worker)return;
 SetEvent(stopEvent);
 if(WaitForSingleObject(worker,2000)!=WAIT_OBJECT_0)return;
 std::lock_guard<std::mutex> lock(mutex);
 CloseHandle(worker);CloseHandle(stopEvent);CloseHandle(wakeEvent);
 worker=stopEvent=wakeEvent=nullptr;
 watching=false;
 watchMixer=false;
 volumes={};changes={};
}

void Watch(bool open,bool mixer){
 mixer=open&&mixer;
 if(watching.load()==open&&watchMixer.load()==mixer)return;
 std::lock_guard<std::mutex> lock(mutex);
 if(!wakeEvent)return;
 const bool openChanged=watching.exchange(open)!=open;
 const bool mixerChanged=watchMixer.exchange(mixer)!=mixer;
 if(openChanged||mixerChanged)SetEvent(wakeEvent);
}

std::array<Volume,3> Snapshot(){
 std::lock_guard<std::mutex> lock(mutex);
 auto result=volumes;
 for(size_t i=0;i<result.size();++i){
  if(changes[i].level)result[i].level=*changes[i].level;
  if(changes[i].muted)result[i].muted=*changes[i].muted;
 }
 return result;
}

void SetLevel(Target target,float level){
 std::lock_guard<std::mutex> lock(mutex);
 changes[static_cast<size_t>(target)].level=std::clamp(level,0.f,1.f);
}

void SetMuted(Target target,bool muted){
 std::lock_guard<std::mutex> lock(mutex);
 changes[static_cast<size_t>(target)].muted=muted;
}
}
