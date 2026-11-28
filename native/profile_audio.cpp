#include "media.h"
#include <windows.h>
#include <audioclient.h>
#include <mmdeviceapi.h>
#include <ksmedia.h>
#include <wrl/client.h>
#include <algorithm>
#include <array>
#include <atomic>
#include <cmath>
#include <cstring>
#include <thread>

namespace {
using Microsoft::WRL::ComPtr;
constexpr unsigned bands=8;
std::array<std::atomic<unsigned>,bands> levels{};
std::atomic<ULONGLONG> requestedUntil{0};
std::atomic<bool> running{false};

bool requested(){return GetTickCount64()<requestedUntil.load();}
bool playing(){return SintetMedia::Snapshot().playing;}

void capture(){
 ComPtr<IMMDeviceEnumerator> devices;
 ComPtr<IMMDevice> device;
 ComPtr<IAudioClient> client;
 ComPtr<IAudioCaptureClient> packets;
 if(FAILED(CoCreateInstance(__uuidof(MMDeviceEnumerator),nullptr,CLSCTX_ALL,IID_PPV_ARGS(&devices)))||
    FAILED(devices->GetDefaultAudioEndpoint(eRender,eMultimedia,&device))||
    FAILED(device->Activate(__uuidof(IAudioClient),CLSCTX_ALL,nullptr,reinterpret_cast<void**>(client.GetAddressOf()))))return;
 WAVEFORMATEX* format=nullptr;
 if(FAILED(client->GetMixFormat(&format)))return;
 const auto framesPerSecond=format->nSamplesPerSec;
 const auto channels=format->nChannels;
 const auto blockAlign=format->nBlockAlign;
 bool isFloat=format->wFormatTag==WAVE_FORMAT_IEEE_FLOAT;
 bool isPCM=format->wFormatTag==WAVE_FORMAT_PCM;
 if(format->wFormatTag==WAVE_FORMAT_EXTENSIBLE&&format->cbSize>=22){
  const auto& subtype=reinterpret_cast<WAVEFORMATEXTENSIBLE*>(format)->SubFormat;
  isFloat=subtype.Data1==WAVE_FORMAT_IEEE_FLOAT;
  isPCM=subtype.Data1==WAVE_FORMAT_PCM;
 }
 const unsigned bytes=format->wBitsPerSample/8;
 const bool supported=framesPerSecond&&channels&&blockAlign>=channels*bytes&&
   ((isFloat&&bytes==4)||(isPCM&&(bytes==2||bytes==3||bytes==4)));
 const auto initialized=supported?client->Initialize(AUDCLNT_SHAREMODE_SHARED,AUDCLNT_STREAMFLAGS_LOOPBACK,
   0,0,format,nullptr):E_FAIL;
 CoTaskMemFree(format);
 if(FAILED(initialized)||FAILED(client->GetService(IID_PPV_ARGS(&packets)))||FAILED(client->Start()))return;

 constexpr std::array<float,bands> cutoff={65,105,175,320,650,1500,3500,8000};
 std::array<float,bands> coefficient{},low{},envelope{};
 for(unsigned i=0;i<bands;i++)coefficient[i]=1-std::exp(-6.2831853f*cutoff[i]/framesPerSecond);
 while(requested()&&playing()){
  std::array<float,bands> energy{};
  unsigned count=0;
  UINT32 ready=0;
  bool failed=false;
  while(true){
   if(FAILED(packets->GetNextPacketSize(&ready))){failed=true;break;}
   if(!ready)break;
   BYTE* buffer=nullptr;
   UINT32 frames=0;
   DWORD flags=0;
   if(FAILED(packets->GetBuffer(&buffer,&frames,&flags,nullptr,nullptr))){failed=true;break;}
   for(UINT32 frame=0;frame<frames;frame++){
    float sample=0;
    if(buffer&&!(flags&AUDCLNT_BUFFERFLAGS_SILENT)){
     for(unsigned channel=0;channel<channels;channel++){
      const BYTE* data=buffer+frame*blockAlign+channel*bytes;
      if(isFloat){
       float value;std::memcpy(&value,data,4);
       if(std::isfinite(value))sample+=std::clamp(value,-1.f,1.f);
      }else if(bytes==2){
       std::int16_t value;std::memcpy(&value,data,2);
       sample+=value/32768.f;
      }else{
       std::int32_t value=0;
       if(bytes==3)std::memcpy(reinterpret_cast<BYTE*>(&value)+1,data,3);
       else std::memcpy(&value,data,4);
       sample+=value/2147483648.f;
      }
     }
     sample/=channels;
    }
    float previous=0;
    for(unsigned i=0;i<bands;i++){
     low[i]+=coefficient[i]*(sample-low[i]);
     const float value=low[i]-previous;
     previous=low[i];
     energy[i]+=value*value;
    }
   }
   count+=frames;
   packets->ReleaseBuffer(frames);
  }
  if(failed)break;
  for(unsigned i=0;i<bands;i++){
   const float rms=count?std::sqrt(energy[i]/count):0;
   const float target=std::clamp((rms-.0015f)*(i<3?9.f:17.f),0.f,1.f);
   envelope[i]+=(target-envelope[i])*(target>envelope[i]?.72f:.20f);
   levels[i].store(static_cast<unsigned>(envelope[i]*1000));
  }
  Sleep(24);
 }
 client->Stop();
}

void work(){
 const auto apartment=CoInitializeEx(nullptr,COINIT_MULTITHREADED);
 if(SUCCEEDED(apartment)){
  while(requested()){
   if(playing())capture();
   for(auto& level:levels)level.store(0);
   Sleep(140);
  }
  CoUninitialize();
 }
 running.store(false);
}
}

extern "C" __declspec(dllexport) unsigned int WINAPI SintetProfileSpectrum(unsigned int* values,unsigned int size){
 if(!values){requestedUntil.store(0);return 0;}
 if(size!=bands)return 0;
 requestedUntil.store(GetTickCount64()+700);
 if(!running.exchange(true)){
  HMODULE pinned;
  if(!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,
       reinterpret_cast<LPCWSTR>(&SintetProfileSpectrum),&pinned)){running.store(false);return 0;}
  try{std::thread(work).detach();}catch(...){running.store(false);return 0;}
 }
 if(!playing())return 0;
 for(unsigned i=0;i<bands;i++)values[i]=levels[i].load();
 return 1;
}
