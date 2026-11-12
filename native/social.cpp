#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include "language.h"
#include <winhttp.h>
#include <atomic>
#include <mutex>
#include <deque>
#include <unordered_map>
#include "vendor/nlohmann/json.hpp"
#include "social.h"
namespace SintetSocial {
namespace {
using Json=nlohmann::json;
std::mutex mutex;
State state;
std::atomic<bool> stopping{false};
std::atomic<bool> commandBusy{false};
HANDLE pollThread,actionThread;
std::wstring hostname=L"127.0.0.1",authorization;
INTERNET_PORT port;
struct Command {std::string action,target;};
std::deque<Command> commands;
struct AvatarEntry {std::string pixels;ULONGLONG nextAttempt=0;};
std::unordered_map<std::string,AvatarEntry> avatars;
std::deque<Person> avatarQueue;
HINTERNET pollRequest,actionRequest;
std::wstring wide(const std::string& str){int n=MultiByteToWideChar(CP_UTF8,0,str.data(),(int)str.size(),nullptr,0);std::wstring out(n,0);MultiByteToWideChar(CP_UTF8,0,str.data(),(int)str.size(),out.data(),n);return out;}
Json request(bool action,const std::wstring& path,const std::string& body="") {
 HINTERNET session=WinHttpOpen(L"Sintet Overlay/1.0",WINHTTP_ACCESS_TYPE_NO_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0);
 if(!session)return Json{{"error",OverlayText("Связь с лаунчером недоступна")}};
 WinHttpSetTimeouts(session,1000,1000,2000,action?110000:1500);
 HINTERNET connection=WinHttpConnect(session,hostname.c_str(),port,0);
 HINTERNET handle=connection?WinHttpOpenRequest(connection,body.empty()?L"GET":L"POST",path.c_str(),nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,0):nullptr;
 std::string data;bool ok=false;
 if(handle){
  {std::lock_guard<std::mutex> lock(mutex);if(action)actionRequest=handle;else pollRequest=handle;}
  const std::wstring headers=L"Authorization: Bearer "+authorization+L"\r\nContent-Type: application/json\r\n";
  ok=WinHttpSendRequest(handle,headers.c_str(),(DWORD)headers.size(),body.empty()?WINHTTP_NO_REQUEST_DATA:(void*)body.data(),(DWORD)body.size(),(DWORD)body.size(),0)&&WinHttpReceiveResponse(handle,nullptr);
  while(ok){char block[8192];DWORD read=0;if(!WinHttpReadData(handle,block,sizeof(block),&read)){ok=false;break;}if(!read)break;data.append(block,read);if(data.size()>256*1024){ok=false;break;}}
  {std::lock_guard<std::mutex> lock(mutex);auto& active=action?actionRequest:pollRequest;if(active==handle){active=nullptr;WinHttpCloseHandle(handle);}}
 }
 if(connection)WinHttpCloseHandle(connection);WinHttpCloseHandle(session);
 if(!ok)return Json{{"error",OverlayText("Связь с лаунчером недоступна")}};
 auto value=Json::parse(data,nullptr,false);return value.is_object()?value:Json{{"error",OverlayText("Некорректный ответ лаунчера")}};
}
Person person(const Json& value){if(!value.is_object())return {};return {value.value("id",""),value.value("username",""),value.value("avatarUrl","")};}
LinkQuality linkQuality(const Json& value){
 if(!value.is_object())return {};
 return {true,value.value("pingMs",-1),value.value("loss",-1.0),value.value("kbPerSec",0.0),value.value("level","")};
}
DWORD WINAPI poll(void*) {
 std::string lastInvite;
 int ipcFailures=0;
 while(!stopping){
  auto value=request(false,L"/state");
  try{
   std::lock_guard<std::mutex> lock(mutex);
   if(!value.contains("enabled")){
    state.reconnecting=true;
    if(++ipcFailures>=3){state.error=value.value("error",OverlayText("Связь с лаунчером недоступна"));state.quality={};state.networkPeers.clear();}
   }else{
   ipcFailures=0;
   state.reconnecting=value.value("reconnecting",false);
   state.busy=commandBusy||value.value("busy",false);state.enabled=value.value("enabled",false);state.offline=value.value("offline",false);state.error=value.value("error","");state.connection=value.value("connection","");
   state.leader=value.value("leader",false);state.gameReady=value.value("gameReady",false);state.gameJoining=value.value("gameJoining",false);state.lanPort=value.value("lanPort",0);state.localPort=value.value("localPort",0);state.connected=value.value("connected",0);state.host=value.value("hostName","");
   state.worldReady=value.value("worldReady",false);state.autoConnect=value.value("autoConnect",false);state.inWorld=value.value("inWorld",false);
   state.preGameLobby=value.value("preGameLobby",false);
   state.quality=linkQuality(value.value("quality",Json()));
   state.networkPeers.clear();
   if(value.contains("networkPeers")&&value["networkPeers"].is_array())for(const auto& row:value["networkPeers"]){
    if(row.is_object()&&state.networkPeers.size()<4)state.networkPeers.push_back({row.value("name",""),linkQuality(row)});
   }
   state.room=value.contains("room")&&value["room"].is_object()?value["room"].value("code",""):"";
   const auto pack=value.value("pack",Json());
   state.packPhase.clear();state.packDigest.clear();state.packError.clear();
   if(pack.is_object()){
    state.packPhase=pack.value("phase","");state.packName=pack.value("name","");state.packGame=pack.value("game","");state.packLoader=pack.value("loader","");state.packDigest=pack.value("digest","");state.packCurrent=pack.value("current","");state.packError=pack.value("error","");state.packMissing=pack.value("missing",0);state.packBytes=pack.value("bytes",0LL);state.packDone=pack.value("done",0LL);state.packTotal=pack.value("total",0LL);state.packRestart=pack.value("restart",false);state.packNeedsGame=pack.value("needsGame",false);
   }
   state.friends.clear();state.invites.clear();state.members.clear();
   if(value.contains("members")&&value["members"].is_array())for(auto& row:value["members"])state.members.push_back({person(row),row.value("leader",false),row.value("preparation",""),row.value("percent",0)});
   if(value.contains("friends")&&value["friends"].is_array())for(auto& row:value["friends"]){
    Friend f;f.person=person(row.value("account",Json()));f.status=row.value("status","");f.incoming=row.value("incoming",false);
    const auto& presence=row.value("presence",Json());
    if(presence.is_object()){
     f.presence=presence.value("status","");
     const auto& activity=presence.value("activity",Json());
     if(activity.is_object()){
      f.presence=activity.value("mode",f.presence);
      if(f.presence=="multiplayer")f.server=activity.value("server","");
     }
    }
    if(!f.person.id.empty())state.friends.push_back(f);
   }
   if(value.contains("invites")&&value["invites"].is_array())for(auto& row:value["invites"])
    state.invites.push_back({row.value("id",""),row.value("room_code",""),person(row.value("account",Json())),row.value("game",""),row.value("build",""),row.value("loader","")});
   if(!state.invites.empty()&&state.invites[0].id!=lastInvite){lastInvite=state.invites[0].id;MessageBeep(MB_OK);}
   }
  }catch(...){std::lock_guard<std::mutex> lock(mutex);state.error=OverlayText("Не удалось прочитать список друзей");}
  for(int i=0;i<7&&!stopping;i++)Sleep(100);
 }
 return 0;
}
DWORD WINAPI actions(void*) {
 while(!stopping){
  Command command;Person avatar;bool found=false;
  {std::lock_guard<std::mutex> lock(mutex);if(!commands.empty()){command=commands.front();commands.pop_front();state.busy=true;commandBusy=true;found=true;}
   else if(!avatarQueue.empty()){avatar=avatarQueue.front();avatarQueue.pop_front();}
  }
  if(!avatar.id.empty()){
   auto result=request(true,L"/avatar?id="+wide(avatar.id));
   auto pixels=result.value("pixels","");
   std::lock_guard<std::mutex> lock(mutex);
   auto entry=avatars.find(avatar.avatar);
   if(entry!=avatars.end()&&pixels.size()==36*36*8)entry->second.pixels=std::move(pixels);
   continue;
  }
  if(!found){Sleep(100);continue;}
  auto result=request(true,L"/action",Json{{"action",command.action},{"target",command.target}}.dump());
  try{std::lock_guard<std::mutex> lock(mutex);commandBusy=false;state.busy=false;if(result.contains("error"))state.error=result.value("error","");
  }catch(...){std::lock_guard<std::mutex> lock(mutex);commandBusy=false;state.busy=false;state.error=OverlayText("Действие не выполнено");}
 }
 return 0;
}
}
std::string AvatarPixels(const Person& person){
 if(person.avatar.empty()||person.id.size()!=36||person.id.find_first_not_of("0123456789abcdef-")!=std::string::npos)return {};
 std::lock_guard<std::mutex> lock(mutex);
 if(avatars.size()>=256&&avatars.find(person.avatar)==avatars.end())avatars.clear();
 auto& entry=avatars[person.avatar];
 if(entry.pixels.empty()&&entry.nextAttempt<=GetTickCount64()&&avatarQueue.size()<16){
  entry.nextAttempt=GetTickCount64()+30000;
  avatarQueue.push_back(person);
 }
 return entry.pixels;
}
State Snapshot(){std::lock_guard<std::mutex> lock(mutex);return state;}
void Action(const std::string& action,const std::string& target){std::lock_guard<std::mutex> lock(mutex);if(commands.size()<8)commands.push_back({action,target});}
void Start(){
 char address[64]={0},secret[65]={0};GetEnvironmentVariableA("SINTET_OVERLAY_IPC",address,sizeof(address));GetEnvironmentVariableA("SINTET_OVERLAY_IPC_SECRET",secret,sizeof(secret));
 const std::string value(address);if(value.rfind("127.0.0.1:",0)!=0||strlen(secret)!=64)return;
 int number=atoi(address+10);if(number<1||number>65535)return;port=(INTERNET_PORT)number;authorization=wide(secret);
 SecureZeroMemory(secret,sizeof(secret));stopping=false;
 pollThread=CreateThread(nullptr,0,poll,nullptr,0,nullptr);actionThread=CreateThread(nullptr,0,actions,nullptr,0,nullptr);
}
void Stop(){stopping=true;{std::lock_guard<std::mutex> lock(mutex);if(pollRequest){WinHttpCloseHandle(pollRequest);pollRequest=nullptr;}if(actionRequest){WinHttpCloseHandle(actionRequest);actionRequest=nullptr;}}
 if(pollThread){WaitForSingleObject(pollThread,2500);CloseHandle(pollThread);pollThread=nullptr;}if(actionThread){WaitForSingleObject(actionThread,2500);CloseHandle(actionThread);actionThread=nullptr;}
}
}
