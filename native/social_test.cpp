#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <cstdio>
#include "social.h"
int main(){
 SintetSocial::Start();bool ready=false;
 for(int i=0;i<40;i++){auto state=SintetSocial::Snapshot();if(state.enabled&&state.friends.size()==1&&state.friends[0].person.name=="Adolo"){ready=true;break;}Sleep(100);}
 if(!ready){SintetSocial::Stop();fputs("Native IPC state was not received\n",stderr);return 1;}
 SintetSocial::Action("invite","test-account");
 bool found=false;
 for(int i=0;i<40;i++){auto state=SintetSocial::Snapshot();if(state.connection=="invite-received"){found=true;break;}Sleep(100);}
 SintetSocial::Stop();
 if(!found){fputs("Native IPC action queue failed\n",stderr);return 2;}
 puts("Native IPC state and actions passed");return 0;
}
