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
#include <cstdio>
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
bool galleryRequested=false;
std::wstring imageRequested;
std::shared_ptr<const Gallery> gallery;
std::shared_ptr<const Image> selected;
HWND copyWindow=nullptr;
std::wstring copyRequested;
std::shared_ptr<const Gallery> thumbCatalog;
size_t thumbPage=0,nextThumb=0;
std::vector<std::shared_ptr<const Image>> thumbnails;

bool isOrdinary(const std::filesystem::path& path,bool directory){
 const DWORD flags=GetFileAttributesW(path.c_str());
 return flags!=INVALID_FILE_ATTRIBUTES&&!(flags&FILE_ATTRIBUTE_REPARSE_POINT)&&
  ((flags&FILE_ATTRIBUTE_DIRECTORY)!=0)==directory;
}

Image decodeImage(const std::filesystem::path& path,UINT limit){
 Image info;
 if(!isOrdinary(path,false))return info;
 WIN32_FILE_ATTRIBUTE_DATA attributes{};
 if(!GetFileAttributesExW(path.c_str(),GetFileExInfoStandard,&attributes)||
    attributes.nFileSizeHigh||attributes.nFileSizeLow>96u*1024*1024)return info;
 ComPtr<IWICImagingFactory> factory;
 ComPtr<IWICBitmapDecoder> decoder;
 ComPtr<IWICBitmapFrameDecode> frame;
 ComPtr<IWICBitmapScaler> scaler;
 ComPtr<IWICFormatConverter> converter;
 UINT width=0,height=0;
 if(FAILED(CoCreateInstance(CLSID_WICImagingFactory,nullptr,CLSCTX_INPROC_SERVER,IID_PPV_ARGS(&factory)))||
    FAILED(factory->CreateDecoderFromFilename(path.c_str(),nullptr,GENERIC_READ,WICDecodeMetadataCacheOnDemand,&decoder))||
    FAILED(decoder->GetFrame(0,&frame))||FAILED(frame->GetSize(&width,&height))||
    !width||!height||static_cast<unsigned long long>(width)*height>40'000'000)return info;
 const double ratio=std::min(1.,static_cast<double>(limit)/std::max(width,height));
 info.width=std::max(1u,static_cast<UINT>(width*ratio));
 info.height=std::max(1u,static_cast<UINT>(height*ratio));
 IWICBitmapSource* source=frame.Get();
 if(ratio<1.){
  if(FAILED(factory->CreateBitmapScaler(&scaler))||
     FAILED(scaler->Initialize(source,info.width,info.height,WICBitmapInterpolationModeFant)))return {};
  source=scaler.Get();
 }
 if(FAILED(factory->CreateFormatConverter(&converter))||
    FAILED(converter->Initialize(source,GUID_WICPixelFormat32bppRGBA,WICBitmapDitherTypeNone,nullptr,0,WICBitmapPaletteTypeCustom)))return {};
 info.rgba.resize(static_cast<size_t>(info.width)*info.height*4);
 if(FAILED(converter->CopyPixels(nullptr,info.width*4,static_cast<UINT>(info.rgba.size()),info.rgba.data())))return {};
 info.filename=path.filename().native();
 info.label=path.stem().u8string();
 return info;
}

std::filesystem::path configuredRoot(){
 constexpr wchar_t name[]=L"SINTET_OVERLAY_SCREENSHOTS_ROOT";
 const DWORD size=GetEnvironmentVariableW(name,nullptr,0);
 if(!size)return {};
 std::wstring value(size,0);
 const auto length=GetEnvironmentVariableW(name,value.data(),size);
 if(!length||length>=size)return {};
 value.resize(length);
 return std::filesystem::path(value);
}

std::string displayDate(FILETIME stamp){
 FILETIME local{};
 SYSTEMTIME date{};
 if(!FileTimeToLocalFileTime(&stamp,&local)||!FileTimeToSystemTime(&local,&date))return {};
 char text[42];
 std::snprintf(text,sizeof(text),"%02u.%02u.%04u · %02u:%02u",date.wDay,date.wMonth,date.wYear,date.wHour,date.wMinute);
 return text;
}

bool generatedName(const std::string& label){
 if(label.size()<19||(label.size()>19&&label[19]!='_'))return false;
 constexpr char pattern[]="0000-00-00_00.00.00";
 for(size_t i=0;i<19;++i){
  if(pattern[i]=='0'?(label[i]<'0'||label[i]>'9'):label[i]!=pattern[i])return false;
 }
 return true;
}

void addFolder(Gallery& items,const std::filesystem::path& folder){
 if(!isOrdinary(folder,true))return;
 std::error_code error;
 for(std::filesystem::directory_iterator entry(folder,error),end;!error&&entry!=end;entry.increment(error)){
  if(WaitForSingleObject(stopEvent,0)==WAIT_OBJECT_0)return;
  const auto& path=entry->path();
  if(_wcsicmp(path.extension().c_str(),L".png")!=0)continue;
  WIN32_FILE_ATTRIBUTE_DATA data{};
  if(!GetFileAttributesExW(path.c_str(),GetFileExInfoStandard,&data)||
     (data.dwFileAttributes&(FILE_ATTRIBUTE_DIRECTORY|FILE_ATTRIBUTE_REPARSE_POINT))||data.nFileSizeHigh||
     data.nFileSizeLow==0||data.nFileSizeLow>96u*1024*1024)continue;
  ULARGE_INTEGER stamp{};
  stamp.LowPart=data.ftLastWriteTime.dwLowDateTime;stamp.HighPart=data.ftLastWriteTime.dwHighDateTime;
  auto label=path.stem().u8string();
  if(generatedName(label))label.clear();
  items.push_back({path.native(),std::move(label),displayDate(data.ftLastWriteTime),stamp.QuadPart});
 }
}

std::shared_ptr<Gallery> loadCatalog(){
 auto result=std::make_shared<Gallery>();
 std::error_code error;
 const auto current=std::filesystem::current_path(error);
 if(!error)addFolder(*result,current/L"screenshots");
 const auto root=configuredRoot();
 if(!root.empty()){
  addFolder(*result,root/L"screenshots");
  if(isOrdinary(root/L"instances",true))for(const auto& parent:{root/L"instances",root/L"instances"/L"family"}){
   if(!isOrdinary(parent,true))continue;
   for(std::filesystem::directory_iterator entry(parent,error),end;!error&&entry!=end;entry.increment(error)){
    if(WaitForSingleObject(stopEvent,0)==WAIT_OBJECT_0)break;
    const auto& path=entry->path();
    if(path.filename()!=L"family"&&isOrdinary(path,true))addFolder(*result,path/L"screenshots");
   }
  }
 }
 std::sort(result->begin(),result->end(),[](const Item& a,const Item& b){
  return a.modified==b.modified?a.filename<b.filename:a.modified>b.modified;
 });
 result->erase(std::unique(result->begin(),result->end(),[](const Item& a,const Item& b){return a.filename==b.filename;}),result->end());
 return result;
}

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
  bool refresh=false;
  std::wstring image,copy,thumbnail;
  HWND clipboard=nullptr;
  std::shared_ptr<const Gallery> catalog;
  size_t thumb=0,page=0;
  {
   std::lock_guard<std::mutex> lock(mutex);
   frame=std::exchange(queued,{});
   refresh=std::exchange(galleryRequested,false);
   image=std::move(imageRequested);imageRequested.clear();
   copy=std::move(copyRequested);copyRequested.clear();clipboard=copyWindow;
   if(imaging&&thumbCatalog==gallery&&nextThumb<thumbnails.size()){
    catalog=thumbCatalog;page=thumbPage;thumb=nextThumb++;
    thumbnail=(*catalog)[page*PageSize+thumb].filename;
    if(nextThumb<thumbnails.size())SetEvent(readyEvent);
   }
  }
  if(!frame.pixels.empty()){
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
   if(saved){
    std::lock_guard<std::mutex> lock(mutex);
    refresh=refresh||static_cast<bool>(gallery);
   }
  }
  if(!copy.empty()){
   bool copied=false;
   if(imaging)try{
    auto source=decodeImage(copy,40'000'000);
    if(!source.rgba.empty()){
     for(size_t i=0;i<source.rgba.size();i+=4)std::swap(source.rgba[i],source.rgba[i+2]);
     copied=copyToClipboard({clipboard,source.width,source.height,std::move(source.rgba)});
    }
   }catch(...){}
   result=copied?2:4;shownUntil=GetTickCount64()+3500;
  }
  if(imaging)try{
   if(refresh){
    auto next=loadCatalog();
    std::lock_guard<std::mutex> lock(mutex);gallery=std::move(next);
   }
   if(!image.empty()){
    auto next=std::make_shared<Image>(decodeImage(image,1600));
    next->filename=image;
    std::lock_guard<std::mutex> lock(mutex);selected=std::move(next);
   }
   if(!thumbnail.empty()&&!refresh){
    auto data=std::make_shared<Image>(decodeImage(thumbnail,336));
    std::lock_guard<std::mutex> lock(mutex);
    if(catalog==gallery&&catalog==thumbCatalog&&thumbPage==page&&thumb<thumbnails.size())thumbnails[thumb]=std::move(data);
   }
  }catch(...){}
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
 galleryRequested=false;imageRequested.clear();gallery.reset();selected.reset();
 copyRequested.clear();copyWindow=nullptr;thumbCatalog.reset();thumbnails.clear();nextThumb=thumbPage=0;
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
 case 4:return OverlayText("Не удалось скопировать снимок");
 default:return OverlayText("Не удалось сделать снимок");
 }
}

void RefreshGallery(){
 std::lock_guard<std::mutex> lock(mutex);
 if(readyEvent){galleryRequested=true;SetEvent(readyEvent);}
}
std::shared_ptr<const Gallery> Catalog(){
 std::lock_guard<std::mutex> lock(mutex);
 return gallery;
}
std::vector<std::shared_ptr<const Image>> Thumbnails(const std::shared_ptr<const Gallery>& catalog,size_t page){
 std::lock_guard<std::mutex> lock(mutex);
 if(!readyEvent||!catalog||catalog!=gallery||catalog->empty()||page>(catalog->size()-1)/PageSize)return {};
 if(thumbCatalog!=catalog||thumbPage!=page){
  thumbCatalog=catalog;thumbPage=page;nextThumb=0;
  thumbnails.assign(std::min(PageSize,catalog->size()-page*PageSize),{});
  SetEvent(readyEvent);
 }
 return thumbnails;
}
void OpenImage(const std::wstring& filename){
 std::lock_guard<std::mutex> lock(mutex);
 if(!readyEvent||!gallery)return;
 const auto match=std::find_if(gallery->begin(),gallery->end(),[&](const Item& item){return item.filename==filename;});
 if(match==gallery->end())return;
 imageRequested=filename;selected.reset();SetEvent(readyEvent);
}
void CopyImage(HWND window,const std::wstring& filename){
 std::lock_guard<std::mutex> lock(mutex);
 if(!readyEvent||!gallery)return;
 const auto match=std::find_if(gallery->begin(),gallery->end(),[&](const Item& item){return item.filename==filename;});
 if(match==gallery->end())return;
 copyRequested=filename;copyWindow=window;SetEvent(readyEvent);
}
std::shared_ptr<const Image> Selected(){
 std::lock_guard<std::mutex> lock(mutex);
 return selected;
}
}
