#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include "language.h"
#include <winhttp.h>
#include <algorithm>
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
ULONGLONG autoConnectPendingUntil=0;
struct AvatarEntry {std::string pixels;ULONGLONG nextAttempt=0;};
std::unordered_map<std::string,AvatarEntry> avatars;
std::deque<Person> avatarQueue;
struct CoverRequest {std::string id,digest;};
std::unordered_map<std::string,AvatarEntry> covers;
std::deque<CoverRequest> coverQueue;
std::atomic<ULONGLONG> inviteNoticeUntil{0};
HINTERNET pollRequest,actionRequest;
std::wstring wide(const std::string& str){int n=MultiByteToWideChar(CP_UTF8,0,str.data(),(int)str.size(),nullptr,0);std::wstring out(n,0);MultiByteToWideChar(CP_UTF8,0,str.data(),(int)str.size(),out.data(),n);return out;}
Json request(bool action,const std::wstring& path,const std::string& body="",size_t maxBytes=256*1024) {
 HINTERNET session=WinHttpOpen(L"Sintet Overlay/1.0",WINHTTP_ACCESS_TYPE_NO_PROXY,WINHTTP_NO_PROXY_NAME,WINHTTP_NO_PROXY_BYPASS,0);
 if(!session)return Json{{"error",OverlayText("Связь с лаунчером недоступна")}};
 WinHttpSetTimeouts(session,1000,1000,2000,action?(body.empty()?3500:110000):1500);
 HINTERNET connection=WinHttpConnect(session,hostname.c_str(),port,0);
 HINTERNET handle=connection?WinHttpOpenRequest(connection,body.empty()?L"GET":L"POST",path.c_str(),nullptr,WINHTTP_NO_REFERER,WINHTTP_DEFAULT_ACCEPT_TYPES,0):nullptr;
 std::string data;bool ok=false;
 if(handle){
  {std::lock_guard<std::mutex> lock(mutex);if(action)actionRequest=handle;else pollRequest=handle;}
  const std::wstring headers=L"Authorization: Bearer "+authorization+L"\r\nContent-Type: application/json\r\n";
  ok=WinHttpSendRequest(handle,headers.c_str(),(DWORD)headers.size(),body.empty()?WINHTTP_NO_REQUEST_DATA:(void*)body.data(),(DWORD)body.size(),(DWORD)body.size(),0)&&WinHttpReceiveResponse(handle,nullptr);
  while(ok){char block[8192];DWORD read=0;if(!WinHttpReadData(handle,block,sizeof(block),&read)){ok=false;break;}if(!read)break;data.append(block,read);if(data.size()>maxBytes){ok=false;break;}}
  {std::lock_guard<std::mutex> lock(mutex);auto& active=action?actionRequest:pollRequest;if(active==handle){active=nullptr;WinHttpCloseHandle(handle);}}
 }
 if(connection)WinHttpCloseHandle(connection);WinHttpCloseHandle(session);
 if(!ok)return Json{{"error",OverlayText("Связь с лаунчером недоступна")}};
 auto value=Json::parse(data,nullptr,false);return value.is_object()?value:Json{{"error",OverlayText("Некорректный ответ лаунчера")}};
}
std::vector<std::string> badgeIDs(const Json& row){
 std::vector<std::string> ids;
 const auto field=row.find("badges");
 if(field==row.end()||!field->is_array())return ids;
 for(const auto& item:*field){
  if(ids.size()==32)break;
  if(!item.is_string())continue;
  const auto& id=item.get_ref<const std::string&>();
  if(!id.empty()&&id.size()<=40&&std::find(ids.begin(),ids.end(),id)==ids.end())ids.push_back(id);
 }
 return ids;
}
Person person(const Json& value){
 if(!value.is_object())return {};
 Person account{value.value("id",""),value.value("username",""),value.value("avatarUrl",""),
         ParseAccountProvider(value.value("provider","")),badgeIDs(value),value.value("publicID",""),value.value("handle","")};
 if(account.publicID.size()<9||account.publicID.size()>16||account.publicID.find_first_not_of("0123456789")!=std::string::npos)account.publicID.clear();
 return account;
}
void pollBadges(std::string& revision){
 const auto reply=request(false,L"/badges?revision="+wide(revision),"",2*1024*1024);
 const auto current=reply.value("revision","");
 const auto icons=reply.find("icons");
 if(current.size()!=64||current==revision||current.find_first_not_of("0123456789abcdef")!=std::string::npos||
    icons==reply.end()||!icons->is_object()||icons->size()>256)return;
 auto next=std::make_shared<BadgeCatalog>();
 for(auto item=icons->begin();item!=icons->end();++item){
  if(item.key().empty()||item.key().size()>40||!item.value().is_object())continue;
  auto label=item.value().value("label","");
  auto pixels=item.value().value("pixels","");
  if(!label.empty()&&label.size()<=320&&pixels.size()==24*24*8)
   next->emplace(item.key(),BadgeIcon{std::move(label),std::move(pixels)});
 }
 std::lock_guard<std::mutex> lock(mutex);
 state.badgeCatalog=std::move(next);
 revision=current;
}
LinkQuality linkQuality(const Json& value){
 if(!value.is_object())return {};
 return {true,value.value("pingMs",-1),value.value("loss",-1.0),value.value("kbPerSec",0.0),value.value("level","")};
}
DWORD WINAPI poll(void*) {
 std::string lastInvite,badgeRevision;
 ULONGLONG nextBadgePoll=0;
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
   state.worldReady=value.value("worldReady",false);state.inWorld=value.value("inWorld",false);
   state.preGameLobby=value.value("preGameLobby",false);
   state.noPack=value.value("noPack",false);
   state.lobbyConnected=value.value("lobbyConnected",false);
   state.hostNeedsPack=value.value("hostNeedsPack",false);
   state.voiceEnabled=value.value("voiceEnabled",false);state.voiceError=value.value("voiceError","");
   state.voiceSpatial=value.value("voiceSpatial",true);
   state.voiceRange=value.value("voiceRange",48);
   const auto voiceMixer=value.value("voiceMixer",Json());
   state.voiceMixerAvailable=voiceMixer.is_object();
   if(state.voiceMixerAvailable){
    state.voiceVolume=voiceMixer.value("level",100);
    state.voiceMuted=voiceMixer.value("muted",false);
   }
   state.lobbyProvider=value.value("lobbyProvider","");
   state.self=person(value.value("lobbyProfile",Json()));
   state.turnActive=value.value("turnActive",false);
   state.turnNotice=value.value("turnNotice",0LL);
   const auto usage=value.value("lobbyTurnUsage",value.value("turnUsage",Json()));
   state.turnUsed=usage.is_object()?usage.value("used",0LL):0;
   state.turnLimit=usage.is_object()?usage.value("limit",0LL):0;
   state.quality=linkQuality(value.value("quality",Json()));
   state.networkPeers.clear();
   if(value.contains("networkPeers")&&value["networkPeers"].is_array())for(const auto& row:value["networkPeers"]){
    if(row.is_object()&&state.networkPeers.size()<4)state.networkPeers.push_back({row.value("name",""),linkQuality(row),ParseAccountProvider(row.value("provider","")),badgeIDs(row)});
   }
   const auto room=value.contains("room")&&value["room"].is_object()?value["room"].value("code",""):"";
   const bool automatic=value.value("autoConnect",false);
   if(room!=state.room||state.inWorld||GetTickCount64()>=autoConnectPendingUntil||automatic==state.autoConnect){
    state.autoConnect=automatic;
    autoConnectPendingUntil=0;
   }
   state.room=room;
   state.friendsOpen=!room.empty()&&value["room"].value("friends_open",false);
   state.friendLobbiesAvailable=value.value("friendLobbiesAvailable",false);
   state.roomCapacity=room.empty()?4:value["room"].value("capacity",4);
   const auto pack=value.value("pack",Json());
   state.packPhase.clear();state.packDigest.clear();state.packError.clear();
   if(pack.is_object()){
    state.packPhase=pack.value("phase","");state.packName=pack.value("name","");state.packGame=pack.value("game","");state.packLoader=pack.value("loader","");state.packDigest=pack.value("digest","");state.packCurrent=pack.value("current","");state.packError=pack.value("error","");state.packMissing=pack.value("missing",0);state.packBytes=pack.value("bytes",0LL);state.packDone=pack.value("done",0LL);state.packTotal=pack.value("total",0LL);state.packRestart=pack.value("restart",false);state.packNeedsGame=pack.value("needsGame",false);
   }
   state.friends.clear();state.invites.clear();state.members.clear();
   if(value.contains("members")&&value["members"].is_array())for(auto& row:value["members"])state.members.push_back({person(row),row.value("leader",false),row.value("preparation",""),row.value("percent",0),row.value("handle",""),row.value("voiceMic",false),row.value("voiceSpeaking",false),row.value("voiceMuted",false)});
   if(value.contains("friends")&&value["friends"].is_array())for(auto& row:value["friends"]){
    Friend f;f.person=person(row.value("account",Json()));f.status=row.value("status","");f.incoming=row.value("incoming",false);f.joinableLobby=row.value("joinableLobby",false);
    const auto& presence=row.value("presence",Json());
    const auto totals=row.value("totals",Json());
    if(totals.is_object()){
     f.hasTotals=true;
     f.launcherMs=totals.value("launcherMs",0LL);f.minecraftMs=totals.value("minecraftMs",0LL);
    }
    if(presence.is_object()){
     f.presence=presence.value("status","");
     f.version=presence.value("version","");
     const auto& activity=presence.value("activity",Json());
     if(activity.is_object()){
      f.presence=activity.value("mode",f.presence);
      if(f.presence=="multiplayer")f.server=activity.value("server","");
      f.version=activity.value("version",f.version);f.build=activity.value("build","");
      const auto music=activity.value("music",Json());
      if(music.is_object()&&!f.presence.empty()){
       f.musicTitle=music.value("title","");f.musicArtist=music.value("artist","");f.musicDigest=music.value("digest","");
      }
     }
    }
    if(!f.person.id.empty())state.friends.push_back(f);
   }
   if(value.contains("invites")&&value["invites"].is_array())for(auto& row:value["invites"])
    state.invites.push_back({row.value("id",""),row.value("room_code",""),person(row.value("account",Json())),row.value("game",""),row.value("build",""),row.value("loader","")});
   if(!state.invites.empty()&&state.invites[0].id!=lastInvite){lastInvite=state.invites[0].id;MessageBeep(MB_OK);}
   }
  }catch(...){std::lock_guard<std::mutex> lock(mutex);state.error=OverlayText("Не удалось прочитать список друзей");}
  const auto now=GetTickCount64();
  if(!stopping&&now>=nextBadgePoll){
   nextBadgePoll=now+2000;
   try{pollBadges(badgeRevision);}catch(const Json::exception&){}
  }
  for(int i=0;i<7&&!stopping;i++)Sleep(100);
 }
 return 0;
}
DWORD WINAPI actions(void*) {
 while(!stopping){
  Command command;Person avatar;CoverRequest cover;bool found=false;
  {std::lock_guard<std::mutex> lock(mutex);if(!commands.empty()){command=commands.front();commands.pop_front();state.busy=true;commandBusy=true;found=true;}
   else if(!avatarQueue.empty()){avatar=avatarQueue.front();avatarQueue.pop_front();}
   else if(!coverQueue.empty()){cover=std::move(coverQueue.front());coverQueue.pop_front();}
  }
  if(!avatar.id.empty()){
   auto result=request(true,L"/avatar?id="+wide(avatar.id));
   auto pixels=result.value("pixels","");
   std::lock_guard<std::mutex> lock(mutex);
   auto entry=avatars.find(avatar.avatar);
   if(entry!=avatars.end()&&pixels.size()==36*36*8)entry->second.pixels=std::move(pixels);
   continue;
  }
  if(!cover.id.empty()){
   auto response=request(true,L"/music-cover?id="+wide(cover.id)+L"&digest="+wide(cover.digest),"",24*1024);
   auto pixels=response.value("pixels","");
   std::lock_guard<std::mutex> lock(mutex);
   auto entry=covers.find(cover.id+cover.digest);
   if(entry!=covers.end()&&pixels.size()==48*48*8)entry->second.pixels=std::move(pixels);
   continue;
  }
  if(!found){Sleep(100);continue;}
  auto result=request(true,L"/action",Json{{"action",command.action},{"target",command.target}}.dump());
  try{std::lock_guard<std::mutex> lock(mutex);commandBusy=false;state.busy=false;if(result.contains("error")){
   state.error=result.value("error","");
   if(command.action=="auto-connect")autoConnectPendingUntil=0;
  }else if(result.value("ok",false)){
   if(command.action=="invite")inviteNoticeUntil=GetTickCount64()+3300;
   if(command.action=="voice-volume"){state.voiceVolume=std::atoi(command.target.c_str());state.voiceMuted=false;}
   if(command.action=="voice-volume-mute")state.voiceMuted=command.target=="true";
  }
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
std::string MusicCoverPixels(const Friend& entry){
 const auto& id=entry.person.id;
 const auto& digest=entry.musicDigest;
 if(id.size()!=36||id.find_first_not_of("0123456789abcdef-")!=std::string::npos||digest.size()!=64||
    digest.find_first_not_of("0123456789abcdef")!=std::string::npos)return {};
 std::lock_guard<std::mutex> lock(mutex);
 const auto key=id+digest;
 if(covers.size()>=48&&covers.find(key)==covers.end())covers.clear();
 auto& cached=covers[key];
 if(cached.pixels.empty()&&cached.nextAttempt<=GetTickCount64()&&coverQueue.size()<8){
  cached.nextAttempt=GetTickCount64()+15000;
  coverQueue.push_back({id,digest});
 }
 return cached.pixels;
}
const char* InviteNotice(){return GetTickCount64()<inviteNoticeUntil?OverlayText("Приглашение отправлено"):nullptr;}
State Snapshot(){std::lock_guard<std::mutex> lock(mutex);return state;}
AccountProvider ParseAccountProvider(const std::string& value){
 if(value=="sintet")return AccountProvider::Sintet;
 if(value=="microsoft")return AccountProvider::Microsoft;
 if(value=="ely")return AccountProvider::Ely;
 if(value=="local")return AccountProvider::Local;
 return AccountProvider::Unknown;
}
bool MixedProfileLobby(const State& state){
 if(!state.lobbyConnected||state.room.empty())return false;
 auto provider=ParseAccountProvider(state.lobbyProvider);
 for(const auto& member:state.members){
  const auto current=member.person.provider;
  if(current==AccountProvider::Unknown)continue;
  if(provider!=AccountProvider::Unknown&&provider!=current)return true;
  provider=current;
 }
 return false;
}
std::wstring LobbySearchKey(const std::string& text){
 const auto first=text.find_first_not_of(" \t\r\n@");
 if(first==std::string::npos)return {};
 auto key=wide(text.substr(first,text.find_last_not_of(" \t\r\n")-first+1));
 if(!key.empty())CharLowerBuffW(key.data(),static_cast<DWORD>(key.size()));
 return key;
}
void Action(const std::string& action,const std::string& target){
 std::lock_guard<std::mutex> lock(mutex);
 if(commands.size()>=8)return;
 if(action=="auto-connect"){
  state.autoConnect=target=="true";
  autoConnectPendingUntil=GetTickCount64()+5000;
 }
 commands.push_back({action,target});
}
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
