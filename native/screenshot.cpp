#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include "language.h"
#include <wincodec.h>
#include <wrl/client.h>
#include <gl/GL.h>
#include <algorithm>
#include <atomic>
#include <cstring>
#include <filesystem>
#include <mutex>
#include <utility>
#include <vector>
#include "screenshot.h"

namespace SintetScreenshot {
namespace {
using Microsoft::WRL::ComPtr;

struct Frame {
 HWND window=nullptr;
 UINT width=0,height=0;
 std::vector<BYTE> pixels;
};

HANDLE stopEvent=nullptr,readyEvent=nullptr,worker=nullptr;
std::mutex mutex;
Frame queued;
std::atomic<bool> requested{false},busy{false};
std::atomic<int> result{0};
std::atomic<ULONGLONG> shownUntil{0};

void notify(bool saved,bool copied){
 result=(saved?1:0)|(copied?2:0);
 shownUntil=GetTickCount64()+3500;
}

bool encodePng(Frame& frame,const wchar_t* path){
 ComPtr<IWICImagingFactory> factory;
 ComPtr<IWICStream> stream;
 ComPtr<IWICBitmapEncoder> encoder;
 ComPtr<IWICBitmapFrameEncode> image;
 WICPixelFormatGUID format=GUID_WICPixelFormat32bppBGRA;
 if(FAILED(CoCreateInstance(CLSID_WICImagingFactory,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(&factory)))||
    FAILED(factory->CreateStream(&stream))||FAILED(stream->InitializeFromFilename(path,GENERIC_WRITE))||
    FAILED(factory->CreateEncoder(GUID_ContainerFormatPng,nullptr,&encoder))||FAILED(encoder->Initialize(stream.Get(),WICBitmapEncoderNoCache))||
    FAILED(encoder->CreateNewFrame(&image,nullptr))||FAILED(image->Initialize(nullptr))||
    FAILED(image->SetSize(frame.width,frame.height))||FAILED(image->SetPixelFormat(&format))||format!=GUID_WICPixelFormat32bppBGRA)return false;
 return SUCCEEDED(image->WritePixels(frame.height,frame.width*4,static_cast<UINT>(frame.pixels.size()),frame.pixels.data()))&&
        SUCCEEDED(image->Commit())&&SUCCEEDED(encoder->Commit());
}

bool save(Frame& frame){
 std::error_code error;
 const auto root=std::filesystem::current_path(error);
 if(error)return false;
 const auto directory=root/L"screenshots";
 std::filesystem::create_directories(directory,error);
 if(error)return false;
 SYSTEMTIME now;
 GetLocalTime(&now);
 wchar_t name[80];
 swprintf_s(name,L"%04d-%02d-%02d_%02d.%02d.%02d_%03d_%lu",now.wYear,now.wMonth,now.wDay,
            now.wHour,now.wMinute,now.wSecond,now.wMilliseconds,GetCurrentProcessId());
 const auto target=directory/(std::wstring(name)+L".png");
 const auto temp=directory/(std::wstring(name)+L".tmp");
 const bool written=encodePng(frame,temp.c_str())&&MoveFileExW(temp.c_str(),target.c_str(),MOVEFILE_WRITE_THROUGH);
 if(!written)DeleteFileW(temp.c_str());
 return written;
}

bool copyToClipboard(const Frame& frame){
 const SIZE_T bytes=sizeof(BITMAPINFOHEADER)+frame.pixels.size();
 HGLOBAL memory=GlobalAlloc(GMEM_MOVEABLE,bytes);
 if(!memory)return false;
 auto* dst=static_cast<BYTE*>(GlobalLock(memory));
 if(!dst){GlobalFree(memory);return false;}
 BITMAPINFOHEADER header{};
 header.biSize=sizeof(header);header.biWidth=frame.width;header.biHeight=-static_cast<LONG>(frame.height);
 header.biPlanes=1;header.biBitCount=32;header.biCompression=BI_RGB;header.biSizeImage=static_cast<DWORD>(frame.pixels.size());
 std::memcpy(dst,&header,sizeof(header));
 std::memcpy(dst+sizeof(header),frame.pixels.data(),frame.pixels.size());
 GlobalUnlock(memory);
 for(int attempt=0;attempt<6;++attempt){
  if(OpenClipboard(frame.window)){
   const bool copied=EmptyClipboard()&&SetClipboardData(CF_DIB,memory)!=nullptr;
   CloseClipboard();
   if(!copied)GlobalFree(memory);
   return copied;
  }
  if(WaitForSingleObject(stopEvent,45)!=WAIT_TIMEOUT)break;
 }
 GlobalFree(memory);
 return false;
}

DWORD WINAPI run(void*){
 const bool imaging=SUCCEEDED(CoInitializeEx(nullptr,COINIT_MULTITHREADED));
 HANDLE events[]={stopEvent,readyEvent};
 while(WaitForMultipleObjects(2,events,FALSE,INFINITE)==WAIT_OBJECT_0+1){
  Frame frame;
  {std::lock_guard<std::mutex> lock(mutex);frame=std::move(queued);}
  const size_t stride=frame.width*4;
  // OpenGL starts at the bottom-left. Both PNG and a top-down DIB use the top row first.
  for(UINT y=0;y<frame.height/2;++y){
   auto first=frame.pixels.begin()+y*stride;
   std::swap_ranges(first,first+stride,frame.pixels.begin()+(frame.height-1-y)*stride);
  }
  for(size_t alpha=3;alpha<frame.pixels.size();alpha+=4)frame.pixels[alpha]=255;
  bool saved=false;
  if(imaging){try{saved=save(frame);}catch(...){}}
  const bool copied=copyToClipboard(frame);
  notify(saved,copied);
  busy=false;
 }
 if(imaging)CoUninitialize();
 return 0;
}
}

void Start(){
 if(worker)return;
 stopEvent=CreateEventW(nullptr,TRUE,FALSE,nullptr);
 readyEvent=CreateEventW(nullptr,FALSE,FALSE,nullptr);
 if(stopEvent&&readyEvent)worker=CreateThread(nullptr,0,run,nullptr,0,nullptr);
 if(!worker){if(stopEvent)CloseHandle(stopEvent);if(readyEvent)CloseHandle(readyEvent);stopEvent=readyEvent=nullptr;}
}

void Stop(){
 if(!worker)return;
 SetEvent(stopEvent);
 if(WaitForSingleObject(worker,2200)!=WAIT_OBJECT_0)return;
 std::lock_guard<std::mutex> lock(mutex);
 CloseHandle(worker);CloseHandle(stopEvent);CloseHandle(readyEvent);
 worker=stopEvent=readyEvent=nullptr;
 queued={};busy=false;requested=false;
}

void Request(){if(!busy)requested=true;}

void Capture(HWND window){
 if(!requested.exchange(false)||busy.exchange(true))return;
 RECT client;
 if(GetClientRect(window,&client)&&client.right>0&&client.bottom>0&&
    static_cast<ULONGLONG>(client.right)*client.bottom<=40'000'000){
  try{
   Frame frame{window,static_cast<UINT>(client.right),static_cast<UINT>(client.bottom),{}};
   frame.pixels.resize(static_cast<size_t>(frame.width)*frame.height*4);
   glGetError();
   glReadBuffer(GL_BACK);
   glPixelStorei(GL_PACK_ALIGNMENT,4);
   constexpr GLenum bgra=0x80E1;
   glReadPixels(0,0,frame.width,frame.height,bgra,GL_UNSIGNED_BYTE,frame.pixels.data());
   if(glGetError()==GL_NO_ERROR){
    std::lock_guard<std::mutex> lock(mutex);
    if(readyEvent){queued=std::move(frame);SetEvent(readyEvent);return;}
   }
  }catch(...){ }
 }
 notify(false,false);busy=false;
}

const char* Notice(){
 if(GetTickCount64()>=shownUntil)return nullptr;
 switch(result.load()){
 case 3:return OverlayText("Снимок сохранён и скопирован");
 case 2:return OverlayText("Снимок в буфере обмена");
 case 1:return OverlayText("Снимок сохранён в screenshots");
 default:return OverlayText("Не удалось сделать снимок");
 }
}
}
