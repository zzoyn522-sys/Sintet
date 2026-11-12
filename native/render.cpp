#include "language.h"
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <gl/GL.h>
#include <atomic>
#include <algorithm>
#include <cstdio>
#include <cmath>
#include <cfloat>
#include <cstring>
#include <cstdlib>
#include <initializer_list>
#include <mutex>
#include <string>
#include <vector>
#include <unordered_map>
#include "vendor/imgui/imgui.h"
#include "social.h"
#include "media.h"
#include "audio.h"
#include "screenshot.h"
#include "widgets.h"
#include "layout.h"
#include "vendor/imgui/backends/imgui_impl_win32.h"
#include "vendor/imgui/backends/imgui_impl_opengl3.h"
#include "vendor/minhook/include/MinHook.h"
extern IMGUI_IMPL_API LRESULT ImGui_ImplWin32_WndProcHandler(HWND,UINT,WPARAM,LPARAM);
namespace {
struct GLFWwindow;
using Swap = void (*)(GLFWwindow*);
using WindowHandle = HWND (*)(GLFWwindow*);
using InputMode = void (*)(GLFWwindow*,int,int);
using GetMode = int (*)(GLFWwindow*,int);
Swap originalSwap;
using NativeSwap=BOOL(WINAPI*)(HDC);
using CurrentGame=GLFWwindow*(*)();
NativeSwap originalNativeSwap;
CurrentGame currentGame;
RECT savedClip;
int cursorAdjustments=0;
WindowHandle nativeWindow;
InputMode inputMode;
GetMode getMode;
void* target;
HANDLE loaderThread;
std::atomic<bool> stopping{false};
std::recursive_mutex renderMutex;
HWND window;
std::atomic<WNDPROC> originalProc{nullptr};
ImGuiContext* context;
HGLRC overlayGL;
float interfaceScale=1.0f;
ImFont* titleFont;
ImFont* headingFont;
GLuint avatarTexture=0,buildIconTexture=0,backdropTexture=0;
std::string buildName,buildPreset,gameVersionLine,gpuRenderer;
std::string overlayHotkey="Shift + Tab";
int overlayHotkeyKey=VK_TAB,overlayHotkeyMods=MOD_SHIFT;
int backdropWidth=0,backdropHeight=0;
float copiedUntil=0;
void logo(ImDrawList*,ImVec2,float,ImU32);
void drawFittedText(ImDrawList*,ImFont*,float,ImVec2,float,ImU32,const std::string&);
GLFWwindow* game;
bool visible=false,previousToggle=false,previousEscape=false,previousCapture=false,usingGLFWCursor=false;
bool openLobbyOnStart=false;
std::atomic<bool> acceptWheel{false};
std::atomic<int> pendingWheelX{0},pendingWheelY{0};
ULONGLONG welcomeStarted=0;
ULONGLONG sessionStarted=0;
float panelAnimation=0.0f;
int oldCursor=0x34001,section=0;
constexpr int CursorMode=0x33001,CursorNormal=0x34001;
void closePanel(){
 acceptWheel=false;pendingWheelX=0;pendingWheelY=0;
 visible=false;
 if(usingGLFWCursor)inputMode(game,CursorMode,oldCursor);
 else{
  ClipCursor(&savedClip);
  while(cursorAdjustments>0){ShowCursor(FALSE);--cursorAdjustments;}
  SetCursor(LoadCursorW(nullptr,IDC_ARROW));
 }
}
void openPanel(){
 pendingWheelX=0;pendingWheelY=0;acceptWheel=true;
 visible=true;
 usingGLFWCursor=game&&getMode&&inputMode;
 if(usingGLFWCursor){oldCursor=getMode(game,CursorMode);inputMode(game,CursorMode,CursorNormal);}
 else{
  GetClipCursor(&savedClip);
  ClipCursor(nullptr);
  int cursorCount=0;
  do{
   cursorCount=ShowCursor(TRUE);
   ++cursorAdjustments;
  }while(cursorCount<0&&cursorAdjustments<16);
  SetCursor(LoadCursorW(nullptr,IDC_ARROW));
 }
}
LRESULT CALLBACK input(HWND hwnd,UINT message,WPARAM w,LPARAM l){
 // Win32 input and GL rendering can run on different threads. Queue wheel
 // deltas so a busy render mutex never drops a scroll or forwards it to Minecraft.
 if(acceptWheel&&(message==WM_MOUSEWHEEL||message==WM_MOUSEHWHEEL)){
  const int delta=static_cast<short>(HIWORD(w));
  if(message==WM_MOUSEWHEEL)pendingWheelY.fetch_add(delta);else pendingWheelX.fetch_sub(delta);
  return 0;
 }
 std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
 if(lock.owns_lock()&&!stopping&&context&&visible){
  ImGui::SetCurrentContext(context);
  ImGui_ImplWin32_WndProcHandler(hwnd,message,w,l);
  if((message>=WM_MOUSEFIRST&&message<=WM_MOUSELAST)||message==WM_INPUT||message==WM_CHAR||message==WM_KEYDOWN||message==WM_SYSKEYDOWN)return 0;
 }
 const WNDPROC previous=originalProc.load();
 return previous?CallWindowProcW(previous,hwnd,message,w,l):DefWindowProcW(hwnd,message,w,l);
}
float easeOut(float value) {
 value=std::clamp(value,0.0f,1.0f);
 return 1.0f-std::pow(1.0f-value,3.0f);
}

void drawWelcome() {
 const float elapsed=(float)(GetTickCount64()-welcomeStarted)/1000.0f;
 if(elapsed>6.0f||visible)return;
 const float scale=interfaceScale;
 const float fade=elapsed>5.4f?1.0f-easeOut((elapsed-5.4f)/.6f):easeOut(elapsed/.55f);
 const std::string title=OverlayText("Игра запущена через Sintet");
 const std::string shortcut=overlayHotkey+"  ·  "+OverlayText("Открыть оверлей");
 auto* bodyFont=ImGui::GetFont();
 const float titleSize=19*scale,shortcutSize=16*scale;
 const float left=66*scale,right=18*scale,height=86*scale;
 const float textWidth=std::max(headingFont->CalcTextSizeA(titleSize,FLT_MAX,0,title.c_str()).x,
                                bodyFont->CalcTextSizeA(shortcutSize,FLT_MAX,0,shortcut.c_str()).x);
 const float width=std::min(ImGui::GetIO().DisplaySize.x-12*scale,std::max(310*scale,left+textWidth+right+2*scale));
 if(width<=left+right)return;
 const ImVec2 top(-(1-fade)*width,26*scale),bottom(top.x+width,top.y+height);
 ImDrawList* draw=ImGui::GetForegroundDrawList();
 draw->AddRectFilled(ImVec2(top.x,top.y+5*scale),ImVec2(bottom.x+5*scale,bottom.y+5*scale),IM_COL32(0,0,0,(int)(65*fade)),6*scale);
 draw->AddRectFilled(top,bottom,IM_COL32(22,23,23,(int)(250*fade)),6*scale,ImDrawFlags_RoundCornersRight);
 draw->AddRectFilled(top,ImVec2(top.x+3*scale,bottom.y),IM_COL32(119,196,139,(int)(255*fade)));
 logo(draw,ImVec2(top.x+20*scale,top.y+26*scale),scale,IM_COL32(237,239,238,(int)(255*fade)));
 drawFittedText(draw,headingFont,titleSize,ImVec2(top.x+left,top.y+17*scale),width-left-right,
                IM_COL32(237,239,238,(int)(255*fade)),title);
 drawFittedText(draw,bodyFont,shortcutSize,ImVec2(top.x+left,top.y+47*scale),width-left-right,
                IM_COL32(160,165,162,(int)(255*fade)),shortcut);
}

// Capture the drawable in our isolated context. The scene is never read back to CPU.
void captureBackdrop() {
 if(!visible&&panelAnimation<=0)return;
 RECT rect;GetClientRect(window,&rect);
 const int width=std::min<int>(rect.right,(int)std::ceil(430*interfaceScale)),height=rect.bottom;
 if(width<=0||height<=0)return;
 static ULONGLONG capturedAt=0;
 const ULONGLONG now=GetTickCount64();
 if(backdropTexture&&width==backdropWidth&&height==backdropHeight&&now-capturedAt<33)return;
 capturedAt=now;
 if(!backdropTexture){
  glGenTextures(1,&backdropTexture);
  glBindTexture(GL_TEXTURE_2D,backdropTexture);
  glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
  glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
  glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_S,0x812F);
  glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_T,0x812F);
 }else glBindTexture(GL_TEXTURE_2D,backdropTexture);
 glReadBuffer(GL_BACK);
 if(width!=backdropWidth||height!=backdropHeight){
  glCopyTexImage2D(GL_TEXTURE_2D,0,GL_RGB,0,0,width,height,0);
  backdropWidth=width;backdropHeight=height;
 }else glCopyTexSubImage2D(GL_TEXTURE_2D,0,0,0,0,0,width,height);
 glBindTexture(GL_TEXTURE_2D,0);
}
void drawGlass(ImVec2 position,ImVec2 size) {
 if(!backdropTexture||!backdropWidth||!backdropHeight)return;
 ImDrawList* draw=ImGui::GetBackgroundDrawList();
 int sample=0;
 // Sample only the visible drawer instead of blurring the full framebuffer.
 for(int y=-1;y<=1;y++)for(int x=-1;x<=1;x++){
  ++sample;
  float dx=x*8.0f*interfaceScale,dy=y*8.0f*interfaceScale;
  ImVec2 uv0((position.x+dx)/backdropWidth,1-(position.y+dy)/backdropHeight);
  ImVec2 uv1((position.x+size.x+dx)/backdropWidth,1-(position.y+size.y+dy)/backdropHeight);
  draw->AddImageRounded((ImTextureID)(intptr_t)backdropTexture,position,ImVec2(position.x+size.x,position.y+size.y),uv0,uv1,IM_COL32(255,255,255,(int)(255.0f/sample)),16*interfaceScale,ImDrawFlags_RoundCornersRight);
 }
}
GLuint uploadAvatar(const std::string& hex) {
 if(hex.size()!=36*36*8)return 0;
 unsigned char pixels[36*36*4];
 auto digit=[](char c)->int {
  if(c>='0'&&c<='9')return c-'0';
  if(c>='a'&&c<='f')return c-'a'+10;
  return -1;
 };
 for(size_t i=0;i<sizeof(pixels);i++){
  int high=digit(hex[i*2]),low=digit(hex[i*2+1]);
  if(high<0||low<0)return 0;
  pixels[i]=(unsigned char)(high*16+low);
 }
 GLuint texture=0;
 glGenTextures(1,&texture);
 glBindTexture(GL_TEXTURE_2D,texture);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
 glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,36,36,0,GL_RGBA,GL_UNSIGNED_BYTE,pixels);
 glBindTexture(GL_TEXTURE_2D,0);
 return texture;
}

GLuint loadIcon(const char* variable) {
 char hex[36*36*8+1]={0};
 return GetEnvironmentVariableA(variable,hex,sizeof(hex))==36*36*8?uploadAvatar(hex):0;
}

std::string environmentText(const wchar_t* variable) {
 wchar_t value[256]={0};
 const DWORD length=GetEnvironmentVariableW(variable,value,256);
 if(length==0||length>=256)return {};
 const int bytes=WideCharToMultiByte(CP_UTF8,0,value,length,nullptr,0,nullptr,nullptr);
 if(bytes<=0)return {};
 std::string result(bytes,'\0');
 WideCharToMultiByte(CP_UTF8,0,value,length,result.data(),bytes,nullptr,nullptr);
 return result;
}

void loadGameProfile() {
 const auto shortcut=environmentText(L"SINTET_OVERLAY_HOTKEY");
 const int key=std::atoi(environmentText(L"SINTET_OVERLAY_HOTKEY_KEY").c_str());
 const int mods=std::atoi(environmentText(L"SINTET_OVERLAY_HOTKEY_MODS").c_str());
 if(!shortcut.empty()&&key>0&&key<256&&mods>0&&mods<=7){overlayHotkey=shortcut;overlayHotkeyKey=key;overlayHotkeyMods=mods;}
 avatarTexture=loadIcon("SINTET_OVERLAY_AVATAR");
 buildIconTexture=loadIcon("SINTET_OVERLAY_BUILD_ICON");
 buildName=environmentText(L"SINTET_OVERLAY_BUILD_NAME");
 buildPreset=environmentText(L"SINTET_OVERLAY_BUILD_PRESET");
 const auto version=environmentText(L"SINTET_OVERLAY_VERSION");
 const auto loader=environmentText(L"SINTET_OVERLAY_LOADER");
 if(!version.empty())gameVersionLine=buildName.empty()?version:"Minecraft "+version;
 if(!loader.empty()){
  if(!gameVersionLine.empty())gameVersionLine+=" · ";
  gameVersionLine+=loader;
 }
}

void drawBuildPreset(ImDrawList* draw,ImVec2 pos,float scale) {
 auto p=[&](float x,float y){return ImVec2(pos.x+x*scale,pos.y+y*scale);};
 auto color=[](int r,int g,int b){return ImGui::GetColorU32(IM_COL32(r,g,b,255));};
 const auto polygon=[&](std::initializer_list<ImVec2> points,ImU32 fill){
  draw->AddConvexPolyFilled(points.begin(),(int)points.size(),fill);
 };
 if(buildPreset=="sword"){
  const ImU32 steel=color(202,211,220),handle=color(178,132,91);
  polygon({p(29,6),p(29,13),p(16,26),p(10,20),p(23,7)},steel);
  draw->AddLine(p(10,17),p(20,27),handle,2.4f*scale);
  draw->AddLine(p(14,23),p(8,29),handle,2.8f*scale);
 }else if(buildPreset=="crystal"){
  polygon({p(18,6),p(29,16),p(18,30),p(7,16)},color(110,202,210));
  polygon({p(18,6),p(20,16),p(7,16)},color(191,240,240));
  polygon({p(20,16),p(29,16),p(18,30)},color(67,148,170));
 }else if(buildPreset=="copper"){
  draw->AddCircleFilled(p(18,18),10*scale,color(208,132,82));
  draw->AddCircle(p(18,18),6.5f*scale,color(241,174,123),0,1.3f*scale);
 }else if(buildPreset=="chest"){
  draw->AddRectFilled(p(7,10),p(29,27),color(166,112,68),2*scale);
  draw->AddRectFilled(p(7,10),p(29,18),color(213,159,97),2*scale);
  draw->AddRectFilled(p(16,16),p(20,22),color(236,205,129),scale);
 }else if(buildPreset=="end"){
  polygon({p(18,6),p(27,11),p(27,25),p(18,30),p(9,25),p(9,11)},color(136,111,190));
  draw->AddLine(p(18,6),p(18,30),color(190,164,234),1.2f*scale);
  draw->AddCircleFilled(p(25,9),2*scale,color(219,201,248));
 }else{
  polygon({p(7,12),p(18,6),p(29,12),p(18,18)},color(135,184,89));
  polygon({p(7,12),p(18,18),p(18,30),p(7,24)},color(119,85,57));
  polygon({p(18,18),p(29,12),p(29,24),p(18,30)},color(161,115,75));
 }
}

void drawFittedText(ImDrawList* draw,ImFont* font,float size,ImVec2 pos,float width,ImU32 color,const std::string& text) {
 if(text.empty()||width<=0)return;
 const char* end=nullptr;
 font->CalcTextSizeA(size,width,0,text.c_str(),nullptr,&end);
 if(end&&!*end){
  draw->AddText(font,size,pos,color,text.c_str());
  return;
 }
 const float suffixWidth=font->CalcTextSizeA(size,FLT_MAX,0,"...").x;
 if(width<=suffixWidth)return;
 const float length=font->CalcTextSizeA(size,width-suffixWidth,0,text.c_str(),nullptr,&end).x;
 if(end&&end>text.c_str())draw->AddText(font,size,pos,color,text.c_str(),end);
 draw->AddText(font,size,ImVec2(pos.x+length,pos.y),color,"...");
}

void drawGameProfile(float scale) {
 if(gameVersionLine.empty()&&buildName.empty())return;
 const ImVec2 top=ImGui::GetCursorScreenPos();
 const float width=ImGui::GetContentRegionAvail().x;
 if(width<=0)return;
 auto p=[&](float x,float y){return ImVec2(top.x+x*scale,top.y+y*scale);};
 auto* draw=ImGui::GetWindowDrawList();
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+54*scale),ImGui::GetColorU32(IM_COL32(30,30,30,235)),10*scale);
 const ImVec2 tile=p(9,9),tileEnd=p(45,45);
 draw->AddRectFilled(tile,tileEnd,ImGui::GetColorU32(IM_COL32(49,49,49,255)),7*scale);
 if(buildIconTexture){
  draw->AddImageRounded((ImTextureID)(intptr_t)buildIconTexture,tile,tileEnd,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(IM_COL32_WHITE),7*scale);
 }else{
  drawBuildPreset(draw,tile,scale);
 }
 const float available=width-66*scale;
 drawFittedText(draw,headingFont,17*scale,p(55,7),available,ImGui::GetColorU32(ImGuiCol_Text),buildName.empty()?"Minecraft":buildName);
 drawFittedText(draw,ImGui::GetFont(),14*scale,p(55,29),available,ImGui::GetColorU32(ImGuiCol_TextDisabled),gameVersionLine);
 ImGui::Dummy(ImVec2(width,54*scale));
}

std::string sessionTimeText() {
 const auto minutes = (GetTickCount64() - sessionStarted) / 60000;
 if (minutes == 0) return OverlayText("Меньше минуты");
 const int language=OverlayLanguage();
 if (language != 1 && language != 2) {
  return std::to_string(minutes) + " " + (minutes==1?OverlayText("минута"):OverlayText("минут"));
 }
 const auto lastDigit = minutes % 10;
 const auto lastTwo = minutes % 100;
 const char* unit = OverlayText("минут");
 if (lastTwo < 11 || lastTwo > 14) {
  if (lastDigit == 1) unit = OverlayText("минута");
  else if (lastDigit >= 2 && lastDigit <= 4) unit = OverlayText("минуты");
 }
 return std::to_string(minutes) + " " + unit;
}

void socialAvatar(const SintetSocial::Person& person,float scale) {
 static std::unordered_map<std::string,GLuint> textures;
 GLuint texture=0;
 auto existing=textures.find(person.avatar);
 if(existing!=textures.end()){
  texture=existing->second;
 }else if(!person.avatar.empty()){
  auto pixels=SintetSocial::AvatarPixels(person);
  if(!pixels.empty()&&textures.size()<256){
   texture=uploadAvatar(pixels);
   if(texture)textures.emplace(person.avatar,texture);
  }
 }
 const ImVec2 start=ImGui::GetCursorScreenPos();
 const ImVec2 end(start.x+36*scale,start.y+36*scale);
 auto* draw=ImGui::GetWindowDrawList();
 if(texture){
  draw->AddImageRounded((ImTextureID)(intptr_t)texture,start,end,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(ImGuiCol_Text),7*scale);
 }else{
  draw->AddRectFilled(start,end,ImGui::GetColorU32(ImVec4(.21f,.21f,.21f,1)),7*scale);
  const char initial[]={person.name.empty()?'?':person.name[0],0};
  const float fontSize=16*scale;
  auto* font=ImGui::GetFont();
  const ImVec2 size=font->CalcTextSizeA(fontSize,FLT_MAX,0,initial);
  draw->AddText(font,fontSize,ImVec2(start.x+(36*scale-size.x)*.5f,start.y+(36*scale-size.y)*.5f),ImGui::GetColorU32(ImGuiCol_Text),initial);
 }
 ImGui::Dummy(ImVec2(36*scale,36*scale));
 ImGui::SameLine();
}

void drawAvatar(ImDrawList* draw,ImVec2 p,float scale) {
 const ImVec2 end(p.x+36*scale,p.y+36*scale);
 if(avatarTexture)draw->AddImageRounded((ImTextureID)(intptr_t)avatarTexture,p,end,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(ImVec4(1,1,1,1)),7*scale);
 else {
  // A local profile still gets a recognizable default skin head.
  const unsigned int colors[]={0x33251c,0x453020,0xb78662,0xcc9a73,0xeeeeee,0x5551a0,0x53392b};
  const char* face[]={"00000000","01111110","12222221","23333332","24433442","25533552","23366332","22622622"};
  for(int y=0;y<8;y++)for(int x=0;x<8;x++){
   unsigned int c=colors[face[y][x]-'0'];
   draw->AddRectFilled(ImVec2(p.x+x*4.5f*scale,p.y+y*4.5f*scale),ImVec2(p.x+(x+1)*4.5f*scale,p.y+(y+1)*4.5f*scale),ImGui::GetColorU32(ImVec4(((c>>16)&255)/255.f,((c>>8)&255)/255.f,(c&255)/255.f,1)));
  }
 }
 ImVec2 dot(end.x-1*scale,end.y-1*scale);
 draw->AddCircleFilled(dot,6*scale,ImGui::GetColorU32(ImVec4(.12f,.12f,.12f,1)));
 draw->AddCircleFilled(dot,4*scale,ImGui::GetColorU32(ImVec4(.47f,.78f,.55f,1)));
}

// Icons are drawn as curves at the current DPI, rather than font symbols.
void icon(ImDrawList* d, ImVec2 p, int kind, float s, ImU32 color) {
 auto v=[&](float x,float y){return ImVec2(p.x+x*s,p.y+y*s);};
 const float line=1.7f*s;
 if(kind==0){
  d->AddCircle(v(8,6),3.3f*s,color,24,line);
  d->AddBezierCubic(v(1,19),v(1,10),v(15,10),v(15,19),color,line);
  d->AddCircle(v(18,8),2.7f*s,color,24,line);
  d->AddBezierCubic(v(17,13),v(22,12),v(24,16),v(23,19),color,line);
 }else if(kind==1){
  d->AddRect(v(2,7),v(22,21),color,3*s,0,line);
  d->AddLine(v(8,7),v(8,2),color,line);d->AddLine(v(16,7),v(16,2),color,line);
  d->AddLine(v(8,2),v(16,2),color,line);
 }else{
  d->AddCircle(v(12,12),3*s,color,24,line);
  d->AddBezierCubic(v(6,5),v(0,8),v(0,16),v(6,19),color,line);
  d->AddBezierCubic(v(18,5),v(24,8),v(24,16),v(18,19),color,line);
 }
}
void logo(ImDrawList* d, ImVec2 p, float s, ImU32 color) {
 auto v=[&](float x,float y){return ImVec2(p.x+x*s,p.y+y*s);};
 d->AddBezierCubic(v(23,5),v(18,-1),v(2,1),v(3,10),color,3.5f*s);
 d->AddBezierCubic(v(3,10),v(4,17),v(23,9),v(23,19),color,3.5f*s);
 d->AddBezierCubic(v(23,19),v(23,27),v(6,29),v(2,22),color,3.5f*s);
}
void socialTitle(const char* title){ImGui::PushFont(headingFont);ImGui::TextUnformatted(title);ImGui::PopFont();}
void socialButton(const char* label,const char* action,const std::string& target=""){
 if(ImGui::Button(label))SintetSocial::Action(action,target);
}
std::string invitationDetails(const SintetSocial::Invite& invite){
 std::string text=invite.game.empty()?"":"Minecraft "+invite.game;
 if(!invite.build.empty()&&invite.build!=text){if(!text.empty())text+=" · ";text+=invite.build;}
 return text;
}
void drawSocialContent(const SintetSocial::State& state,float height) {
 static bool dispatched=false;
 const bool nowDispatched=state.connection=="Minecraft подключается к миру";
 if(nowDispatched&&!dispatched&&visible)closePanel();
 dispatched=nowDispatched;
 const float scale=interfaceScale;auto px=[scale](float x){return x*scale;};
 if(state.offline){
  ImGui::Dummy(ImVec2(0,std::max(0.f,(height-px(140))*.5f)));
  socialTitle(OverlayText("Без доступа к интернету"));
  return;
 }
 if(!state.enabled){
  ImGui::Dummy(ImVec2(0,std::max(0.f,(height-px(140))*.5f)));
  socialTitle(OverlayText("Друзья в Sintet ID"));ImGui::TextWrapped(OverlayText("Войдите в аккаунт Sintet ID в лаунчере"));
  return;
 }
 if(!state.error.empty()){ImGui::PushStyleColor(ImGuiCol_Text,ImVec4(.88f,.65f,.52f,1));ImGui::TextWrapped("%s",OverlayText(state.error.c_str()));ImGui::PopStyleColor();ImGui::Spacing();}
 else if(state.reconnecting){ImGui::TextDisabled(OverlayText("Восстанавливаем связь с Sintet ID…"));ImGui::Spacing();}
 ImGui::BeginDisabled(state.busy||!visible);
 if(section==0){
  for(const auto& invite:state.invites){
   ImGui::PushID(invite.id.c_str());socialTitle(invite.sender.name.c_str());ImGui::TextDisabled(OverlayText("Приглашает в лобби"));
   const auto details=invitationDetails(invite);
   if(!details.empty())ImGui::TextWrapped("%s",details.c_str());
   socialButton(OverlayText("Принять"),"invite-accept",invite.id);ImGui::SameLine();socialButton(OverlayText("Отклонить"),"invite-decline",invite.id);ImGui::Separator();ImGui::PopID();
  }
  for(const auto& f:state.friends){
   ImGui::PushID(f.person.id.c_str());
   ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(12),px(11)));
   ImGui::PushStyleVar(ImGuiStyleVar_ItemSpacing,ImVec2(px(9),px(6)));
   ImGui::PushStyleVar(ImGuiStyleVar_ChildRounding,px(11));
   ImGui::PushStyleColor(ImGuiCol_ChildBg,ImVec4(.115f,.115f,.12f,.95f));
   const auto cardFlags=ImGuiChildFlags_AlwaysUseWindowPadding|ImGuiChildFlags_AutoResizeY|ImGuiChildFlags_AlwaysAutoResize;
   if(ImGui::BeginChild("Friend",ImVec2(0,0),cardFlags,SintetLayout::FixedCard)){
    socialAvatar(f.person,scale);
    ImGui::BeginGroup();
    const float nameWidth=ImGui::GetContentRegionAvail().x;
    drawFittedText(ImGui::GetWindowDrawList(),headingFont,headingFont->FontSize,ImGui::GetCursorScreenPos(),
                   nameWidth,ImGui::GetColorU32(ImGuiCol_Text),f.person.name);
    ImGui::Dummy(ImVec2(0,headingFont->FontSize));
    const bool inLobby=std::any_of(state.members.begin(),state.members.end(),[&](const auto& member){return member.person.id==f.person.id;});
    if(f.status=="pending"){
     ImGui::TextDisabled(f.incoming?OverlayText("Заявка в друзья"):OverlayText("Заявка отправлена"));
     socialButton(f.incoming?OverlayText("Отклонить"):OverlayText("Отменить"),"friend-remove",f.person.id);
    }else{
     std::string status=OverlayText("Не в сети");
     if(inLobby||f.presence=="lobby")status=OverlayText("В лобби");
     else if(f.presence=="singleplayer")status=OverlayText("Одиночный мир");
     else if(f.presence=="multiplayer"){
      status=OverlayText("Мультиплеер");
      if(!f.server.empty())status+=" · "+f.server;
     }
     else if(f.presence=="world")status=OverlayText("В мире");
     else if(f.presence=="menu")status=OverlayText("В главном меню");
     else if(f.presence=="launcher")status=OverlayText("В лаунчере");
     drawFittedText(ImGui::GetWindowDrawList(),ImGui::GetFont(),ImGui::GetFontSize(),ImGui::GetCursorScreenPos(),
                    ImGui::GetContentRegionAvail().x,ImGui::GetColorU32(ImGuiCol_TextDisabled),status);
     ImGui::Dummy(ImVec2(0,ImGui::GetTextLineHeight()));
     if(inLobby||!f.presence.empty()){
      ImGui::BeginDisabled(inLobby);
      socialButton(OverlayText("Пригласить"),"invite",f.person.id);
      ImGui::EndDisabled();
      const auto& style=ImGui::GetStyle();
      const float removeWidth=ImGui::CalcTextSize(OverlayText("Удалить")).x+style.FramePadding.x*2+style.ItemSpacing.x;
      if(ImGui::GetItemRectMax().x+removeWidth<=ImGui::GetCursorScreenPos().x+ImGui::GetContentRegionAvail().x)ImGui::SameLine();
     }
     socialButton(OverlayText("Удалить"),"friend-remove",f.person.id);
    }
    ImGui::EndGroup();
   }
   ImGui::EndChild();
   ImGui::PopStyleColor();
   ImGui::PopStyleVar(3);
   ImGui::PopID();
  }
  if(state.friends.empty()&&state.invites.empty()){
   float y=std::max(px(10),(height-px(210))*.5f);ImGui::Dummy(ImVec2(0,y));
   float width=ImGui::GetWindowWidth();ImVec2 center(ImGui::GetWindowPos().x+width*.5f,ImGui::GetCursorScreenPos().y+px(25));
   auto* draw=ImGui::GetWindowDrawList();draw->AddCircleFilled(center,px(29),ImGui::GetColorU32(ImVec4(.28f,.44f,.32f,.12f)));draw->AddCircle(center,px(24),ImGui::GetColorU32(ImVec4(.5f,.73f,.56f,.3f)),48,px(1));
   icon(draw,ImVec2(center.x-px(12),center.y-px(12)),0,scale,ImGui::GetColorU32(ImVec4(.72f,.81f,.74f,1)));
   ImGui::Dummy(ImVec2(0,px(60)));
   ImGui::PushFont(headingFont);ImGui::SetCursorPosX((width-ImGui::CalcTextSize(OverlayText("Пока нет друзей")).x)*.5f);ImGui::TextUnformatted(OverlayText("Пока нет друзей"));ImGui::PopFont();
   ImGui::SetCursorPosX((width-ImGui::CalcTextSize(OverlayText("Друзья появятся здесь")).x)*.5f);ImGui::TextDisabled(OverlayText("Друзья появятся здесь"));
   const char* hint=OverlayText("Добавляйте друзей в лаунчере");
   ImGui::SetCursorPosX((width-ImGui::CalcTextSize(hint).x)*.5f);
   ImGui::TextDisabled("%s",hint);
  }
 }else if(section==1){
  socialTitle(OverlayText("Играть вместе"));
  if(!state.room.empty()||state.localPort){
   ImGui::SameLine();
   ImGui::PushFont(titleFont);
   ImGui::Text("%d / 4",state.members.empty()?state.connected+1:(int)state.members.size());
   ImGui::PopFont();
   if(state.localPort&&state.worldReady){
    if(ImGui::Button(OverlayText("Скопировать адрес"))){
     char address[40];sprintf_s(address,"127.0.0.1:%d",state.localPort);ImGui::SetClipboardText(address);
    }
   if(!state.inWorld&&!state.gameJoining){
    ImGui::SameLine();
   ImGui::BeginDisabled(!state.gameReady||(!state.packPhase.empty()&&state.packPhase!="ready"));
   ImGui::PushStyleColor(ImGuiCol_Button,ImVec4(.15f,.33f,.23f,1));
   ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ImVec4(.19f,.42f,.29f,1));
   ImGui::PushStyleColor(ImGuiCol_ButtonActive,ImVec4(.22f,.47f,.33f,1));
    if(ImGui::Button("##auto-join",ImVec2(px(35),ImGui::GetFrameHeight())))SintetSocial::Action("connect-game");
   ImGui::PopStyleColor(3);
    ImGui::EndDisabled();
    const auto corner=ImGui::GetItemRectMin();
    const ImVec2 center(corner.x+px(17),corner.y+ImGui::GetFrameHeight()*.5f);
    const ImVec2 arrow[]={{center.x-px(4),center.y-px(6)},{center.x+px(6),center.y},{center.x-px(4),center.y+px(6)}};
    ImGui::GetWindowDrawList()->AddTriangleFilled(arrow[0],arrow[1],arrow[2],ImGui::GetColorU32(state.gameReady?ImGuiCol_Text:ImGuiCol_TextDisabled));
    if(ImGui::IsItemHovered(ImGuiHoveredFlags_AllowWhenDisabled))ImGui::SetTooltip("%s",state.gameReady?OverlayText("Автоматически войти в мир"):OverlayText("Запустите Minecraft из Sintet для автовхода"));
   }
   }
   if(state.preGameLobby&&!state.leader&&!state.inWorld){
    bool automatic=state.autoConnect;
    if(ImGui::Checkbox(OverlayText("Подключиться, когда мир готов"),&automatic))SintetSocial::Action("auto-connect",automatic?"true":"false");
    if(ImGui::IsItemHovered())ImGui::SetTooltip(OverlayText("Если запущена другая сборка, Minecraft перезапустится"));
   }
   if(!state.leader&&state.packDigest.empty()&&!state.busy)socialButton(OverlayText("Восстановить соединение"),"reconnect-lobby");
   if(!state.packPhase.empty()){
    const bool required=state.packPhase=="required"||state.packPhase=="error";
    const bool working=state.packPhase=="downloading"||state.packPhase=="stopping"||state.packPhase=="installing"||state.packPhase=="launching"||state.packPhase=="checking";
    ImGui::Spacing();
    if(required){
     ImGui::TextColored(ImVec4(.9f,.58f,.58f,1),"%s",state.packMissing?OverlayText("Нужна сборка друга"):OverlayText("Нужен перезапуск"));
     if(state.packMissing)ImGui::TextDisabled(OverlayText("%d файлов · %s"),state.packMissing,SintetLayout::TransferSize(state.packBytes).c_str());
     if(!state.packError.empty()){ImGui::TextDisabled(OverlayText("Не удалось подготовить сборку"));if(ImGui::IsItemHovered())ImGui::SetTooltip("%s",OverlayText(state.packError.c_str()));}
     if(!state.packDigest.empty()){
      ImGui::PushStyleColor(ImGuiCol_Button,ImVec4(.28f,.14f,.14f,1));ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ImVec4(.37f,.19f,.19f,1));
      if(ImGui::Button(state.preGameLobby?OverlayText("Подготовить сборку"):state.packRestart?OverlayText("Скачать и перезапустить"):OverlayText("Скачать и войти"),ImVec2(-1,px(30))))ImGui::OpenPopup(OverlayText("Сборка друга"));ImGui::PopStyleColor(2);
     }
    }
    if(working){
     const char* title=state.packPhase=="stopping"?OverlayText("Сохраняем и закрываем Minecraft"):state.packPhase=="installing"?OverlayText("Готовим Minecraft"):state.packPhase=="launching"?OverlayText("Запускаем Minecraft"):state.packPhase=="checking"?OverlayText("Сверяем сборку"):OverlayText("Скачиваем сборку");
     ImGui::TextWrapped("%s",title);
     const float fraction=state.packTotal?(float)state.packDone/state.packTotal:-(float)ImGui::GetTime()*.25f;
     ImGui::ProgressBar(fraction,ImVec2(-1,px(5)),"");
     if(state.packTotal&&state.packPhase!="installing")ImGui::TextDisabled("%s / %s",SintetLayout::TransferSize(state.packDone).c_str(),SintetLayout::TransferSize(state.packTotal).c_str());
     if(state.packPhase!="checking"&&state.packPhase!="launching")socialButton(OverlayText("Отменить"),"cancel-pack");
    }
    if(state.packPhase=="hosting")ImGui::TextDisabled(OverlayText("Сборка доступна гостям"));
    ImGui::SetNextWindowSize(ImVec2(px(355),0),ImGuiCond_Appearing);
    if(ImGui::BeginPopupModal(OverlayText("Сборка друга"),nullptr,ImGuiWindowFlags_AlwaysAutoResize|ImGuiWindowFlags_NoSavedSettings)){
     ImGui::PushTextWrapPos(ImGui::GetCursorPosX()+px(315));
     ImGui::TextWrapped("Minecraft %s · %s",state.packGame.c_str(),state.packLoader.c_str());
     ImGui::Spacing();ImGui::TextWrapped("%s",state.preGameLobby?OverlayText("Файлы сохранятся отдельной сборкой. Текущая игра продолжит работать. Подключиться можно будет после открытия мира лидером."):OverlayText("Файлы сохранятся отдельной сборкой. После загрузки Minecraft корректно закроется и запустится у друга. Сохрани текущий мир."));
     ImGui::Spacing();ImGui::TextDisabled(OverlayText("Продолжай, если доверяешь модам хоста."));ImGui::PopTextWrapPos();
     if(ImGui::Button(OverlayText("Отмена")))ImGui::CloseCurrentPopup();ImGui::SameLine();
     if(ImGui::Button(state.preGameLobby?OverlayText("Подготовить сборку"):OverlayText("Скачать и перезапустить"))){SintetSocial::Action("sync-pack",state.packDigest);ImGui::CloseCurrentPopup();}
     ImGui::EndPopup();
    }
    ImGui::Spacing();ImGui::Separator();ImGui::Spacing();
   }
   for(const auto& member:state.members){
    ImGui::PushID(member.person.id.c_str());
    ImGui::BeginGroup();
    socialAvatar(member.person,scale);
    ImGui::BeginGroup();
    ImGui::PushFont(titleFont);
    ImGui::TextUnformatted(member.person.name.c_str());
    ImGui::PopFont();
    if(member.leader){
     ImGui::SameLine();
     const ImVec2 crown=ImGui::GetCursorScreenPos();
     const ImVec2 points[]={{crown.x+px(1),crown.y+px(5)},{crown.x+px(5),crown.y+px(8)},
       {crown.x+px(9),crown.y+px(2)},{crown.x+px(13),crown.y+px(8)},
       {crown.x+px(17),crown.y+px(5)},{crown.x+px(15),crown.y+px(13)},{crown.x+px(3),crown.y+px(13)}};
     ImGui::GetWindowDrawList()->AddConvexPolyFilled(points,7,ImGui::GetColorU32(ImVec4(.91f,.69f,.29f,1)));
     ImGui::Dummy(ImVec2(px(18),px(16)));
    }
    const char* preparation=member.preparation=="ready"?OverlayText("Готов"):member.preparation=="world"?OverlayText("В мире"):member.preparation=="downloading"?OverlayText("Скачивает сборку"):member.preparation=="installing"?OverlayText("Устанавливает Minecraft"):member.preparation=="required"?OverlayText("Нужна сборка"):member.preparation=="error"?OverlayText("Нужна повторная попытка"):member.preparation=="launching"?OverlayText("Запускает игру"):OverlayText("В лобби");
    if(member.leader)ImGui::TextDisabled(state.worldReady?OverlayText("Мир открыт"):OverlayText("Лидер лобби"));
    else if(member.preparation=="downloading"||member.preparation=="installing")ImGui::TextDisabled("%s · %d%%",preparation,member.percent);
    else ImGui::TextDisabled("%s",preparation);
    ImGui::EndGroup();
    if(state.leader&&!member.leader){
     ImGui::SameLine(ImGui::GetWindowWidth()-px(49));
     if(ImGui::Button("×",ImVec2(px(34),px(35))))SintetSocial::Action("kick",member.person.id);
     if(ImGui::IsItemHovered())ImGui::SetTooltip(OverlayText("Исключить из лобби"));
    }
    ImGui::EndGroup();
    ImGui::Dummy(ImVec2(0,px(8)));
    ImGui::PopID();
   }
   ImGui::TextDisabled(OverlayText("Код комнаты"));ImGui::PushFont(titleFont);ImGui::TextUnformatted(state.room.c_str());ImGui::PopFont();
   if(ImGui::Button(OverlayText("Скопировать код"),ImVec2(-1,px(36))))ImGui::SetClipboardText(state.room.c_str());
   if(state.leader){
    ImGui::PushStyleColor(ImGuiCol_Button,ImVec4(.45f,.15f,.17f,1));
    ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ImVec4(.56f,.20f,.22f,1));
    ImGui::PushStyleColor(ImGuiCol_ButtonActive,ImVec4(.62f,.23f,.25f,1));
    socialButton(OverlayText("Закрыть лобби"),"close");
    ImGui::PopStyleColor(3);
   }else socialButton(OverlayText("Выйти из лобби"),"close");
  }else{
   ImGui::TextWrapped("%s",state.preGameLobby?OverlayText("Соберитесь в лобби и подготовьте сборку. Мир можно открыть для сети позже."):OverlayText("Откройте одиночный мир для сети в Minecraft"));
   if(ImGui::Button(OverlayText("Создать комнату"),ImVec2(-1,px(38))))SintetSocial::Action("host");
  }
  if(state.room.empty()&&!state.localPort){
  ImGui::Dummy(ImVec2(0,px(16)));ImGui::Separator();ImGui::Dummy(ImVec2(0,px(10)));
  static char code[7]={0};ImGui::TextUnformatted(OverlayText("Войти по коду"));ImGui::SetNextItemWidth(-1);
  ImGui::InputTextWithHint("##room-code",OverlayText("6 цифр"),code,sizeof(code),ImGuiInputTextFlags_CharsDecimal);
  if(ImGui::Button(OverlayText("Подключиться"),ImVec2(-1,px(38))))SintetSocial::Action("join",code);
  }
 }
 if(!state.connection.empty()){ImGui::Spacing();ImGui::TextWrapped("%s",OverlayText(state.connection.c_str()));}
 if(state.busy){ImGui::Spacing();ImGui::TextDisabled(OverlayText("Подождите..."));}
 ImGui::EndDisabled();
}
ImVec4 linkColor(const SintetSocial::LinkQuality& quality){
 if(quality.level=="poor")return {.97f,.44f,.43f,1};
 if(quality.level=="unstable")return {.97f,.77f,.40f,1};
 return {.69f,.76f,.71f,1};
}

void drawWifi(ImDrawList* draw,ImVec2 center,float scale,ImU32 color){
 for(float radius:{5.f,9.f,13.f}){
  draw->PathArcTo(center,radius*scale,-2.32f,-.82f,12);
  draw->PathStroke(color,0,1.5f*scale);
 }
 draw->AddCircleFilled(center,1.6f*scale,color,10);
}

std::string pingText(const SintetSocial::LinkQuality& quality){
 return quality.ping<0?OverlayText("– мс"):std::to_string(quality.ping)+" "+OverlayText("мс");
}

bool drawNetworkHUD(const SintetSocial::State& state){
 if(!state.quality.available)return false;
 const auto& quality=state.quality;
 const float scale=interfaceScale;
 const auto display=ImGui::GetIO().DisplaySize;
 char loss[32],rate[40];
 if(quality.loss>=0)sprintf_s(loss,"%.0f%%",quality.loss);
 else strcpy_s(loss,"–");
 sprintf_s(rate,OverlayText("%.1f КБ/с"),quality.kbPerSec);
 const std::string label=pingText(quality)+"   ·   "+loss+"   ·   "+rate;
 const float textSize=13*scale;
 auto* font=ImGui::GetFont();
 const float width=std::min(display.x-24*scale,46*scale+font->CalcTextSizeA(textSize,FLT_MAX,0,label.c_str()).x);
 if(width<46*scale)return false;
 const ImVec2 top(display.x-width-12*scale,12*scale);
 const ImVec2 bottom(top.x+width,top.y+28*scale);
 auto* draw=ImGui::GetForegroundDrawList();
 const auto color=linkColor(quality);
 draw->AddRectFilled(top,bottom,IM_COL32(19,19,20,228),7*scale);
 if(quality.level=="unstable"||quality.level=="poor")
  draw->AddRect(top,bottom,ImGui::GetColorU32(ImVec4(color.x,color.y,color.z,.55f)),7*scale);
 drawWifi(draw,ImVec2(top.x+18*scale,top.y+19*scale),.72f*scale,ImGui::GetColorU32(color));
 drawFittedText(draw,font,textSize,ImVec2(top.x+33*scale,top.y+7.5f*scale),width-42*scale,IM_COL32(231,231,233,255),label);
 return true;
}

void drawScreenshotNotice(){
 const char* notice=SintetScreenshot::Notice();
 if(!notice)return;
 const float scale=interfaceScale,textSize=14*scale;
 const auto display=ImGui::GetIO().DisplaySize;
 auto* font=ImGui::GetFont();
 const float width=std::min(display.x-20*scale,font->CalcTextSizeA(textSize,FLT_MAX,0,notice).x+30*scale);
 if(width<=24*scale)return;
 const ImVec2 top((display.x-width)*.5f,display.y-55*scale);
 auto* draw=ImGui::GetForegroundDrawList();
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+32*scale),IM_COL32(22,22,23,241),8*scale);
 drawFittedText(draw,font,textSize,ImVec2(top.x+15*scale,top.y+8*scale),width-28*scale,IM_COL32(235,235,236,255),notice);
}

void drawTransportButton(ImDrawList* draw,ImVec2 cursor,float scale,const SintetMedia::Track& track,
                         SintetMedia::Action action,bool enabled,const char* tooltip){
 using SintetMedia::Action;
 ImGui::SetCursorPos(cursor);
 ImGui::PushID(static_cast<int>(action));
 ImGui::BeginDisabled(!enabled);
 if(ImGui::InvisibleButton("##transport",ImVec2(25*scale,28*scale)))SintetMedia::Control(track.generation,action);
 const auto start=ImGui::GetItemRectMin(),end=ImGui::GetItemRectMax();
 if(ImGui::IsItemHovered())draw->AddRectFilled(start,end,ImGui::GetColorU32(ImVec4(.24f,.24f,.25f,1)),6*scale);
 const auto color=ImGui::GetColorU32(ImGuiCol_Text);
 const ImVec2 center((start.x+end.x)*.5f,(start.y+end.y)*.5f);
 auto p=[&](float x,float y){return ImVec2(center.x+x*scale,center.y+y*scale);};
 if(action==Action::Toggle&&track.playing){
  draw->AddRectFilled(p(-4,-5),p(-1,5),color,scale);
  draw->AddRectFilled(p(1,-5),p(4,5),color,scale);
 }else{
  const float direction=action==Action::Previous?-1.f:1.f;
  draw->AddTriangleFilled(p(-4*direction,-5),p(4*direction,0),p(-4*direction,5),color);
  if(action!=Action::Toggle)draw->AddLine(p(6*direction,-5),p(6*direction,5),color,1.6f*scale);
 }
 if(ImGui::IsItemHovered(ImGuiHoveredFlags_AllowWhenDisabled))ImGui::SetTooltip("%s",tooltip);
 ImGui::EndDisabled();
 ImGui::PopID();
}

void drawCover(ImDrawList* draw,const SintetMedia::Cover& cover,GLuint texture,ImVec2 start,ImVec2 end,float scale){
 const auto side=static_cast<float>(std::min(cover.width,cover.height));
 const ImVec2 inset((1-side/cover.width)*.5f,(1-side/cover.height)*.5f);
 draw->AddImageRounded((ImTextureID)(intptr_t)texture,start,end,inset,ImVec2(1-inset.x,1-inset.y),
                       ImGui::GetColorU32(IM_COL32_WHITE),7*scale);
}

void drawMedia(float scale){
 using SintetMedia::Action;
 const auto track=SintetMedia::Snapshot();
 static std::uint64_t seeking=0;
 static GLuint coverTexture=0;
 static std::shared_ptr<const SintetMedia::Cover> uploaded;
 if(!visible||seeking!=track.generation)seeking=0;
 if(track.title.empty())return;
 auto px=[scale](float value){return value*scale;};
 const auto top=ImGui::GetCursorScreenPos();
 const auto cursor=ImGui::GetCursorPos();
 const float width=ImGui::GetContentRegionAvail().x;
 if(width<px(181))return;
 auto* draw=ImGui::GetWindowDrawList();
 auto p=[&](float x,float y){return ImVec2(top.x+px(x),top.y+px(y));};
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+px(60)),ImGui::GetColorU32(ImVec4(.12f,.12f,.13f,.93f)),px(10));
 draw->AddRectFilled(p(9,9),p(51,51),ImGui::GetColorU32(ImVec4(.19f,.19f,.20f,1)),px(7));
 const auto muted=ImGui::GetColorU32(ImVec4(.69f,.69f,.71f,1));
 if(track.cover&&track.cover!=uploaded){
  if(!coverTexture)glGenTextures(1,&coverTexture);
  if(coverTexture){
   glBindTexture(GL_TEXTURE_2D,coverTexture);
   glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
   glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
   glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,track.cover->width,track.cover->height,0,GL_RGBA,GL_UNSIGNED_BYTE,track.cover->rgba.data());
   glBindTexture(GL_TEXTURE_2D,0);
   uploaded=track.cover;
  }
 }
 if(track.cover&&track.cover==uploaded){
  drawCover(draw,*track.cover,coverTexture,p(9,9),p(51,51),scale);
 }else{
  draw->AddCircleFilled(p(27,35),px(4),muted);
  draw->AddLine(p(30,34),p(30,21),muted,px(1.8f));
  draw->AddLine(p(30,21),p(38,24),muted,px(1.8f));
 }
 const float textWidth=width-px(159);
 drawFittedText(draw,headingFont,px(15),p(62,track.artist.empty()?17:9),textWidth,ImGui::GetColorU32(ImGuiCol_Text),track.title);
 drawFittedText(draw,ImGui::GetFont(),px(12),p(62,27),textWidth,muted,track.artist);
 const float controls=cursor.x+width;
 drawTransportButton(draw,ImVec2(controls-px(89),cursor.y+px(9)),scale,track,Action::Previous,track.canPrevious,OverlayText("Предыдущий трек"));
 drawTransportButton(draw,ImVec2(controls-px(62),cursor.y+px(9)),scale,track,Action::Toggle,track.canToggle,track.playing?OverlayText("Пауза"):OverlayText("Воспроизвести"));
 drawTransportButton(draw,ImVec2(controls-px(35),cursor.y+px(9)),scale,track,Action::Next,track.canNext,OverlayText("Следующий трек"));

 const float lineWidth=width-px(73);
 if(track.end>track.start){
  double position=track.position+(GetTickCount64()-track.measuredAt)*.001*track.rate;
  ImGui::SetCursorPos(ImVec2(cursor.x+px(62),cursor.y+px(40)));
  ImGui::BeginDisabled(!track.canSeek);
  ImGui::InvisibleButton("##media-seek",ImVec2(lineWidth,px(13)));
  if(ImGui::IsItemActivated())seeking=track.generation;
  const bool active=ImGui::IsItemActive();
  const bool released=ImGui::IsItemDeactivated()&&ImGui::IsMouseReleased(ImGuiMouseButton_Left);
  if(track.canSeek&&seeking&&(active||released)){
   const auto ratio=std::clamp((ImGui::GetIO().MousePos.x-p(62,0).x)/lineWidth,0.f,1.f);
   position=std::clamp(track.start+ratio*(track.end-track.start),track.seekMin,track.seekMax);
   if(released)SintetMedia::Control(seeking,Action::Seek,position);
  }
  if(!active)seeking=0;
  const bool highlight=ImGui::IsItemHovered()||active;
  ImGui::EndDisabled();
  const float fill=static_cast<float>(std::clamp((position-track.start)/(track.end-track.start),0.0,1.0))*lineWidth;
  const ImVec2 start=p(62,45),end(start.x+lineWidth,start.y+px(3));
  draw->AddRectFilled(start,end,ImGui::GetColorU32(ImVec4(.27f,.27f,.29f,1)),px(1.5f));
  if(fill>0)draw->AddRectFilled(start,ImVec2(start.x+fill,end.y),ImGui::GetColorU32(ImVec4(.72f,.72f,.75f,1)),px(1.5f));
  if(highlight)draw->AddCircleFilled(ImVec2(start.x+fill,start.y+px(1.5f)),px(3.5f),ImGui::GetColorU32(ImGuiCol_Text));
 }
 ImGui::SetCursorPos(cursor);
 ImGui::Dummy(ImVec2(width,px(60)));
 ImGui::Dummy(ImVec2(0,px(5)));
}

void drawInvitation(const SintetSocial::State& state,bool hasNetworkHUD){
 static std::string last;static ULONGLONG started=0;
 if(state.invites.empty())return;
 const auto& invite=state.invites[0];if(last!=invite.id){last=invite.id;started=GetTickCount64();}
 float elapsed=(GetTickCount64()-started)/1000.f;if(elapsed>10&&!visible)return;
 float scale=interfaceScale,fade=easeOut(elapsed/.3f);auto& io=ImGui::GetIO();
 ImGui::SetNextWindowPos(ImVec2(io.DisplaySize.x-330*scale+(1-fade)*350*scale,(visible?70:hasNetworkHUD?48:24)*scale));ImGui::SetNextWindowSize(ImVec2(310*scale,0));
 ImGui::PushStyleVar(ImGuiStyleVar_WindowRounding,6*scale);ImGui::PushStyleVar(ImGuiStyleVar_Alpha,fade);
 ImGui::Begin("Game invitation",nullptr,ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|ImGuiWindowFlags_AlwaysAutoResize|ImGuiWindowFlags_NoFocusOnAppearing|(visible?0:ImGuiWindowFlags_NoInputs));
 socialTitle(invite.sender.name.c_str());ImGui::TextUnformatted(OverlayText("Приглашает вас в лобби"));
 const auto details=invitationDetails(invite);
 if(!details.empty())ImGui::TextWrapped("%s",details.c_str());
 if(visible){ImGui::BeginDisabled(state.busy);socialButton(OverlayText("Принять"),"invite-accept",invite.id);ImGui::SameLine();socialButton(OverlayText("Отклонить"),"invite-decline",invite.id);ImGui::EndDisabled();}
 else ImGui::TextDisabled(OverlayText("%s · Ответить"),overlayHotkey.c_str());
 ImGui::End();ImGui::PopStyleVar(2);
}
void drawPanel(const SintetSocial::State& state) {
 ImGuiIO& io=ImGui::GetIO();
 const float step=std::min(io.DeltaTime,.05f)/.22f;
 panelAnimation=std::clamp(panelAnimation+(visible?step:-step),0.0f,1.0f);
 if(panelAnimation<=0)return;
 const float progress=easeOut(panelAnimation),scale=interfaceScale;
 auto px=[scale](float value){return value*scale;};
 const ImVec2 size(std::min(px(414),io.DisplaySize.x),io.DisplaySize.y);
 const ImVec2 position(-(1-progress)*size.x,0);
 drawGlass(position,size);
 ImDrawList* behind=ImGui::GetBackgroundDrawList();
 behind->AddRectFilled(ImVec2(0,0),io.DisplaySize,IM_COL32(0,0,0,(int)(102*progress)));
 behind->AddRectFilled(position,ImVec2(position.x+size.x,size.y),IM_COL32(18,18,20,(int)(191*progress)),px(16),ImDrawFlags_RoundCornersRight);
 behind->AddLine(ImVec2(position.x+size.x-1,px(16)),ImVec2(position.x+size.x-1,size.y-px(16)),IM_COL32(255,255,255,(int)(20*progress)),px(1));
 ImGui::SetNextWindowPos(position,ImGuiCond_Always);
 ImGui::SetNextWindowSize(size,ImGuiCond_Always);
 ImGui::PushStyleVar(ImGuiStyleVar_Alpha,progress);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowBorderSize,0);
 ImGui::PushStyleColor(ImGuiCol_Border,ImVec4(.35f,.58f,.41f,.32f));
 ImGui::Begin("Sintet Overlay",nullptr,ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|ImGuiWindowFlags_NoMove|ImGuiWindowFlags_NoBackground|ImGuiWindowFlags_NoScrollbar|ImGuiWindowFlags_NoScrollWithMouse);
 ImDrawList* draw=ImGui::GetWindowDrawList();
 const ImVec2 start=ImGui::GetCursorScreenPos();
 const ImU32 white=ImGui::GetColorU32(ImVec4(.93f,.93f,.93f,1));
 logo(draw,start,scale,white);
 ImGui::SetCursorPosX(px(63));
 ImGui::PushFont(titleFont);ImGui::TextUnformatted("Sintet");ImGui::PopFont();
 const float timeSize = px(14);
 const ImVec2 timePosition(ImGui::GetItemRectMax().x + px(17),
     ImGui::GetItemRectMin().y + (ImGui::GetItemRectSize().y - timeSize) * .5f);
 const float timeWidth = position.x + size.x - px(62) - timePosition.x;
 drawFittedText(draw, ImGui::GetFont(), timeSize, timePosition, timeWidth,
     ImGui::GetColorU32(ImVec4(.68f,.68f,.68f,1)), sessionTimeText());
 ImGui::SameLine(size.x-px(52));
 ImVec2 cross=ImGui::GetCursorScreenPos();
 if(ImGui::InvisibleButton("Close",ImVec2(px(28),px(28))))closePanel();
 draw->AddCircleFilled(ImVec2(cross.x+px(14),cross.y+px(14)),px(14),ImGui::GetColorU32(ImGui::IsItemHovered()?ImVec4(.23f,.23f,.23f,1):ImVec4(.16f,.16f,.16f,1)));
 ImU32 muted=ImGui::GetColorU32(ImVec4(.64f,.64f,.64f,1));
 draw->AddLine(ImVec2(cross.x+px(10),cross.y+px(10)),ImVec2(cross.x+px(18),cross.y+px(18)),muted,px(1.6f));
 draw->AddLine(ImVec2(cross.x+px(18),cross.y+px(10)),ImVec2(cross.x+px(10),cross.y+px(18)),muted,px(1.6f));
 ImGui::Dummy(ImVec2(0,px(18)));
 drawMedia(scale);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(16),px(10)));
 ImGui::PushStyleVar(ImGuiStyleVar_ItemSpacing,ImVec2(px(8),px(3)));
 ImGui::BeginChild("Player",ImVec2(0,px(70)),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::FixedCard);
 char nick[128]={0};GetEnvironmentVariableA("SINTET_OVERLAY_PLAYER",nick,128);
 ImVec2 avatar=ImGui::GetCursorScreenPos();avatar.y+=px(6);
 draw=ImGui::GetWindowDrawList();drawAvatar(draw,avatar,scale);
 ImGui::SetCursorPos(ImVec2(px(66),px(10)));
 drawFittedText(draw,headingFont,px(20),ImGui::GetCursorScreenPos(),ImGui::GetWindowWidth()-px(116),white,*nick?nick:"Player");
 ImGui::Dummy(ImVec2(0,px(20)));
 ImGui::SetCursorPosX(px(66));ImGui::TextDisabled(OverlayText("В игре"));
 ImGui::SetCursorPos(ImVec2(ImGui::GetWindowWidth()-px(40),px(20)));
 ImVec2 copy=ImGui::GetCursorScreenPos();
 if(ImGui::InvisibleButton("Copy player ID",ImVec2(px(26),px(26)))){
  char id[80]={0};GetEnvironmentVariableA("SINTET_OVERLAY_ID",id,sizeof(id));
  if(*id){ImGui::SetClipboardText(id);copiedUntil=(float)ImGui::GetTime()+2;}
 }
 bool hovered=ImGui::IsItemHovered();
 if(hovered)ImGui::SetTooltip((float)ImGui::GetTime()<copiedUntil?OverlayText("ID скопирован"):OverlayText("Скопировать ID"));
 ImU32 copyColor=ImGui::GetColorU32(hovered?ImVec4(.9f,.9f,.9f,1):ImVec4(.55f,.55f,.55f,1));
 draw->AddRect(ImVec2(copy.x+px(6),copy.y+px(5)),ImVec2(copy.x+px(17),copy.y+px(17)),copyColor,px(2),0,px(1.4f));
 draw->AddRect(ImVec2(copy.x+px(10),copy.y+px(9)),ImVec2(copy.x+px(21),copy.y+px(21)),copyColor,px(2),0,px(1.4f));
 ImGui::EndChild();ImGui::PopStyleVar(2);
 drawGameProfile(scale);
 const char* tabs[]={OverlayText("Друзья"),OverlayText("Лобби")};
 for(int i=0;i<2;i++){
  ImVec2 top=ImGui::GetCursorScreenPos();
  bool selected=section==i;
  if(ImGui::InvisibleButton(tabs[i],ImVec2(-1,px(46))))section=i;
  draw=ImGui::GetWindowDrawList();
  if(selected||ImGui::IsItemHovered())draw->AddRectFilled(top,ImVec2(top.x+ImGui::GetItemRectSize().x,top.y+px(46)),ImGui::GetColorU32(selected?ImVec4(.16f,.25f,.20f,1):ImVec4(.15f,.16f,.16f,1)),px(12));
  ImU32 color=ImGui::GetColorU32(selected?ImVec4(.93f,.93f,.93f,1):ImVec4(.73f,.75f,.74f,1));
  icon(draw,ImVec2(top.x+px(14),top.y+px(12)),i,scale*.85f,color);
  drawFittedText(draw,headingFont,px(20),ImVec2(top.x+px(49),top.y+px(13)),ImGui::GetItemRectSize().x-px(63),color,tabs[i]);
 }
 ImGui::Dummy(ImVec2(0,px(10)));
 float contentHeight=std::max(px(85),ImGui::GetContentRegionAvail().y-px(34));
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(14),px(14)));
 ImGui::PushStyleColor(ImGuiCol_ChildBg,ImVec4(.07f,.07f,.07f,.45f));
 ImGui::PushStyleVar(ImGuiStyleVar_ScrollbarSize,px(5));
 ImGui::PushStyleColor(ImGuiCol_ScrollbarBg,ImVec4(0,0,0,0));
 ImGui::PushStyleColor(ImGuiCol_ScrollbarGrab,ImVec4(.52f,.52f,.52f,.44f));
 ImGui::PushStyleColor(ImGuiCol_ScrollbarGrabHovered,ImVec4(.66f,.66f,.66f,.72f));
 ImGui::PushStyleColor(ImGuiCol_ScrollbarGrabActive,ImVec4(.73f,.73f,.73f,.85f));
 ImGui::BeginChild("Content",ImVec2(0,contentHeight),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::ScrollableContent);
 drawSocialContent(state,contentHeight);
 ImGui::EndChild();ImGui::PopStyleColor(5);ImGui::PopStyleVar(2);
 ImGui::Dummy(ImVec2(0,px(6)));
 drawFittedText(ImGui::GetWindowDrawList(),ImGui::GetFont(),px(16),ImGui::GetCursorScreenPos(),
                ImGui::GetContentRegionAvail().x,ImGui::GetColorU32(ImGuiCol_TextDisabled),overlayHotkey+"  ·  "+OverlayText("Вернуться в игру"));
 ImGui::Dummy(ImVec2(0,px(16)));
 ImGui::End();
 ImGui::PopStyleColor();ImGui::PopStyleVar(2);
}

// An independent GL context keeps Minecraft's upload and rendering state isolated.
class OverlayContextScope {
 HDC previousDC;
 HGLRC previousContext;
 bool active;
public:
 explicit OverlayContextScope(HDC dc):previousDC(wglGetCurrentDC()),previousContext(wglGetCurrentContext()),active(false){
  if(!overlayGL)overlayGL=wglCreateContext(dc);
  active=overlayGL&&wglMakeCurrent(dc,overlayGL);
 }
 ~OverlayContextScope(){if(active)wglMakeCurrent(previousDC,previousContext);}
 bool ready() const{return active;}
};

void renderFrame(GLFWwindow* current) {
 if(stopping||!wglGetCurrentContext()||(game&&current!=game))return;
 HWND currentWindow=nativeWindow&&current?nativeWindow(current):WindowFromDC(wglGetCurrentDC());
 if(!currentWindow)return;
 const bool foreground=GetForegroundWindow()==currentWindow;
 const int modifiers=((GetAsyncKeyState(VK_SHIFT)&0x8000)?MOD_SHIFT:0)|((GetAsyncKeyState(VK_CONTROL)&0x8000)?MOD_CONTROL:0)|((GetAsyncKeyState(VK_MENU)&0x8000)?MOD_ALT:0);
 const bool toggle=foreground&&modifiers==overlayHotkeyMods&&(GetAsyncKeyState(overlayHotkeyKey)&0x8000)&&!(GetAsyncKeyState(VK_LWIN)&0x8000)&&!(GetAsyncKeyState(VK_RWIN)&0x8000);
 const bool escape=foreground&&(GetAsyncKeyState(VK_ESCAPE)&0x8000);
 const bool capture=foreground&&!toggle&&modifiers==MOD_CONTROL&&(GetAsyncKeyState(VK_F12)&0x8000);
 if(capture&&!previousCapture)SintetScreenshot::Request();
 previousCapture=capture;
 if(context){
  if(toggle&&!previousToggle){if(visible)closePanel();else openPanel();}
  if(escape&&!previousEscape&&visible)closePanel();
 }
 previousToggle=toggle;previousEscape=escape;
 if(!foreground)return;
 // Keep a lightweight frame for invitation notifications even while the drawer is closed.
 if(context&&currentWindow!=window)return;
 if(!context){
  const auto* renderer=glGetString(GL_RENDERER);
  if(renderer)gpuRenderer=reinterpret_cast<const char*>(renderer);
 }
 OverlayContextScope graphics(wglGetCurrentDC());
 if(!graphics.ready())return;
 if(!context){
  window=currentWindow;game=current;context=ImGui::CreateContext();
  ImGuiIO& io=ImGui::GetIO();io.IniFilename=nullptr;io.LogFilename=nullptr;
  const UINT dpi=GetDpiForWindow(window);
  interfaceScale=dpi?(float)dpi/96.0f:1.0f;
  WCHAR fonts[MAX_PATH];
  GetWindowsDirectoryW(fonts,MAX_PATH);
  WCHAR semibold[MAX_PATH],bold[MAX_PATH],selected[32768];
  swprintf_s(semibold,L"%s\\Fonts\\seguisb.ttf",fonts);
  swprintf_s(bold,L"%s\\Fonts\\segoeuib.ttf",fonts);
  const DWORD selectedLength=GetEnvironmentVariableW(L"SINTET_OVERLAY_FONT",selected,32768);
  const bool hasSelected=selectedLength>0&&selectedLength<32768;
  auto loadFont=[&](const wchar_t* path,float size,ImFontConfig config,const ImWchar* ranges){
   const int count=WideCharToMultiByte(CP_UTF8,0,path,-1,nullptr,0,nullptr,nullptr);
   if(count<=0)return static_cast<ImFont*>(nullptr);
   std::vector<char> utf8(count);
   WideCharToMultiByte(CP_UTF8,0,path,-1,utf8.data(),count,nullptr,nullptr);
   if(GetFileAttributesW(path)==INVALID_FILE_ATTRIBUTES)return static_cast<ImFont*>(nullptr);
   return io.Fonts->AddFontFromFileTTF(utf8.data(),size*interfaceScale,&config,ranges);
  };
  auto overlayFont=[&](const wchar_t* fallback,float size){
   ImFontConfig config;
   config.OversampleH=2;
   config.OversampleV=2;
   const ImWchar* ranges=io.Fonts->GetGlyphRangesCyrillic();
   if(hasSelected){
    ImFont* chosen=loadFont(selected,size,config,ranges);
    if(chosen){
     config.MergeMode=true;
     loadFont(fallback,size,config,ranges);
     return chosen;
    }
   }
   return loadFont(fallback,size,config,ranges);
  };
  ImFont* body=overlayFont(semibold,18);
  if(!body)body=io.Fonts->AddFontDefault();
  io.FontDefault=body;
  headingFont=overlayFont(bold,22);
  titleFont=overlayFont(bold,29);
  if(!headingFont)headingFont=body;
  if(!titleFont)titleFont=body;
  ImGui::StyleColorsDark();auto& style=ImGui::GetStyle();
  style.WindowRounding=22;style.ChildRounding=15;style.FrameRounding=12;
  style.WindowBorderSize=0;style.ChildBorderSize=0;
  style.WindowPadding=ImVec2(22,22);style.FramePadding=ImVec2(14,9);style.ItemSpacing=ImVec2(10,10);
  style.AntiAliasedLines=true;style.AntiAliasedFill=true;
  style.Colors[ImGuiCol_WindowBg]=ImVec4(.078f,.078f,.078f,1);
  style.Colors[ImGuiCol_ChildBg]=ImVec4(.115f,.115f,.115f,1);
  style.Colors[ImGuiCol_Button]=ImVec4(.17f,.17f,.18f,1);
  style.Colors[ImGuiCol_ButtonHovered]=ImVec4(.23f,.23f,.24f,1);
  style.Colors[ImGuiCol_ButtonActive]=ImVec4(.27f,.27f,.28f,1);
  style.Colors[ImGuiCol_FrameBg]=ImVec4(.14f,.14f,.14f,1);
  style.Colors[ImGuiCol_FrameBgHovered]=ImVec4(.20f,.20f,.20f,1);
  style.Colors[ImGuiCol_FrameBgActive]=ImVec4(.25f,.25f,.25f,1);
  style.Colors[ImGuiCol_Header]=ImVec4(.18f,.18f,.18f,1);
  style.Colors[ImGuiCol_HeaderHovered]=ImVec4(.24f,.24f,.24f,1);
  style.Colors[ImGuiCol_HeaderActive]=ImVec4(.29f,.29f,.29f,1);
  style.Colors[ImGuiCol_TextSelectedBg]=ImVec4(.40f,.40f,.40f,.55f);
  style.Colors[ImGuiCol_NavCursor]=ImVec4(.70f,.70f,.70f,1);
  style.Colors[ImGuiCol_Text]=ImVec4(.93f,.93f,.93f,1);
  style.Colors[ImGuiCol_TextDisabled]=ImVec4(.72f,.74f,.73f,1);
  style.ScaleAllSizes(interfaceScale);
  if(!ImGui_ImplWin32_InitForOpenGL(window)||!ImGui_ImplOpenGL3_Init("#version 150")){stopping=true;return;}
  originalProc=(WNDPROC)SetWindowLongPtrW(window,GWLP_WNDPROC,(LONG_PTR)input);
  oldCursor=game&&getMode?getMode(game,CursorMode):CursorNormal;
  loadGameProfile();
  SintetWidgets::LoadPreferences();
  openLobbyOnStart=environmentText(L"SINTET_OVERLAY_OPEN_LOBBY")=="true";
  welcomeStarted=GetTickCount64();
  fputs("Sintet Overlay: isolated OpenGL renderer initialized\n",stderr);
 }
 if(visible){if(usingGLFWCursor)inputMode(game,CursorMode,CursorNormal);else{ClipCursor(nullptr);SetCursor(LoadCursorW(nullptr,IDC_ARROW));}}
 ImGui::SetCurrentContext(context);
 ImGui::GetIO().ConfigFlags |= ImGuiConfigFlags_NoMouseCursorChange;
 captureBackdrop();
 ImGui_ImplOpenGL3_NewFrame();ImGui_ImplWin32_NewFrame();
 const float wheelX=(float)pendingWheelX.exchange(0)/WHEEL_DELTA,wheelY=(float)pendingWheelY.exchange(0)/WHEEL_DELTA;
 if(visible&&(wheelX!=0||wheelY!=0))ImGui::GetIO().AddMouseWheelEvent(wheelX,wheelY);
 ImGui::NewFrame();
 const auto state=SintetSocial::Snapshot();
 if(openLobbyOnStart&&state.localPort>0&&!state.room.empty()){
  openLobbyOnStart=false;
  section=1;
  if(!visible)openPanel();
 }
 const bool showPassive=!SintetWidgets::GhostMode();
 if(showPassive)drawWelcome();
 drawPanel(state);
 const bool networkHUD=!visible&&showPassive&&SintetWidgets::NetworkPinned()&&drawNetworkHUD(state);
 SintetWidgets::Draw(visible,interfaceScale,headingFont,state,gpuRenderer.c_str());
 if(visible||showPassive)drawInvitation(state,networkHUD);
 // ImGui has only recorded vertices so far. The back buffer still contains just Minecraft.
 SintetScreenshot::Capture(window);
 drawScreenshotNotice();
 ImGui::Render();
 ImGui_ImplOpenGL3_RenderDrawData(ImGui::GetDrawData());
}
BOOL WINAPI nativeSwapFrame(HDC dc) {
 {
  // Multiple windows can swap buffers at the same time. ImGui has one context.
  std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
  if(lock.owns_lock()&&!stopping){
   try{
    if(!currentGame){
     HMODULE glfw=GetModuleHandleW(L"glfw.dll");if(!glfw)glfw=GetModuleHandleW(L"glfw3.dll");
     if(glfw){currentGame=(CurrentGame)GetProcAddress(glfw,"glfwGetCurrentContext");nativeWindow=(WindowHandle)GetProcAddress(glfw,"glfwGetWin32Window");inputMode=(InputMode)GetProcAddress(glfw,"glfwSetInputMode");getMode=(GetMode)GetProcAddress(glfw,"glfwGetInputMode");}
    }
    renderFrame(currentGame?currentGame():nullptr);
   }catch(const std::exception&){
    if(visible)closePanel();
    stopping=true;
    fputs("Sintet Overlay: rendering disabled after an internal error\n",stderr);
   }
  }
 }
 return originalNativeSwap(dc);
}
DWORD WINAPI loadRenderer(void*){
 // Both LWJGL 2 (old releases) and GLFW/LWJGL 3 present through Windows SwapBuffers.
 HMODULE gdi=GetModuleHandleW(L"gdi32.dll");
 target=gdi?(void*)GetProcAddress(gdi,"SwapBuffers"):nullptr;
 if(target&&MH_Initialize()==MH_OK&&MH_CreateHook(target,(void*)nativeSwapFrame,(void**)&originalNativeSwap)==MH_OK&&MH_EnableHook(target)==MH_OK){fputs("Sintet Overlay: Windows OpenGL renderer connected\n",stderr);return 0;}
 fputs("Sintet Overlay: cannot connect Windows OpenGL renderer\n",stderr);return 1;
}
}
extern "C" void SintetRenderStart(){sessionStarted=GetTickCount64();SintetSocial::Start();SintetMedia::Start();SintetAudio::Start();SintetScreenshot::Start();stopping=false;loaderThread=CreateThread(nullptr,0,loadRenderer,nullptr,0,nullptr);}
extern "C" void SintetRenderStop(){
 stopping=true;
 SintetSocial::Stop();
 SintetMedia::Stop();
 SintetAudio::Stop();
 SintetScreenshot::Stop();
 if(loaderThread){WaitForSingleObject(loaderThread,1000);CloseHandle(loaderThread);loaderThread=nullptr;}
 if(target)MH_DisableHook(target);
 std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
 if(!lock.owns_lock())return;
 const WNDPROC previous=originalProc.load();
 if(window&&previous&&GetWindowLongPtrW(window,GWLP_WNDPROC)==(LONG_PTR)input)
  SetWindowLongPtrW(window,GWLP_WNDPROC,(LONG_PTR)previous);
}
