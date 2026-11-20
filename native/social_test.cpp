#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <cstdio>
#include "social.h"
int main(){
 using SintetSocial::AccountProvider;
 if(SintetSocial::LobbySearchKey("  @ТеСт149  ")!=L"тест149")return 3;
 SintetSocial::State lobby;
 lobby.lobbyConnected=true;
 lobby.room="123456";
 lobby.lobbyProvider="local";
 lobby.members.resize(2);
 lobby.members[0].person.provider=AccountProvider::Local;
 if(SintetSocial::MixedProfileLobby(lobby))return 4;
 lobby.members[1].person.provider=AccountProvider::Microsoft;
 if(!SintetSocial::MixedProfileLobby(lobby))return 5;
 lobby.lobbyProvider="microsoft";
 if(!SintetSocial::MixedProfileLobby(lobby))return 6;
 lobby.lobbyConnected=false;
 if(SintetSocial::MixedProfileLobby(lobby))return 7;
 SintetSocial::Start();bool ready=false;
 for(int i=0;i<40;i++){
  auto state=SintetSocial::Snapshot();
  if(state.enabled&&state.lobbyProvider=="local"&&state.roomCapacity==150&&state.members.size()==150&&
     state.members.back().handle=="player149"&&state.members.back().person.provider==AccountProvider::Microsoft&&
     state.networkPeers.size()==1&&state.networkPeers[0].provider==AccountProvider::Microsoft&&
     state.members.back().person.badges==std::vector<std::string>{"developer"}&&
     state.self.badges==std::vector<std::string>{"developer"}&&
     state.networkPeers[0].badges==std::vector<std::string>{"developer"}&&
     state.badgeCatalog&&state.badgeCatalog->count("developer")==1&&
     state.badgeCatalog->at("developer").label=="Developer"&&
     SintetSocial::MixedProfileLobby(state)&&state.turnLimit==1000000000LL){ready=true;break;}
  Sleep(100);
 }
 if(!ready){SintetSocial::Stop();fputs("Native IPC state was not received\n",stderr);return 1;}
 SintetSocial::Action("join","123456");
 bool found=false;
 for(int i=0;i<40;i++){
  auto state=SintetSocial::Snapshot();
  if(state.lobbyProvider=="sintet"&&state.friends.size()==1&&state.friends[0].person.name=="Adolo"&&
     state.friends[0].person.provider==AccountProvider::Local&&state.connection=="join-received"&&
     state.roomCapacity==8&&state.members.size()==8&&state.networkPeers.size()==1&&
     state.networkPeers[0].provider==AccountProvider::Ely&&SintetSocial::MixedProfileLobby(state)&&
     state.friends[0].person.badges==std::vector<std::string>{"developer"}&&
     state.badgeCatalog&&state.badgeCatalog->count("developer")==1&&
     state.badgeCatalog->at("developer").label=="Maintainer"&&state.turnLimit==5000000000LL){found=true;break;}
  Sleep(100);
 }
 SintetSocial::Stop();
 if(!found){fputs("Native IPC action queue failed\n",stderr);return 2;}
 puts("Native IPC state and actions passed");return 0;
}
