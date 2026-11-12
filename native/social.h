#pragma once
#include <string>
#include <vector>
namespace SintetSocial {
struct Person {std::string id,name,avatar;};
struct Friend {Person person;std::string status,presence,server;bool incoming=false;};
struct Member {Person person;bool leader=false;std::string preparation;int percent=0;};
struct Invite {std::string id,code;Person sender;std::string game,build,loader;};
struct LinkQuality {
 bool available=false;
 int ping=-1;
 double loss=-1, kbPerSec=0;
 std::string level;
};
struct NetworkPeer {std::string name;LinkQuality quality;};
struct State {
	std::string packPhase,packName,packGame,packLoader,packDigest,packCurrent,packError;
	bool packRestart=false,packNeedsGame=false;
	long long packBytes=0,packDone=0,packTotal=0;
	int packMissing=0;
 bool enabled=false,busy=false,gameReady=false,gameJoining=false,leader=false,offline=false,reconnecting=false;
 bool worldReady=false,autoConnect=false,inWorld=false;
 bool preGameLobby=false;
 std::string error,connection,room,host;
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
std::string AvatarPixels(const Person&);
void Action(const std::string&,const std::string& target="");
}
