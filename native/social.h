#pragma once
#include <memory>
#include <string>
#include <unordered_map>
#include <vector>
namespace SintetSocial {
enum class AccountProvider { Unknown, Sintet, Microsoft, Ely, Local };
struct Person {
 std::string id,name,avatar;
 AccountProvider provider=AccountProvider::Unknown;
 std::vector<std::string> badges;
 std::string publicID,handle;
};
struct BadgeIcon {std::string label,pixels;};
using BadgeCatalog=std::unordered_map<std::string,BadgeIcon>;
struct Friend {
 Person person;
 std::string status,presence,server;
 bool incoming=false,joinableLobby=false;
 std::string version,build,musicTitle,musicArtist,musicDigest;
 bool hasTotals=false;
 long long launcherMs=0,minecraftMs=0;
};
struct Member {Person person;bool leader=false;std::string preparation;int percent=0;std::string handle;bool voiceMic=false,voiceSpeaking=false,voiceMuted=false;};
struct Invite {std::string id,code;Person sender;std::string game,build,loader;};
struct LinkQuality {
 bool available=false;
 int ping=-1;
 double loss=-1, kbPerSec=0;
 std::string level;
};
struct NetworkPeer {
 std::string name;
 LinkQuality quality;
 AccountProvider provider=AccountProvider::Unknown;
 std::vector<std::string> badges;
};
struct State {
	std::string packPhase,packName,packGame,packLoader,packDigest,packCurrent,packError;
	bool packRestart=false,packNeedsGame=false;
	long long packBytes=0,packDone=0,packTotal=0;
	int packMissing=0;
 bool enabled=false,busy=false,gameReady=false,gameJoining=false,leader=false,offline=false,reconnecting=false;
 bool worldReady=false,autoConnect=false,inWorld=false,friendsOpen=false,friendLobbiesAvailable=false,voiceEnabled=false,voiceSpatial=true;
 std::string voiceError;
 bool preGameLobby=false,noPack=false,lobbyConnected=false,hostNeedsPack=false;
 bool turnActive=false;
 long long turnUsed=0,turnLimit=0,turnNotice=0;
 std::string error,connection,room,host;
 std::string lobbyProvider;
 Person self;
 std::shared_ptr<const BadgeCatalog> badgeCatalog;
 int roomCapacity=4;
 int voiceRange=48;
 bool voiceMixerAvailable=false,voiceMuted=false;
 int voiceVolume=100;
 int lanPort=0,localPort=0,connected=0;
 LinkQuality quality;
 std::vector<NetworkPeer> networkPeers;
 std::vector<Member> members;
 std::vector<Friend> friends;
 std::vector<Invite> invites;
};
void Start();
void Stop();
State Snapshot();
AccountProvider ParseAccountProvider(const std::string&);
bool MixedProfileLobby(const State&);
std::string AvatarPixels(const Person&);
std::string MusicCoverPixels(const Friend&);
const char* InviteNotice();
std::wstring LobbySearchKey(const std::string&);
void Action(const std::string&,const std::string& target="");
}
