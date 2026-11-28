#include "language.h"
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <gl/GL.h>
#include <atomic>
#include <algorithm>
#include <array>
#include <cstdio>
#include <cmath>
#include <chrono>
#include <cfloat>
#include <cstring>
#include <cstdlib>
#include <cwchar>
#include <deque>
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
#include "overlay_style.h"
#include "acrylic.h"
#include "layout.h"
#include "brand.h"
#include "provider_mark.h"
#include "badge_marks.h"
#include "vendor/imgui/backends/imgui_impl_win32.h"
#include "vendor/imgui/backends/imgui_impl_opengl3.h"
#include "vendor/minhook/include/MinHook.h"
extern IMGUI_IMPL_API LRESULT ImGui_ImplWin32_WndProcHandler(HWND,UINT,WPARAM,LPARAM);
extern "C" unsigned int WINAPI SintetProfileSpectrum(unsigned int*,unsigned int);
namespace {
using SintetOverlayStyle::ControlFill;
struct GLFWwindow;
using Swap = void (*)(GLFWwindow*);
using WindowHandle = HWND (*)(GLFWwindow*);
using InputMode = void (*)(GLFWwindow*,int,int);
using GetMode = int (*)(GLFWwindow*,int);
using GlfwGetCursorPos = void (*)(GLFWwindow*,double*,double*);
using GlfwSetCursorPos = void (*)(GLFWwindow*,double,double);
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
GlfwGetCursorPos getCursorPos;
GlfwSetCursorPos setCursorPos;
void* target;
HANDLE loaderThread;
std::atomic<bool> stopping{false};
std::recursive_mutex renderMutex;
HWND window;
std::atomic<WNDPROC> originalProc{nullptr};
ImGuiContext* context;
HGLRC overlayGL;
float interfaceScale=1.0f;
constexpr float scrollInset=7.f;
float fontRasterScale=1.0f;
ImFont* titleFont;
ImFont* headingFont;
GLuint avatarTexture=0,buildIconTexture=0,brandTexture=0;
SintetAcrylic::Backdrop acrylic,mediaAcrylic,dockAcrylic,welcomeAcrylic;
SintetSocial::State previewSocial;
constexpr int previewWidth=704,previewHeight=396;
bool previewEmbedded=false;
struct PreviewEvent {int kind,x,y,value;};
std::mutex previewInputMutex;
std::deque<PreviewEvent> previewEvents;
std::mutex previewFrameMutex;
std::vector<unsigned char> previewPixels;
unsigned int previewFrameID=0;
std::atomic<bool> previewFrameAvailable{false};
std::shared_ptr<const SintetSocial::BadgeCatalog> badgeCatalog;
std::unordered_map<std::string,GLuint> badgeTextures;
std::string buildName,buildPreset,gameVersionLine,gpuRenderer;
std::string overlayHotkey="Shift + Tab";
int overlayHotkeyKey=VK_TAB,overlayHotkeyMods=MOD_SHIFT;
float copiedUntil=0;
void logo(ImDrawList*,ImVec2,float,ImU32);
void drawFittedText(ImDrawList*,ImFont*,float,ImVec2,float,ImU32,const std::string&);
GLFWwindow* game;
std::atomic<bool> visible{false};
bool previousToggle=false,previousEscape=false,previousCapture=false,usingGLFWCursor=false;
bool openLobbyOnStart=false;
std::atomic<bool> acceptWheel{false};
std::atomic<int> pendingWheelX{0},pendingWheelY{0};
std::atomic<ULONGLONG> mouseResumeAfter{0};
std::atomic<unsigned> gameMouseButtons{0},carriedMouseButtons{0};
ULONGLONG welcomeStarted=0;
ULONGLONG sessionStarted=0;
float panelAnimation=0.0f;
enum Section { Friends, Lobby, Settings, Music, Screenshots, Mixer, NoSection };
int oldCursor=0x34001,section=Friends;
void selectSection(int next);
std::string selectedFriend;
bool friendProfileOpen=false,confirmFriendRemoval=false;
float friendPageProgress=0;
std::wstring selectedScreenshot;
bool lobbyInvites=false;
bool previewPlaying=true;
float previewPosition=72.f;
bool showMiniPlayer=true;
constexpr int CursorMode=0x33001,CursorNormal=0x34001;
constexpr int CursorDisabled=0x34003;
double savedGameCursorX=0,savedGameCursorY=0;
bool savedGameCursorValid=false;

struct MouseButton {
 UINT down,doubleClick,up;
 int key;
 WORD mask,xButton;
};
constexpr MouseButton mouseButtons[]={
 {WM_LBUTTONDOWN,WM_LBUTTONDBLCLK,WM_LBUTTONUP,VK_LBUTTON,MK_LBUTTON,0},
 {WM_RBUTTONDOWN,WM_RBUTTONDBLCLK,WM_RBUTTONUP,VK_RBUTTON,MK_RBUTTON,0},
 {WM_MBUTTONDOWN,WM_MBUTTONDBLCLK,WM_MBUTTONUP,VK_MBUTTON,MK_MBUTTON,0},
 {WM_XBUTTONDOWN,WM_XBUTTONDBLCLK,WM_XBUTTONUP,VK_XBUTTON1,MK_XBUTTON1,XBUTTON1},
 {WM_XBUTTONDOWN,WM_XBUTTONDBLCLK,WM_XBUTTONUP,VK_XBUTTON2,MK_XBUTTON2,XBUTTON2}
};

unsigned physicalMouseButtons(){
 unsigned mask=0;
 for(const auto& button:mouseButtons)if(GetAsyncKeyState(button.key)&0x8000)mask|=button.mask;
 return mask;
}

void resetOverlayMouse(){
 if(!context||!window)return;
 ImGui::SetCurrentContext(context);
 auto& io=ImGui::GetIO();
 io.ClearEventsQueue();
 io.ClearInputMouse();
 io.AddMousePosEvent(-FLT_MAX,-FLT_MAX);
 for(const auto& button:mouseButtons)ImGui_ImplWin32_WndProcHandler(window,button.up,MAKEWPARAM(0,button.xButton),0);
}

void releaseGameMouse(){
 unsigned held=gameMouseButtons.exchange(0);
 const auto previous=originalProc.load();
 if(!held||!window||!previous)return;
 POINT pointer{};
 GetCursorPos(&pointer);ScreenToClient(window,&pointer);
 const WORD modifiers=((GetKeyState(VK_SHIFT)&0x8000)?MK_SHIFT:0)|((GetKeyState(VK_CONTROL)&0x8000)?MK_CONTROL:0);
 for(const auto& button:mouseButtons)if(held&button.mask){
  held&=~button.mask;
  CallWindowProcW(previous,window,button.up,MAKEWPARAM(held|modifiers,button.xButton),MAKELPARAM(pointer.x,pointer.y));
 }
}

void closePanel(){
 if(SintetOverlayStyle::preview){visible=false;return;}
 acceptWheel=false;pendingWheelX=0;pendingWheelY=0;
 carriedMouseButtons.fetch_or(physicalMouseButtons());
 resetOverlayMouse();
 // Discard queued motion while the game restores its cursor, but pass button releases.
 mouseResumeAfter=GetTickCount64()+80;
 visible=false;
 if(usingGLFWCursor){
  inputMode(game,CursorMode,oldCursor);
  // GLFW seeds the disabled cursor's virtual position from the visible OS
  // cursor when the mode changes. Restore the position Minecraft had before
  // opening the overlay, or the first mouse delta is measured from the UI
  // cursor's location and turns the camera by that offset.
  if(savedGameCursorValid&&oldCursor==CursorDisabled)setCursorPos(game,savedGameCursorX,savedGameCursorY);
  savedGameCursorValid=false;
 }
 else{
  ClipCursor(&savedClip);
  while(cursorAdjustments>0){ShowCursor(FALSE);--cursorAdjustments;}
  SetCursor(LoadCursorW(nullptr,IDC_ARROW));
 }
}
void openPanel(){
 pendingWheelX=0;pendingWheelY=0;acceptWheel=true;
 mouseResumeAfter=0;
 visible=true;
 carriedMouseButtons=physicalMouseButtons();
 releaseGameMouse();
 resetOverlayMouse();
 usingGLFWCursor=game&&getMode&&inputMode;
 if(usingGLFWCursor){
  oldCursor=getMode(game,CursorMode);
  savedGameCursorValid=oldCursor==CursorDisabled&&getCursorPos&&setCursorPos;
  if(savedGameCursorValid)getCursorPos(game,&savedGameCursorX,&savedGameCursorY);
  inputMode(game,CursorMode,CursorNormal);
 }
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
 const bool mouseMessage=message==WM_INPUT||(message>=WM_MOUSEFIRST&&message<=WM_MOUSELAST);
 unsigned buttonMask=0;
 bool released=false;
 if(mouseMessage)for(const auto& button:mouseButtons){
  if(message!=button.down&&message!=button.doubleClick&&message!=button.up)continue;
  if(button.xButton&&HIWORD(w)!=button.xButton)continue;
  buttonMask=button.mask;
  released=message==button.up;
  break;
 }
 if(buttonMask&&(carriedMouseButtons.load()&buttonMask)){
  if(released)carriedMouseButtons.fetch_and(~buttonMask);
  return 0;
 }
 if(message==WM_KILLFOCUS){gameMouseButtons=0;carriedMouseButtons=0;}
 if(acceptWheel&&(message==WM_MOUSEWHEEL||message==WM_MOUSEHWHEEL)){
  const int delta=static_cast<short>(HIWORD(w));
  if(message==WM_MOUSEWHEEL)pendingWheelY.fetch_add(delta);else pendingWheelX.fetch_sub(delta);
  return 0;
 }
 // Never let UI movement reach Minecraft, even when the render thread briefly
 // owns the shared mutex and ImGui cannot consume this individual event.
 if(visible&&mouseMessage){
  std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
  if(lock.owns_lock()&&!stopping&&context){
   ImGui::SetCurrentContext(context);
   ImGui_ImplWin32_WndProcHandler(hwnd,message,w,l);
  }
  return 0;
 }
 ULONGLONG resumeAt=mouseResumeAfter.load();
 if(resumeAt&&buttonMask&&!released&&static_cast<LONG>(GetMessageTime()-static_cast<DWORD>(resumeAt-80))<0)return 0;
 if(resumeAt&&(message==WM_MOUSEMOVE||message==WM_INPUT)){
  if(GetTickCount64()<resumeAt)return 0;
  if(!mouseResumeAfter.compare_exchange_strong(resumeAt,0)&&GetTickCount64()<resumeAt)return 0;
 }
 std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
 if(lock.owns_lock()&&!stopping&&context&&visible){
  ImGui::SetCurrentContext(context);
  ImGui_ImplWin32_WndProcHandler(hwnd,message,w,l);
  if((message>=WM_MOUSEFIRST&&message<=WM_MOUSELAST)||message==WM_INPUT||message==WM_CHAR||message==WM_KEYDOWN||message==WM_SYSKEYDOWN)return 0;
 }
 const WNDPROC previous=originalProc.load();
 if(buttonMask){
  if(released)gameMouseButtons.fetch_and(~buttonMask);
  else gameMouseButtons.fetch_or(buttonMask);
 }
 return previous?CallWindowProcW(previous,hwnd,message,w,l):DefWindowProcW(hwnd,message,w,l);
}
float easeOut(float value) {
 value=std::clamp(value,0.0f,1.0f);
 return 1.0f-std::pow(1.0f-value,3.0f);
}

void drawWelcome() {
 if(!welcomeStarted||visible)return;
 const float elapsed=(float)(GetTickCount64()-welcomeStarted)/1000.0f;
 if(elapsed>6.0f)return;
 const float scale=interfaceScale;
 const float slide=elapsed>5.4f?1.0f-easeOut((elapsed-5.4f)/.6f):easeOut(elapsed/.55f);
 const std::string title=OverlayText("Игра запущена через Sintet");
 const std::string shortcut=OverlayText("Открыть оверлей");
 auto* bodyFont=ImGui::GetFont();
 const float titleSize=20*scale,shortcutSize=17*scale,keySize=15*scale;
 const float left=76*scale,right=17*scale,height=76*scale,margin=23*scale;
 const float shortcutWidth=bodyFont->CalcTextSizeA(shortcutSize,FLT_MAX,0,shortcut.c_str()).x;
 const float keyWidth=bodyFont->CalcTextSizeA(keySize,FLT_MAX,0,overlayHotkey.c_str()).x+14*scale;
 const float textWidth=std::max(headingFont->CalcTextSizeA(titleSize,FLT_MAX,0,title.c_str()).x,
                                shortcutWidth+keyWidth+9*scale);
 const float width=std::min(ImGui::GetIO().DisplaySize.x-2*margin,std::max(320*scale,left+textWidth+right));
 if(width<=left+right+keyWidth)return;
 const ImVec2 top(margin-(1-slide)*(width+margin+8*scale),margin);
 const auto at=[&](float x,float y){return ImVec2(top.x+x*scale,top.y+y*scale);};
 auto* draw=ImGui::GetForegroundDrawList();
 auto appearance=SintetOverlayStyle::style;
 if(appearance.acrylic&&appearance.opacity<1.f&&appearance.blur>0.f){
  appearance.opacity*=.90f;
  appearance.blur*=1.35f;
 }
 const float rounding=std::clamp(appearance.rounding+7.f,22.f,34.f)*scale;
 draw->AddRectFilled(ImVec2(top.x,top.y+4*scale),ImVec2(top.x+width,top.y+height+4*scale),IM_COL32(0,0,0,48),rounding);
 SintetAcrylic::DrawSurface(welcomeAcrylic,draw,top,ImVec2(width,height),scale,1.f,rounding,0.f,appearance);
 const auto ink=ImGui::GetColorU32(ImGuiCol_Text),muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
 draw->AddRectFilled(at(13,13),at(63,63),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.12f),.64f)),14*scale);
 logo(draw,at(23,24),.86f*scale,ink);
 drawFittedText(draw,headingFont,titleSize,at(76,13),width-left-right,ink,title);
 const float keyX=std::min(left+shortcutWidth+9*scale,width-right-keyWidth);
 drawFittedText(draw,bodyFont,shortcutSize,at(76,45),keyX-left-7*scale,muted,shortcut);
 const ImVec2 key(top.x+keyX,top.y+40*scale);
 draw->AddRectFilled(key,ImVec2(key.x+keyWidth,key.y+24*scale),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.16f),.72f)),7*scale);
 draw->AddText(bodyFont,keySize,ImVec2(key.x+7*scale,key.y+4*scale),ink,overlayHotkey.c_str());
}

float overlaySceneDim(){return SintetOverlayStyle::style.dim*easeOut(panelAnimation);}
template<int size> GLuint uploadIcon(const std::string& hex) {
 if(hex.size()!=size*size*8)return 0;
 unsigned char pixels[size*size*4];
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
 glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,size,size,0,GL_RGBA,GL_UNSIGNED_BYTE,pixels);
 glBindTexture(GL_TEXTURE_2D,0);
 return texture;
}

GLuint loadIcon(const char* variable) {
 char hex[36*36*8+1]={0};
 return GetEnvironmentVariableA(variable,hex,sizeof(hex))==36*36*8?uploadIcon<36>(hex):0;
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

void drawSocialName(ImFont* font,float fontSize,float available,const std::string& name,
                    SintetSocial::AccountProvider provider,const std::vector<std::string>& badges){
 const float width=SintetBadgeMark::DrawName(ImGui::GetWindowDrawList(),font,fontSize,ImGui::GetCursorScreenPos(),
    available,ImGui::GetColorU32(ImGuiCol_Text),name,14*interfaceScale,provider,badges);
 ImGui::Dummy(ImVec2(width,fontSize));
}

void drawMixedProfileWarning(float scale){
 const ImVec2 top=ImGui::GetCursorScreenPos();
 const float center=top.y+ImGui::GetTextLineHeight()*.5f;
 const ImVec4 amber(.89f,.76f,.49f,1);
 const ImU32 color=ImGui::GetColorU32(amber);
 auto* draw=ImGui::GetWindowDrawList();
 draw->AddTriangle(ImVec2(top.x+7*scale,center-6*scale),ImVec2(top.x+scale,center+5*scale),
                   ImVec2(top.x+13*scale,center+5*scale),color,1.2f*scale);
 draw->AddLine(ImVec2(top.x+7*scale,center-2.6f*scale),ImVec2(top.x+7*scale,center+scale),color,1.15f*scale);
 draw->AddCircleFilled(ImVec2(top.x+7*scale,center+3*scale),.65f*scale,color);
 ImGui::Dummy(ImVec2(14*scale,ImGui::GetTextLineHeight()));
 ImGui::SameLine();
 ImGui::PushStyleColor(ImGuiCol_Text,amber);
 ImGui::TextWrapped("%s",OverlayText("Разные типы профилей. Скины и плащи друг друга не видны."));
 ImGui::PopStyleColor();
}

void drawGameProfile(float scale) {
 if(gameVersionLine.empty()&&buildName.empty())return;
 const ImVec2 top=ImGui::GetCursorScreenPos();
 const float width=ImGui::GetContentRegionAvail().x;
 if(width<=0)return;
 auto p=[&](float x,float y){return ImVec2(top.x+x*scale,top.y+y*scale);};
 auto* draw=ImGui::GetWindowDrawList();
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+54*scale),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.05f))),10*scale);
 const ImVec2 tile=p(9,9),tileEnd=p(45,45);
 draw->AddRectFilled(tile,tileEnd,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.12f))),7*scale);
 if(buildIconTexture){
  draw->AddImageRounded((ImTextureID)(intptr_t)buildIconTexture,tile,tileEnd,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(IM_COL32_WHITE),7*scale);
 }else{
  drawBuildPreset(draw,tile,scale);
 }
 const float available=width-66*scale;
 drawFittedText(draw,headingFont,17*scale,p(55,7),available,ImGui::GetColorU32(ImGuiCol_Text),buildName.empty()?"Minecraft":buildName);
 drawFittedText(draw,ImGui::GetFont(),SintetOverlayStyle::secondaryTextSize*scale,p(55,29),available,ImGui::GetColorU32(ImGuiCol_TextDisabled),gameVersionLine);
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

void socialAvatar(const SintetSocial::Person& person,float scale,bool colorful=false) {
 static std::unordered_map<std::string,GLuint> textures;
 GLuint texture=0;
 auto existing=textures.find(person.avatar);
 if(existing!=textures.end()){
  texture=existing->second;
 }else if(!person.avatar.empty()){
  auto pixels=SintetSocial::AvatarPixels(person);
  if(!pixels.empty()&&textures.size()<256){
   texture=uploadIcon<36>(pixels);
   if(texture)textures.emplace(person.avatar,texture);
  }
 }
 const ImVec2 start=ImGui::GetCursorScreenPos();
 const ImVec2 end(start.x+36*scale,start.y+36*scale);
 auto* draw=ImGui::GetWindowDrawList();
 if(texture){
  draw->AddImageRounded((ImTextureID)(intptr_t)texture,start,end,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(ImGuiCol_Text),7*scale);
 }else{
  constexpr ImVec4 accents[]={{.75f,.82f,.87f,1},{.81f,.77f,.91f,1},{.88f,.83f,.74f,1}};
  unsigned int hue=0;
  for(const unsigned char character:person.id)hue=hue*31+character;
  const ImVec4 fill=colorful?accents[hue%3]:SintetOverlayStyle::Surface(.14f);
  draw->AddRectFilled(start,end,ImGui::GetColorU32(fill),9*scale);
  const size_t first=person.name.find_first_not_of(" ");
  std::string initial="?";
  if(first!=std::string::npos){
   size_t last=first+1;
   while(last<person.name.size()&&(static_cast<unsigned char>(person.name[last])&0xc0)==0x80)++last;
   initial=person.name.substr(first,last-first);
  }
  const float fontSize=16*scale;
  auto* font=ImGui::GetFont();
  const ImVec2 size=font->CalcTextSizeA(fontSize,FLT_MAX,0,initial.c_str());
  const auto ink=colorful?ImGui::GetColorU32(ImVec4(.15f,.16f,.18f,1)):ImGui::GetColorU32(ImGuiCol_Text);
  draw->AddText(font,fontSize,ImVec2(start.x+(36*scale-size.x)*.5f,start.y+(36*scale-size.y)*.5f),ink,initial.c_str());
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
 }else if(kind==2){
  d->AddLine(v(2,5),v(22,5),color,line);d->AddCircleFilled(v(9,5),2.8f*s,color);
  d->AddLine(v(2,12),v(22,12),color,line);d->AddCircleFilled(v(16,12),2.8f*s,color);
  d->AddLine(v(2,19),v(22,19),color,line);d->AddCircleFilled(v(7,19),2.8f*s,color);
 }else if(kind==3){
  d->AddCircle(v(12,12),3*s,color,24,line);
  d->AddBezierCubic(v(6,5),v(0,8),v(0,16),v(6,19),color,line);
  d->AddBezierCubic(v(18,5),v(24,8),v(24,16),v(18,19),color,line);
 }else if(kind==4){
  d->AddLine(v(9,18),v(9,5),color,line);d->AddLine(v(9,5),v(20,3),color,line);
  d->AddLine(v(20,3),v(20,16),color,line);
  d->AddCircleFilled(v(6,18),3*s,color);d->AddCircleFilled(v(17,16),3*s,color);
 }else if(kind==5){
  d->AddRect(v(2,3),v(22,21),color,3*s,0,line);
  d->AddCircle(v(8,8),2*s,color,12,line);
  const ImVec2 mountain[]={v(3,19),v(15,9),v(22,16)};
  d->AddPolyline(mountain,3,color,0,line);
 }else{
  const ImVec2 speaker[]={v(2,9),v(7,9),v(12,5),v(12,19),v(7,15),v(2,15)};
  d->AddPolyline(speaker,6,color,ImDrawFlags_Closed,line);
  d->AddBezierCubic(v(16,7),v(21,9),v(21,15),v(16,17),color,line);
 }
}
GLuint uploadBrandMark() {
 constexpr int samples=4;
 constexpr int rasterWidth=SintetMarkWidth*samples, rasterHeight=SintetMarkHeight*samples;
 BITMAPINFO bitmapInfo{};
 bitmapInfo.bmiHeader.biSize=sizeof(BITMAPINFOHEADER);
 bitmapInfo.bmiHeader.biWidth=rasterWidth;
 bitmapInfo.bmiHeader.biHeight=-rasterHeight;
 bitmapInfo.bmiHeader.biPlanes=1;
 bitmapInfo.bmiHeader.biBitCount=32;
 bitmapInfo.bmiHeader.biCompression=BI_RGB;
 HDC rasterDC=CreateCompatibleDC(nullptr);
 if(!rasterDC)return 0;
 void* bitmapBits=nullptr;
 HBITMAP bitmap=CreateDIBSection(rasterDC,&bitmapInfo,DIB_RGB_COLORS,&bitmapBits,nullptr,0);
 if(!bitmap){
  DeleteDC(rasterDC);
  return 0;
 }
 const HGDIOBJ oldBitmap=SelectObject(rasterDC,bitmap);
 SelectObject(rasterDC,GetStockObject(WHITE_BRUSH));
 std::memset(bitmapBits,0,rasterWidth*rasterHeight*4);
 const BOOL drawn=draw_sintet_mark(rasterDC,0,0,rasterWidth);
 GdiFlush();
 unsigned char pixels[SintetMarkWidth*SintetMarkHeight*4];
 const auto* raster=static_cast<const unsigned char*>(bitmapBits);
 for(int y=0;y<SintetMarkHeight;y++){
  for(int x=0;x<SintetMarkWidth;x++){
   unsigned coverage=0;
   for(int row=0;row<samples;row++){
    for(int column=0;column<samples;column++){
     coverage+=raster[((y*samples+row)*rasterWidth+x*samples+column)*4];
    }
   }
   unsigned char* pixel=pixels+(y*SintetMarkWidth+x)*4;
   pixel[0]=pixel[1]=pixel[2]=255;
   pixel[3]=static_cast<unsigned char>(coverage/(samples*samples));
  }
 }
 SelectObject(rasterDC,oldBitmap);
 DeleteObject(bitmap);
 DeleteDC(rasterDC);
 if(!drawn)return 0;
 GLuint texture=0;
 glGenTextures(1,&texture);
 glBindTexture(GL_TEXTURE_2D,texture);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_S,GL_CLAMP);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_T,GL_CLAMP);
 glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,SintetMarkWidth,SintetMarkHeight,0,GL_RGBA,GL_UNSIGNED_BYTE,pixels);
 glBindTexture(GL_TEXTURE_2D,0);
 return texture;
}
void logo(ImDrawList* d, ImVec2 p, float s, ImU32 color) {
 if(!brandTexture)brandTexture=uploadBrandMark();
 if(!brandTexture)return;
 const float unit=35*s/SintetMarkWidth;
 d->AddImage((ImTextureID)(intptr_t)brandTexture,p,
  ImVec2(p.x+SintetMarkWidth*unit,p.y+SintetMarkHeight*unit),ImVec2(0,0),ImVec2(1,1),color);
}
void socialTitle(const char* title){ImGui::PushFont(headingFont);ImGui::TextUnformatted(title);ImGui::PopFont();}
void dispatchSocial(const std::string& action,const std::string& target=""){
 if(!SintetOverlayStyle::preview){SintetSocial::Action(action,target);return;}
 auto& state=previewSocial;
 if(action=="host"&&state.hostNeedsPack){state.hostNeedsPack=false;state.connection.clear();return;}
 if(action=="host"||(action=="invite"&&state.room.empty())||(action=="join"&&target.size()==6)){
  state.room=action!="join"?"241608":target;
  state.lobbyProvider="sintet";
  state.leader=action!="join";
  state.host=state.leader?state.self.name:"Alex";
  state.members={{state.self,state.leader,"",0,"preview"},{SintetSocial::Person{"alex","Alex"},!state.leader,"",0,"alex"}};
  state.packName=OverlayText("Моё выживание");
  state.packGame="1.21.4";
  state.worldReady=state.friendsOpen=state.friendLobbiesAvailable=state.voiceEnabled=true;
  state.members[0].voiceMic=state.members[1].voiceMic=true;
  state.members[1].voiceSpeaking=true;
 }else if(action=="close"){
  state.room.clear();state.members.clear();state.leader=false;
 }else if(action=="kick"&&state.leader){
  state.members.erase(std::remove_if(state.members.begin(),state.members.end(),
      [&](const SintetSocial::Member& member){return !member.leader&&member.person.id==target;}),state.members.end());
 }else if(action=="voice-enable"){
  state.voiceEnabled=target=="true";
 }else if(action=="voice-spatial"){
  state.voiceSpatial=target=="true";
 }else if(action=="voice-range"){
  state.voiceRange=std::atoi(target.c_str());
 }else if(action=="voice-mic"||action=="voice-mute"){
  for(auto& member:state.members){
   if(action=="voice-mic"&&member.person.id==state.self.id)member.voiceMic=target=="true";
   if(action=="voice-mute"&&member.person.id==target)member.voiceMuted=!member.voiceMuted;
  }
 }else if(action=="lobby-friends-open"){
  state.friendsOpen=target=="true";
 }else if(action=="friend-remove"){
  state.friends.erase(std::remove_if(state.friends.begin(),state.friends.end(),
      [&](const SintetSocial::Friend& entry){return entry.person.id==target;}),state.friends.end());
 }else if(action=="lobby-capacity"||action=="lobby-capacity-confirm"){
  state.roomCapacity=std::atoi(target.c_str());
 }
}
void socialButton(const char* label,const char* action,const std::string& target=""){
 if(ImGui::Button(label))dispatchSocial(action,target);
}
std::string invitationDetails(const SintetSocial::Invite& invite){
 std::string text=invite.game.empty()?"":"Minecraft "+invite.game;
 if(invite.game=="sintet:manual")return OverlayText("Ничего не ставить");
 if(!invite.build.empty()&&invite.build!=text){if(!text.empty())text+=" · ";text+=invite.build;}
 return text;
}
void voiceIcon(bool microphone,bool active,bool speaking,const char* action,const std::string& target,float scale){
 const auto label=microphone?"##mic":"##speaker";
 const ImVec2 size(28*scale,28*scale);
 ImVec2 top=ImGui::GetCursorScreenPos();
 if(action){
  if(ImGui::InvisibleButton(label,size))dispatchSocial(action,target);
  if(ImGui::IsItemHovered())ImGui::GetWindowDrawList()->AddRectFilled(top,ImGui::GetItemRectMax(),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.16f),.82f)),5*scale);
 }else ImGui::Dummy(size);
 auto v=[&](float x,float y){return ImVec2(top.x+(x+2)*scale,top.y+(y+1.5f)*scale);};
 auto draw=ImGui::GetWindowDrawList();
 const auto color=ImGui::GetColorU32(speaking?SintetOverlayStyle::Accent(1):ImGui::GetStyleColorVec4(active?ImGuiCol_Text:ImGuiCol_TextDisabled));
 const float stroke=1.4f*scale;
 if(microphone){
  draw->AddRect(v(9,4),v(15,14),color,3*scale,0,stroke);
  draw->AddBezierCubic(v(6,11),v(6,21),v(18,21),v(18,11),color,stroke);
  draw->AddLine(v(12,18),v(12,22),color,stroke);
 }else{
  const ImVec2 speaker[]={v(4,10),v(8,10),v(13,6),v(13,19),v(8,15),v(4,15)};
  draw->AddPolyline(speaker,6,color,ImDrawFlags_Closed,stroke);
  if(active)draw->AddBezierCubic(v(17,8),v(22,10),v(22,15),v(17,17),color,stroke);
 }
 if(!active)draw->AddLine(v(5,5),v(19,20),color,stroke);
}

void drawLobbyMember(const SintetSocial::State& state,const SintetSocial::Member& member,float scale){
 auto px=[scale](float v){return v*scale;};
 const ImVec2 top=ImGui::GetCursorScreenPos();
 const float width=ImGui::GetContentRegionAvail().x;
 const bool own=member.person.id==state.self.id;
 const bool kick=state.leader&&!member.leader;
 const int voiceCount=state.voiceEnabled?(own?1:2):0;
 const int controls=voiceCount+(kick?1:0);
 const float actionsWidth=px(28*controls+4*std::max(0,controls-1));
 const bool stacked=controls&&width-actionsWidth<px(188);
 const float height=px(stacked?85:54);
 const float textWidth=width-px(64)-(controls&&!stacked?actionsWidth+px(6):0);
 auto* draw=ImGui::GetWindowDrawList();
 const auto surface=member.leader?SintetOverlayStyle::Accent(.105f):SintetOverlayStyle::Surface(.065f);
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+height),ImGui::GetColorU32(ControlFill(surface,.64f)),px(11));
 ImGui::SetCursorScreenPos(ImVec2(top.x+px(9),top.y+px(9)));
 socialAvatar(member.person,scale,true);
 ImGui::SetCursorScreenPos(ImVec2(top.x+px(55),top.y+px(7)));
 drawSocialName(headingFont,px(19),std::max(0.f,textWidth-(member.leader?px(25):0)),member.person.name,member.person.provider,member.person.badges);
 if(member.leader){
  ImGui::SameLine();
  const auto crown=ImGui::GetCursorScreenPos();
  const ImVec2 points[]={{crown.x+px(1),crown.y+px(7)},{crown.x+px(5),crown.y+px(10)},
   {crown.x+px(9),crown.y+px(4)},{crown.x+px(13),crown.y+px(10)},{crown.x+px(17),crown.y+px(7)},
   {crown.x+px(15),crown.y+px(15)},{crown.x+px(3),crown.y+px(15)}};
  draw->AddConvexPolyFilled(points,7,ImGui::GetColorU32(ImVec4(.91f,.69f,.29f,1)));
  ImGui::Dummy(ImVec2(px(18),px(18)));
 }
 const char* preparation=member.preparation=="ready"?OverlayText("Готов"):member.preparation=="world"?OverlayText("В мире"):member.preparation=="downloading"?OverlayText("Скачивает сборку"):member.preparation=="installing"?OverlayText("Устанавливает Minecraft"):member.preparation=="required"?OverlayText("Нужна сборка"):member.preparation=="error"?OverlayText("Нужна повторная попытка"):member.preparation=="launching"?OverlayText("Запускает игру"):OverlayText("В лобби");
 std::string status=member.leader?(state.worldReady?OverlayText("Мир открыт"):OverlayText("Лидер лобби")):preparation;
 if(!member.leader&&(member.preparation=="downloading"||member.preparation=="installing"))status+=" · "+std::to_string(member.percent)+"%";
 drawFittedText(draw,ImGui::GetFont(),px(18),ImVec2(top.x+px(55),top.y+px(28)),textWidth,ImGui::GetColorU32(ImGuiCol_TextDisabled),status);
 float right=std::max(top.x,top.x+width-px(8)-actionsWidth);
 const float y=top.y+px(stacked?49:13);
 const auto placeAction=[&]{ImGui::SetCursorScreenPos(ImVec2(right,y));right+=px(32);};
 if(voiceCount){
  placeAction();
  voiceIcon(true,member.voiceMic,member.voiceSpeaking,own?"voice-mic":nullptr,member.voiceMic?"false":"true",scale);
  if(!own){placeAction();voiceIcon(false,!member.voiceMuted,false,"voice-mute",member.person.id,scale);}
 }
 if(kick){
  placeAction();
  const auto button=ImGui::GetCursorScreenPos();
  if(ImGui::InvisibleButton("##kick",ImVec2(px(28),px(28))))dispatchSocial("kick",member.person.id);
  const bool hovered=ImGui::IsItemHovered();
  const auto fill=hovered?ImVec4(.46f,.20f,.22f,1):SintetOverlayStyle::Surface(.11f);
  draw->AddRectFilled(button,ImGui::GetItemRectMax(),ImGui::GetColorU32(ControlFill(fill,.76f)),px(8));
  const auto ink=ImGui::GetColorU32(hovered?ImGuiCol_Text:ImGuiCol_TextDisabled);
  draw->AddLine(ImVec2(button.x+px(10),button.y+px(10)),ImVec2(button.x+px(18),button.y+px(18)),ink,px(1.5f));
  draw->AddLine(ImVec2(button.x+px(10),button.y+px(18)),ImVec2(button.x+px(18),button.y+px(10)),ink,px(1.5f));
  if(hovered)ImGui::SetTooltip("%s",OverlayText("Исключить из лобби"));
 }
 ImGui::SetCursorScreenPos(ImVec2(top.x,top.y+height));
 ImGui::Dummy(ImVec2(0,0));
}

std::string friendPresence(const SintetSocial::Friend& friendRow){
 if(friendRow.status=="pending")return OverlayText(friendRow.incoming?"Заявка в друзья":"Заявка отправлена");
 if(friendRow.presence=="lobby")return OverlayText("В лобби");
 if(friendRow.presence=="singleplayer")return OverlayText("Одиночный мир");
 if(friendRow.presence=="world")return OverlayText("В мире");
 if(friendRow.presence=="menu")return OverlayText("В главном меню");
 if(friendRow.presence=="launcher")return OverlayText("В лаунчере");
 if(friendRow.presence=="multiplayer"){
  std::string label=OverlayText("Мультиплеер");
  return friendRow.server.empty()?label:label+" · "+friendRow.server;
 }
 return OverlayText("Не в сети");
}

void drawFriendPresence(const SintetSocial::Friend& entry,ImVec2 position,float width,float scale){
 const auto color=entry.status=="pending"?ImVec4(.88f,.72f,.45f,1):
  entry.presence.empty()?ImGui::GetStyleColorVec4(ImGuiCol_TextDisabled):
  entry.presence=="launcher"?ImVec4(.57f,.73f,.96f,1):ImVec4(.50f,.84f,.67f,1);
 const float size=SintetOverlayStyle::secondaryTextSize*scale;
 auto* draw=ImGui::GetWindowDrawList();
 draw->AddCircleFilled(ImVec2(position.x+3*scale,position.y+size*.5f+1*scale),3*scale,ImGui::GetColorU32(color));
 position.x+=13*scale;
 drawFittedText(draw,ImGui::GetFont(),size,position,width-13*scale,ImGui::GetColorU32(ImGuiCol_TextDisabled),friendPresence(entry));
}

float friendProfileExtra(const SintetSocial::Friend& entry){
 return (!entry.person.handle.empty()||!entry.person.publicID.empty()?34.f:0.f)+
  (!entry.version.empty()||!entry.build.empty()?30.f:0.f)+(entry.hasTotals?59.f:0.f)+(!entry.musicTitle.empty()?65.f:0.f);
}

std::string friendTime(long long milliseconds){
 const auto minutes=std::max(0LL,milliseconds)/60000;
 char text[48];
 if(minutes>=60)std::snprintf(text,sizeof(text),OverlayText("%lld ч %lld мин"),minutes/60,minutes%60);
 else std::snprintf(text,sizeof(text),OverlayText("%lld мин"),minutes);
 return text;
}

void drawFriendExtras(const SintetSocial::Friend& entry,float scale){
 auto px=[scale](float value){return value*scale;};
 auto* draw=ImGui::GetWindowDrawList();
 auto* font=ImGui::GetFont();
 const auto text=ImGui::GetColorU32(ImGuiCol_Text),muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
 const auto fill=ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.08f),.70f));
 if(!entry.person.publicID.empty()||!entry.person.handle.empty()){
  const auto start=ImGui::GetCursorScreenPos();
  const float width=ImGui::GetContentRegionAvail().x;
  float idWidth=0;
  if(!entry.person.publicID.empty()){
   const auto label="ID · "+entry.person.publicID;
   idWidth=std::min(width,font->CalcTextSizeA(px(17),FLT_MAX,0,label.c_str()).x+px(26));
   if(ImGui::InvisibleButton("##friend-public-id",ImVec2(idWidth,px(28)))){
    ImGui::SetClipboardText(entry.person.publicID.c_str());copiedUntil=static_cast<float>(ImGui::GetTime())+2.f;
   }
   const bool hovered=ImGui::IsItemHovered();
   draw->AddRectFilled(start,ImGui::GetItemRectMax(),ImGui::GetColorU32(hovered?ImGuiCol_ButtonHovered:ImGuiCol_Button),px(8));
   drawFittedText(draw,font,px(17),ImVec2(start.x+px(13),start.y+px(5)),idWidth-px(25),text,label);
   if(hovered)ImGui::SetTooltip("%s",OverlayText(copiedUntil>ImGui::GetTime()?"ID скопирован":"Скопировать ID"));
  }else ImGui::Dummy(ImVec2(0,px(28)));
  const float offset=idWidth>0?idWidth+px(10):0;
  if(!entry.person.handle.empty())drawFittedText(draw,font,px(17),ImVec2(start.x+offset,start.y+px(5)),width-offset,muted,"@"+entry.person.handle);
 }
 if(!entry.build.empty()||!entry.version.empty()){
  const auto start=ImGui::GetCursorScreenPos();
  const float width=ImGui::GetContentRegionAvail().x;
  const auto label=entry.build.empty()?"Minecraft "+entry.version:entry.version.empty()?entry.build:entry.build+" · "+entry.version;
  drawFittedText(draw,font,px(18),start,width,muted,label);
  ImGui::Dummy(ImVec2(0,px(24)));
 }
 if(entry.hasTotals){
  const auto start=ImGui::GetCursorScreenPos();
  const float width=ImGui::GetContentRegionAvail().x;
  const float half=(width-px(6))*.5f;
  for(int i=0;i<2;++i){
   const ImVec2 from(start.x+i*(half+px(6)),start.y);
   draw->AddRectFilled(from,ImVec2(from.x+half,from.y+px(53)),fill,px(9));
   drawFittedText(draw,font,px(16),ImVec2(from.x+px(10),from.y+px(6)),half-px(20),muted,OverlayText(i?"В лаунчере":"В Minecraft"));
   drawFittedText(draw,headingFont,px(19),ImVec2(from.x+px(10),from.y+px(26)),half-px(20),text,friendTime(i?entry.launcherMs:entry.minecraftMs));
  }
  ImGui::Dummy(ImVec2(width,px(53)));
 }
 if(!entry.musicTitle.empty()){
  const auto from=ImGui::GetCursorScreenPos();
  const float width=ImGui::GetContentRegionAvail().x;
  draw->AddRectFilled(from,ImVec2(from.x+width,from.y+px(59)),fill,px(10));
  const ImVec2 record(from.x+px(37),from.y+px(29));
  draw->AddCircleFilled(record,px(22),ImGui::GetColorU32(SintetOverlayStyle::Surface(.02f)),36);
  draw->AddCircle(record,px(19),ImGui::GetColorU32(ImVec4(1,1,1,.14f)),36);
  draw->AddCircle(record,px(16),ImGui::GetColorU32(ImVec4(1,1,1,.10f)),36);
  static std::string trackKey;
  static GLuint image=0;
  const auto key=entry.person.id+entry.musicDigest;
  if(key!=trackKey){if(image)glDeleteTextures(1,&image);image=0;trackKey=key;}
  if(!image&&!entry.musicDigest.empty())image=uploadIcon<48>(SintetSocial::MusicCoverPixels(entry));
  if(image){
   const auto radius=px(13);
   draw->AddImageRounded((ImTextureID)(intptr_t)image,ImVec2(record.x-radius,record.y-radius),ImVec2(record.x+radius,record.y+radius),ImVec2(0,0),ImVec2(1,1),IM_COL32_WHITE,radius);
  }else icon(draw,ImVec2(record.x-px(8),record.y-px(8)),4,.67f*scale,muted);
  drawFittedText(draw,headingFont,px(18),ImVec2(from.x+px(69),from.y+px(entry.musicArtist.empty()?20:9)),width-px(81),text,entry.musicTitle);
  if(!entry.musicArtist.empty())drawFittedText(draw,font,px(17),ImVec2(from.x+px(69),from.y+px(30)),width-px(81),muted,entry.musicArtist);
  ImGui::Dummy(ImVec2(width,px(59)));
 }
}

void drawFriendProfile(const SintetSocial::State& state,const SintetSocial::Friend& entry,float scale){
 auto px=[scale](float value){return value*scale;};
 const auto base=ImGui::GetCursorPos();
 const auto top=ImGui::GetCursorScreenPos();
 const float width=ImGui::GetContentRegionAvail().x;
 auto* draw=ImGui::GetWindowDrawList();
 auto surface=SintetOverlayStyle::Accent(.12f);
 surface.w=.70f;
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+px(88)),ImGui::GetColorU32(surface),px(13));
 ImGui::SetCursorPos(ImVec2(base.x+px(14),base.y+px(15)));
 socialAvatar(entry.person,1.6f*scale,true);
 ImGui::SetCursorPos(ImVec2(base.x+px(86),base.y+px(19)));
 drawSocialName(headingFont,px(23),width-px(98),entry.person.name,entry.person.provider,entry.person.badges);
 drawFriendPresence(entry,ImVec2(top.x+px(86),top.y+px(49)),width-px(99),scale);
 ImGui::SetCursorPos(ImVec2(base.x,base.y+px(100)));
 drawFriendExtras(entry,scale);

 const bool pending=entry.status=="pending";
 const char* action=nullptr;
 const char* label=nullptr;
 if(pending&&entry.incoming){action="friend-accept";label=OverlayText("Принять");}
 else if(!pending){
  const bool inLobby=std::any_of(state.members.begin(),state.members.end(),[&](const auto& member){return member.person.id==entry.person.id;});
  if(entry.joinableLobby&&state.room.empty()){action="join-friend";label=OverlayText("Присоединиться");}
  else if(!entry.presence.empty()&&!inLobby&&(state.room.empty()||state.leader)){action="invite";label=OverlayText("Пригласить");}
 }
 if(!confirmFriendRemoval&&action){
  ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(SintetOverlayStyle::Accent(.22f)));
  ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(SintetOverlayStyle::Accent(.31f),.82f));
  if(ImGui::Button(label,ImVec2(-1,px(38))))dispatchSocial(action,entry.person.id);
  ImGui::PopStyleColor(2);
 }
 if(pending){
  const char* request=OverlayText(entry.incoming?"Отклонить":"Отменить");
  if(ImGui::Button(request,ImVec2(-1,px(36))))dispatchSocial("friend-remove",entry.person.id);
  return;
 }
 if(confirmFriendRemoval){
  ImGui::TextWrapped("%s",OverlayText("Удалить из друзей?"));
  const float half=(width-ImGui::GetStyle().ItemSpacing.x)*.5f;
  if(ImGui::Button(OverlayText("Отменить"),ImVec2(half,px(36))))confirmFriendRemoval=false;
  ImGui::SameLine();
  ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(ImVec4(.39f,.19f,.19f,1)));
  ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(ImVec4(.49f,.23f,.23f,1),.82f));
  if(ImGui::Button(OverlayText("Удалить"),ImVec2(half,px(36)))){
   dispatchSocial("friend-remove",entry.person.id);
   friendProfileOpen=false;
   confirmFriendRemoval=false;
  }
  ImGui::PopStyleColor(2);
 }else{
  ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(SintetOverlayStyle::Surface(.07f)));
  if(ImGui::Button(OverlayText("Удалить из друзей"),ImVec2(-1,px(36))))confirmFriendRemoval=true;
  ImGui::PopStyleColor();
 }
}

bool overlaySwitch(const char* label,bool& state,bool enabled=true){
 const float scale=interfaceScale;
 const auto position=ImGui::GetCursorScreenPos();
 const float width=ImGui::GetContentRegionAvail().x;
 if(width<=39*scale)return false;
 auto* draw=ImGui::GetWindowDrawList();
 const auto ink=ImGui::GetColorU32(enabled?ImGuiCol_Text:ImGuiCol_TextDisabled);
 const float size=SintetOverlayStyle::secondaryTextSize*scale;
 drawFittedText(draw,ImGui::GetFont(),size,ImVec2(position.x,position.y+(32*scale-size)*.5f),width-42*scale,ink,label);
 ImGui::BeginDisabled(!enabled);
 const bool clicked=ImGui::InvisibleButton(label,ImVec2(width,32*scale));
 if(clicked)state=!state;
 const float x=position.x+width-31*scale,y=position.y+7*scale;
 const auto pill=state?SintetOverlayStyle::style.accent:SintetOverlayStyle::Surface(ImGui::IsItemHovered()?.31f:.23f);
 draw->AddRectFilled(ImVec2(x,y),ImVec2(x+31*scale,y+18*scale),ImGui::GetColorU32(ControlFill(pill,.88f)),9*scale);
 const auto knob=state?SintetOverlayStyle::style.surface:SintetOverlayStyle::style.text;
 draw->AddCircleFilled(ImVec2(x+(state?22.f:9.f)*scale,y+9*scale),5.5f*scale,ImGui::GetColorU32(knob));
 ImGui::EndDisabled();
 return clicked;
}

void drawSocialContent(const SintetSocial::State& state,float height,int content,const SintetSocial::Friend* profile=nullptr) {
 const float scale=interfaceScale;auto px=[scale](float x){return x*scale;};
 if(content==Settings){
  overlaySwitch(OverlayText("Показывать мини-плеер"),showMiniPlayer);
  bool pinned=SintetWidgets::MiniPlayerPinned();
  if(overlaySwitch(OverlayText("Закрепить мини-плеер"),pinned))SintetWidgets::SetMiniPlayerPinned(pinned);
  if(ImGui::Button(OverlayText("Производительность")))SintetWidgets::TogglePerformance();
  ImGui::Separator();
  const bool canUseVoice=visible&&!state.room.empty();
  bool enabled=state.voiceEnabled;
  if(overlaySwitch(OverlayText("Голосовой чат"),enabled,canUseVoice))dispatchSocial("voice-enable",enabled?"true":"false");
  bool spatial=state.voiceSpatial;
  if(overlaySwitch(OverlayText("Пространственный звук"),spatial,canUseVoice))dispatchSocial("voice-spatial",spatial?"true":"false");
  static int voiceRangeDraft=48;
  static bool voiceRangeEditing=false;
  if(!state.room.empty()){
   ImGui::Spacing();
   ImGui::TextUnformatted(OverlayText("Дальность голоса в блоках"));
   if(!voiceRangeEditing||!state.leader)voiceRangeDraft=state.voiceRange;
   ImGui::BeginDisabled(!visible||!state.leader);
   ImGui::SetNextItemWidth(-1);
   ImGui::SliderInt("##voice-range",&voiceRangeDraft,8,128,"%d",ImGuiSliderFlags_AlwaysClamp);
   voiceRangeEditing=ImGui::IsItemActive();
   if(state.leader&&ImGui::IsItemDeactivatedAfterEdit())dispatchSocial("voice-range",std::to_string(voiceRangeDraft));
   ImGui::EndDisabled();
   if(!state.leader)ImGui::TextDisabled("%s",OverlayText("Дальность для всех участников меняет лидер."));
  }else voiceRangeEditing=false;
  if(state.room.empty())ImGui::TextWrapped("%s",OverlayText("Голосовой чат доступен в лобби"));
  else if(!state.voiceError.empty())ImGui::TextWrapped("%s",OverlayText(state.voiceError.c_str()));
  return;
 }
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
 if(content==Friends){
  if(profile){
   drawFriendProfile(state,*profile,scale);
   ImGui::EndDisabled();
   return;
  }
  for(const auto& invite:state.invites){
   ImGui::PushID(invite.id.c_str());
   drawSocialName(headingFont,headingFont->FontSize,ImGui::GetContentRegionAvail().x,invite.sender.name,invite.sender.provider,invite.sender.badges);ImGui::TextDisabled(OverlayText("Приглашает в лобби"));
   const auto details=invitationDetails(invite);
   if(!details.empty())ImGui::TextWrapped("%s",details.c_str());
   socialButton(OverlayText("Принять"),"invite-accept",invite.id);ImGui::SameLine();socialButton(OverlayText("Отклонить"),"invite-decline",invite.id);ImGui::Separator();ImGui::PopID();
  }

  for(const auto& f:state.friends){
   ImGui::PushID(f.person.id.c_str());
   const auto start=ImGui::GetCursorPos();
   const auto top=ImGui::GetCursorScreenPos();
   const float width=ImGui::GetContentRegionAvail().x;
   const bool canInvite=width>=px(220)&&f.status!="pending"&&!f.presence.empty()&&!f.joinableLobby&&(state.room.empty()||state.leader)&&
    std::none_of(state.members.begin(),state.members.end(),[&](const auto& member){return member.person.id==f.person.id;});
   const float rowHeight=px(53),inviteWidth=canInvite?px(30):0;
   if(ImGui::InvisibleButton("##friend-row",ImVec2(std::max(1.f,width-inviteWidth),rowHeight))){
    selectedFriend=f.person.id;
    friendProfileOpen=true;
    confirmFriendRemoval=false;
   }
   auto* storage=ImGui::GetStateStorage();
   const auto hoverId=ImGui::GetID("##hover");
   const float target=ImGui::IsItemHovered()?1.f:0.f;
   float hover=storage->GetFloat(hoverId);
   hover+=(target-hover)*std::min(1.f,ImGui::GetIO().DeltaTime*18.f);
   storage->SetFloat(hoverId,hover);
   auto* draw=ImGui::GetWindowDrawList();
   auto fill=SintetOverlayStyle::Surface(.065f+.08f*hover);
   fill.w=.48f+.26f*hover;
   draw->AddRectFilled(top,ImVec2(top.x+width,top.y+rowHeight),ImGui::GetColorU32(fill),px(11));
   ImGui::SetCursorPos(ImVec2(start.x+px(9),start.y+px(8)));
   socialAvatar(f.person,scale,true);
   ImGui::SetCursorPos(ImVec2(start.x+px(56),start.y+px(7)));
   const float textWidth=width-px(89)-inviteWidth;
   drawSocialName(headingFont,px(19),std::max(0.f,textWidth),f.person.name,f.person.provider,f.person.badges);
   drawFriendPresence(f,ImVec2(top.x+px(56),top.y+px(28)),textWidth,scale);
   const float chevronX=top.x+width-inviteWidth-px(16)+px(2*hover);
   const float centerY=top.y+rowHeight*.5f;
   const ImVec2 chevron[]={{chevronX-px(2),centerY-px(4)},{chevronX+px(2),centerY},{chevronX-px(2),centerY+px(4)}};
   draw->AddPolyline(chevron,3,ImGui::GetColorU32(hover>.5f?ImGuiCol_Text:ImGuiCol_TextDisabled),0,px(1.5f));
   if(canInvite){
    ImGui::SetCursorPos(ImVec2(start.x+width-inviteWidth,start.y+px(11)));
    const auto button=ImGui::GetCursorScreenPos();
    if(ImGui::InvisibleButton("##invite-friend",ImVec2(px(26),px(31))))dispatchSocial("invite",f.person.id);
    if(ImGui::IsItemHovered())ImGui::SetTooltip("%s",OverlayText("Пригласить"));
    const auto ink=ImGui::GetColorU32(ImGui::IsItemHovered()?ImGuiCol_Text:ImGuiCol_TextDisabled);
    draw->AddRectFilled(button,ImGui::GetItemRectMax(),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.15f))),px(8));
    const auto center=ImVec2(button.x+px(13),centerY);
    draw->AddLine(ImVec2(center.x-px(4),center.y),ImVec2(center.x+px(4),center.y),ink,px(1.4f));
    draw->AddLine(ImVec2(center.x,center.y-px(4)),ImVec2(center.x,center.y+px(4)),ink,px(1.4f));
   }
   ImGui::SetCursorPos(ImVec2(start.x,start.y+rowHeight));
   ImGui::Dummy(ImVec2(0,0));
   ImGui::PopID();
  }
  if(!state.friends.empty()){
   ImGui::Dummy(ImVec2(0,px(1)));
   const auto item=ImGui::GetCursorScreenPos();
   const auto label=OverlayText("Открыть лобби");
   const auto width=ImGui::GetContentRegionAvail().x;
   const auto textWidth=headingFont->CalcTextSizeA(px(15),FLT_MAX,0,label).x;
   if(ImGui::InvisibleButton("##go-to-lobby",ImVec2(std::min(width,textWidth+px(38)),px(35))))selectSection(Lobby);
   auto* draw=ImGui::GetWindowDrawList();
   const bool hovered=ImGui::IsItemHovered();
   draw->AddRectFilled(item,ImGui::GetItemRectMax(),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(hovered?.17f:.08f),hovered?.82f:.76f)),px(9));
   const auto ink=ImGui::GetColorU32(ImGuiCol_Text);
   icon(draw,ImVec2(item.x+px(12),item.y+px(10)),1,scale*.64f,ink);
   drawFittedText(draw,headingFont,px(15),ImVec2(item.x+px(33),item.y+px(10)),width-px(35),ink,label);
  }
  if(state.friends.empty()&&state.invites.empty()){
   float y=std::max(px(10),(height-px(210))*.5f);ImGui::Dummy(ImVec2(0,y));
   float width=ImGui::GetWindowWidth();ImVec2 center(ImGui::GetWindowPos().x+width*.5f,ImGui::GetCursorScreenPos().y+px(25));
   auto* draw=ImGui::GetWindowDrawList();draw->AddCircleFilled(center,px(29),ImGui::GetColorU32(SintetOverlayStyle::Accent(.16f)));draw->AddCircle(center,px(24),ImGui::GetColorU32(SintetOverlayStyle::Accent(.42f)),48,px(1));
   icon(draw,ImVec2(center.x-px(12),center.y-px(12)),0,scale,ImGui::GetColorU32(SintetOverlayStyle::style.accent));
   ImGui::Dummy(ImVec2(0,px(60)));
   ImGui::PushFont(headingFont);ImGui::SetCursorPosX((width-ImGui::CalcTextSize(OverlayText("Пока нет друзей")).x)*.5f);ImGui::TextUnformatted(OverlayText("Пока нет друзей"));ImGui::PopFont();
   ImGui::SetCursorPosX((width-ImGui::CalcTextSize(OverlayText("Друзья появятся здесь")).x)*.5f);ImGui::TextDisabled(OverlayText("Друзья появятся здесь"));
   const char* hint=OverlayText("Добавляйте друзей в лаунчере");
   ImGui::SetCursorPosX((width-ImGui::CalcTextSize(hint).x)*.5f);
   ImGui::TextDisabled("%s",hint);
  }
 }else if(content==Lobby){
  if(lobbyInvites){
   if(ImGui::Button(OverlayText("Назад")))lobbyInvites=false;
   ImGui::Spacing();
   bool available=false;
   for(const auto& entry:state.friends){
    if(entry.status=="pending"||entry.presence.empty()||std::any_of(state.members.begin(),state.members.end(),
      [&](const auto& member){return member.person.id==entry.person.id;}))continue;
    available=true;
    ImGui::PushID(entry.person.id.c_str());
    ImGui::BeginGroup();
    socialAvatar(entry.person,scale);
    const auto label=OverlayText("Пригласить");
    const float buttonWidth=ImGui::CalcTextSize(label).x+ImGui::GetStyle().FramePadding.x*2+px(8);
    drawSocialName(headingFont,px(17),std::max(0.f,ImGui::GetContentRegionAvail().x-buttonWidth),
      entry.person.name,entry.person.provider,entry.person.badges);
    ImGui::SameLine();
    socialButton(label,"invite",entry.person.id);
    ImGui::EndGroup();
    ImGui::PopID();
   }
   if(!available)ImGui::TextWrapped("%s",OverlayText("Нет друзей в сети"));
   ImGui::EndDisabled();
   return;
  }
  if(state.hostNeedsPack&&state.leader&&!state.room.empty()){
   ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(12),px(10)));
   ImGui::PushStyleVar(ImGuiStyleVar_ChildRounding,px(11));
   ImGui::PushStyleColor(ImGuiCol_ChildBg,ControlFill(SintetOverlayStyle::Accent(.10f),.68f));
   constexpr auto flags=ImGuiChildFlags_AlwaysUseWindowPadding|ImGuiChildFlags_AutoResizeY|ImGuiChildFlags_AlwaysAutoResize;
   if(ImGui::BeginChild("##restore-lobby-pack",ImVec2(0,0),flags,ImGuiWindowFlags_NoScrollbar)){
    ImGui::TextWrapped("%s",OverlayText("Сборка лобби недоступна"));
    if(ImGui::Button(OverlayText("Восстановить сборку"),ImVec2(-1,px(34))))dispatchSocial("host");
   }
   ImGui::EndChild();
   ImGui::PopStyleColor();ImGui::PopStyleVar(2);
  }
  if(!state.lobbyProvider.empty()&&state.lobbyProvider!="sintet"){
   const ImVec4 amber(.92f,.78f,.45f,1);
   const ImVec2 marker=ImGui::GetCursorScreenPos();
   static bool open=false;
   const auto* title=OverlayText("Лобби без Sintet ID");
   if(ImGui::InvisibleButton("##external-lobby-info",ImVec2(px(22)+ImGui::CalcTextSize(title).x,px(20))))open=!open;
   auto* warningDraw=ImGui::GetWindowDrawList();
   const ImU32 color=ImGui::GetColorU32(amber);
   warningDraw->AddTriangle(ImVec2(marker.x+px(9),marker.y+px(3)),ImVec2(marker.x+px(3),marker.y+px(14)),ImVec2(marker.x+px(15),marker.y+px(14)),color,px(1.3f));
   warningDraw->AddLine(ImVec2(marker.x+px(9),marker.y+px(7)),ImVec2(marker.x+px(9),marker.y+px(10)),color,px(1.2f));
   warningDraw->AddCircleFilled(ImVec2(marker.x+px(9),marker.y+px(12)),px(.65f),color);
   warningDraw->AddText(ImVec2(marker.x+px(22),marker.y),color,title);
   if(open)ImGui::TextWrapped("%s",OverlayText("Для игры с друзьями рекомендуем Sintet ID. Без него доступен 1 ГБ TURN на два месяца. Прямое P2P не тратит этот лимит."));
  }
  constexpr size_t pageSize=12;
  static std::string pagedRoom;
  static size_t memberLimit=pageSize;
  static bool searchOpen=false;
  static char search[96]={};
  static float searchReveal=0;
  if(pagedRoom!=state.room){
   pagedRoom=state.room;memberLimit=pageSize;searchOpen=false;search[0]=0;searchReveal=0;
  }
  const char* capacityTitle=OverlayText("Размер лобби");
  const int occupied=state.members.empty()?state.connected+1:static_cast<int>(state.members.size());
  static std::string capacityRoom;
  static int capacityInput=4;
  const bool canResize=state.leader&&!state.room.empty();
  if(!state.room.empty()||state.localPort){
   const auto cursor=ImGui::GetCursorPos();
   const auto from=ImGui::GetCursorScreenPos();
   const float width=ImGui::GetContentRegionAvail().x;
   auto* draw=ImGui::GetWindowDrawList();
   draw->AddRectFilled(from,ImVec2(from.x+width,from.y+px(54)),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.065f))),px(10));
   icon(draw,ImVec2(from.x+px(11),from.y+px(16)),1,scale*.82f,ImGui::GetColorU32(SintetOverlayStyle::style.accent));
   const std::string name=state.packName.empty()?(buildName.empty()?OverlayText("Играть вместе"):buildName):state.packName;
   const std::string version=state.packGame.empty()?gameVersionLine:"Minecraft "+state.packGame;
   char slots[32];std::snprintf(slots,sizeof(slots),"%d / %d",occupied,state.roomCapacity);
   const float slotsWidth=headingFont->CalcTextSizeA(px(17),FLT_MAX,0,slots).x;
   const float capacityWidth=slotsWidth+px(canResize?48:20);
   const float capacityX=from.x+width-capacityWidth-px(9);
   const float nameWidth=width-capacityWidth-px(63);
   drawFittedText(draw,headingFont,px(18),ImVec2(from.x+px(43),from.y+px(8)),nameWidth,ImGui::GetColorU32(ImGuiCol_Text),name);
   drawFittedText(draw,ImGui::GetFont(),px(SintetOverlayStyle::secondaryTextSize),ImVec2(from.x+px(43),from.y+px(28)),nameWidth,ImGui::GetColorU32(ImGuiCol_TextDisabled),version);
   draw->AddRectFilled(ImVec2(capacityX,from.y+px(13)),ImVec2(capacityX+capacityWidth,from.y+px(41)),
    ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.12f))),px(8));
   draw->AddText(headingFont,px(17),ImVec2(capacityX+px(10),from.y+px(18)),ImGui::GetColorU32(ImGuiCol_Text),slots);
   if(canResize){
    ImGui::SetCursorScreenPos(ImVec2(capacityX+capacityWidth-px(28),from.y+px(13)));
    if(ImGui::InvisibleButton("##lobby-capacity",ImVec2(px(28),px(28)))){
     capacityInput=state.roomCapacity;
     capacityRoom=state.room;
     ImGui::OpenPopup(capacityTitle);
    }
    const auto ink=ImGui::GetColorU32(ImGui::IsItemHovered()?ImGuiCol_Text:ImGuiCol_TextDisabled);
    const ImVec2 center(capacityX+capacityWidth-px(14),from.y+px(27));
    draw->AddLine(ImVec2(center.x-px(4),center.y),ImVec2(center.x+px(4),center.y),ink,px(1.5f));
    draw->AddLine(ImVec2(center.x,center.y-px(4)),ImVec2(center.x,center.y+px(4)),ink,px(1.5f));
    if(ImGui::IsItemHovered())ImGui::SetTooltip("%s",capacityTitle);
   }
   ImGui::SetCursorPos(ImVec2(cursor.x,cursor.y+px(54)));
   ImGui::Dummy(ImVec2(0,0));
  }else socialTitle(OverlayText("Играть вместе"));
  ImGui::SetNextWindowSize(ImVec2(px(390),0),ImGuiCond_Appearing);
  if(ImGui::BeginPopupModal(capacityTitle,nullptr,ImGuiWindowFlags_AlwaysAutoResize|ImGuiWindowFlags_NoSavedSettings)){
   if(!canResize||capacityRoom!=state.room||!visible)ImGui::CloseCurrentPopup();
   ImGui::TextUnformatted(OverlayText("Максимум участников · 4–150"));
   ImGui::SetNextItemWidth(px(160));
   ImGui::InputInt("##capacity",&capacityInput);
   const bool valid=capacityInput>=4&&capacityInput<=150;
   const int excess=valid?std::max(0,occupied-capacityInput):0;
   if(capacityInput>4){
    ImGui::Spacing();
    ImGui::PushStyleColor(ImGuiCol_Text,ImVec4(.92f,.78f,.45f,1));
    ImGui::TextWrapped("%s",OverlayText("Больше 4 участников повышают нагрузку на компьютер и интернет лидера. Возможны лаги и обрывы."));
    ImGui::PopStyleColor();
   }
   if(capacityInput>8){
    ImGui::Spacing();
    ImGui::TextWrapped("%s",OverlayText("При 9 и более участниках лимит TURN в лобби — 1 ГБ на аккаунт на два месяца. Прямое P2P лимит не тратит."));
   }
   if(excess){
    ImGui::Spacing();
    ImGui::PushStyleColor(ImGuiCol_Text,ImVec4(.92f,.78f,.45f,1));
    ImGui::TextWrapped(OverlayText("Будут отключены последние вошедшие участники: %d."),excess);
    ImGui::PopStyleColor();
   }
   ImGui::Spacing();
   if(ImGui::Button(OverlayText("Отмена")))ImGui::CloseCurrentPopup();
   ImGui::SameLine();
   ImGui::BeginDisabled(!canResize||capacityRoom!=state.room||!valid||capacityInput==state.roomCapacity);
   if(ImGui::Button(excess?OverlayText("Отключить лишних и применить"):OverlayText("Применить"))){
    dispatchSocial(excess?"lobby-capacity-confirm":"lobby-capacity",std::to_string(capacityInput));
    ImGui::CloseCurrentPopup();
   }
   ImGui::EndDisabled();
   ImGui::EndPopup();
  }
  if(!state.room.empty()||state.localPort){
   if(state.localPort&&state.worldReady){
    if(ImGui::Button(OverlayText("Скопировать адрес"))){
     char address[40];sprintf_s(address,"127.0.0.1:%d",state.localPort);ImGui::SetClipboardText(address);
    }
   if(!state.inWorld&&!state.gameJoining){
    ImGui::SameLine();
   ImGui::BeginDisabled(!state.gameReady||(!state.packPhase.empty()&&state.packPhase!="ready"));
   ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(SintetOverlayStyle::Accent(.34f)));
   ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(SintetOverlayStyle::Accent(.45f),.82f));
   ImGui::PushStyleColor(ImGuiCol_ButtonActive,ControlFill(SintetOverlayStyle::Accent(.55f),.86f));
    if(ImGui::Button("##auto-join",ImVec2(px(35),ImGui::GetFrameHeight())))dispatchSocial("connect-game");
   ImGui::PopStyleColor(3);
    ImGui::EndDisabled();
    const auto corner=ImGui::GetItemRectMin();
    const ImVec2 center(corner.x+px(17),corner.y+ImGui::GetFrameHeight()*.5f);
    const ImVec2 arrow[]={{center.x-px(4),center.y-px(6)},{center.x+px(6),center.y},{center.x-px(4),center.y+px(6)}};
    ImGui::GetWindowDrawList()->AddTriangleFilled(arrow[0],arrow[1],arrow[2],ImGui::GetColorU32(state.gameReady?ImGuiCol_Text:ImGuiCol_TextDisabled));
   }
   }
   if(state.preGameLobby&&!state.leader&&!state.inWorld){
    bool automatic=state.autoConnect;
    if(ImGui::Checkbox(OverlayText("Подключиться, когда мир готов"),&automatic))dispatchSocial("auto-connect",automatic?"true":"false");
   }
   if(state.leader&&state.friendLobbiesAvailable){
    bool friendsOpen=state.friendsOpen;
    if(overlaySwitch(OverlayText("Открыть лобби для друзей"),friendsOpen))dispatchSocial("lobby-friends-open",friendsOpen?"true":"false");
   }
   if(!state.leader&&!state.lobbyConnected&&!state.busy)socialButton(OverlayText("Восстановить соединение"),"reconnect-lobby");
   if(!state.packPhase.empty()){
    const bool required=state.packPhase=="required"||state.packPhase=="error";
    const bool working=state.packPhase=="downloading"||state.packPhase=="stopping"||state.packPhase=="installing"||state.packPhase=="launching"||state.packPhase=="checking";
    ImGui::Spacing();
    if(required){
     ImGui::TextColored(ImVec4(.9f,.58f,.58f,1),"%s",state.packMissing?OverlayText("Нужна сборка друга"):OverlayText("Нужен перезапуск"));
     if(state.packMissing)ImGui::TextDisabled(OverlayText("%d файлов · %s"),state.packMissing,SintetLayout::TransferSize(state.packBytes).c_str());
     if(!state.packError.empty()){
      ImGui::TextDisabled("%s",OverlayText("Не удалось подготовить сборку"));
      ImGui::TextWrapped("%s",OverlayText(state.packError.c_str()));
     }
     if(!state.packDigest.empty()){
      ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(ImVec4(.28f,.14f,.14f,1)));ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(ImVec4(.37f,.19f,.19f,1),.82f));
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
     if(ImGui::Button(state.preGameLobby?OverlayText("Подготовить сборку"):OverlayText("Скачать и перезапустить"))){dispatchSocial("sync-pack",state.packDigest);ImGui::CloseCurrentPopup();}
     ImGui::EndPopup();
    }
    ImGui::Spacing();ImGui::Separator();ImGui::Spacing();
   }
   const bool searchable=state.members.size()>4;
   bool resetScroll=false;
   if(searchable){
    const float available=ImGui::GetContentRegionAvail().x;
    const float size=ImGui::GetFrameHeight();
    const bool toggleSearch=ImGui::Button("##lobby-search-toggle",ImVec2(size,size));
    const auto pos=ImGui::GetItemRectMin();
    const ImVec2 center(pos.x+size*.46f,pos.y+size*.45f);
    auto* draw=ImGui::GetWindowDrawList();
    const auto ink=ImGui::GetColorU32(ImGuiCol_Text);
    draw->AddCircle(center,px(4),ink,16,px(1.25f));
    draw->AddLine(ImVec2(center.x+px(3),center.y+px(3)),ImVec2(center.x+px(7),center.y+px(7)),ink,px(1.25f));
    if(toggleSearch){
     searchOpen=!searchOpen;memberLimit=pageSize;search[0]=0;resetScroll=true;
    }
    const float progress=std::min(ImGui::GetIO().DeltaTime/.16f,1.f);
    searchReveal=std::clamp(searchReveal+(searchOpen?progress:-progress),0.f,1.f);
    if(searchOpen||searchReveal>0){
     ImGui::SameLine();
     if(toggleSearch&&searchOpen)ImGui::SetKeyboardFocusHere();
     ImGui::SetNextItemWidth(std::max(px(32),(available-size-ImGui::GetStyle().ItemSpacing.x)*easeOut(searchReveal)));
     ImGui::PushStyleColor(ImGuiCol_FrameBg,ControlFill(ImVec4(.15f,.15f,.15f,1)));
     ImGui::BeginDisabled(!searchOpen);
     if(ImGui::InputTextWithHint("##lobby-search",OverlayText("Ник или @юзернейм"),search,sizeof(search))){
      memberLimit=pageSize;resetScroll=true;
     }
     ImGui::EndDisabled();
     ImGui::PopStyleColor();
    }
   }else{
    searchOpen=false;search[0]=0;searchReveal=0;memberLimit=pageSize;
   }
   const auto query=SintetSocial::LobbySearchKey(search);
   std::vector<const SintetSocial::Member*> matches;
   matches.reserve(state.members.size());
   for(const auto& member:state.members){
    if(query.empty()||SintetSocial::LobbySearchKey(member.person.name).find(query)!=std::wstring::npos||
       SintetSocial::LobbySearchKey(member.handle).find(query)!=std::wstring::npos)matches.push_back(&member);
   }
   if(matches.empty()&&!query.empty())ImGui::TextDisabled("%s",OverlayText("Участники не найдены"));
   const bool scrollMembers=searchable&&!matches.empty();
   if(scrollMembers){
    if(resetScroll)ImGui::SetNextWindowScroll(ImVec2(0,0));
    ImGui::BeginChild("lobby-members",ImVec2(0,px(std::min<size_t>(matches.size()*60,190))));
   }
   for(size_t index=0;index<matches.size()&&index<memberLimit;++index){
    const auto& member=*matches[index];
    ImGui::PushID(member.person.id.c_str());
    drawLobbyMember(state,member,scale);
    ImGui::PopID();
   }
   if(scrollMembers)ImGui::EndChild();
   if(memberLimit<matches.size()&&ImGui::Button(OverlayText("Показать ещё")))memberLimit+=pageSize;
   ImGui::Spacing();
   const auto codeCard=ImGui::GetCursorScreenPos();
   const float codeWidth=ImGui::GetContentRegionAvail().x;
   auto* draw=ImGui::GetWindowDrawList();
   draw->AddRectFilled(codeCard,ImVec2(codeCard.x+codeWidth,codeCard.y+px(55)),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.08f))),px(11));
   const auto codeLabel=OverlayText("Код комнаты");
   drawFittedText(draw,ImGui::GetFont(),px(16),ImVec2(codeCard.x+px(12),codeCard.y+px(7)),codeWidth-px(61),ImGui::GetColorU32(ImGuiCol_TextDisabled),codeLabel);
   drawFittedText(draw,headingFont,px(24),ImVec2(codeCard.x+px(12),codeCard.y+px(24)),codeWidth-px(61),ImGui::GetColorU32(ImGuiCol_Text),state.room);
   ImGui::SetCursorScreenPos(ImVec2(codeCard.x+codeWidth-px(42),codeCard.y+px(11)));
   const auto button=ImGui::GetCursorScreenPos();
   if(ImGui::InvisibleButton("##copy-room-code",ImVec2(px(32),px(32))))ImGui::SetClipboardText(state.room.c_str());
   const bool copyHovered=ImGui::IsItemHovered();
   draw->AddRectFilled(button,ImGui::GetItemRectMax(),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(copyHovered?.20f:.12f))),px(8));
   const auto ink=ImGui::GetColorU32(copyHovered?ImGuiCol_Text:ImGuiCol_TextDisabled);
   draw->AddRect(ImVec2(button.x+px(9),button.y+px(8)),ImVec2(button.x+px(19),button.y+px(18)),ink,px(2),0,px(1.4f));
   draw->AddRect(ImVec2(button.x+px(13),button.y+px(12)),ImVec2(button.x+px(23),button.y+px(22)),ink,px(2),0,px(1.4f));
   if(copyHovered)ImGui::SetTooltip("%s",OverlayText("Скопировать код"));
   ImGui::SetCursorScreenPos(ImVec2(codeCard.x,codeCard.y+px(55)));
   ImGui::Dummy(ImVec2(0,0));
   const char* invite=OverlayText("Пригласить");
   const char* leave=OverlayText(state.leader?"Закрыть лобби":"Выйти из лобби");
   const bool canInvite=state.leader&&state.enabled&&state.lobbyProvider=="sintet";
   const float gap=ImGui::GetStyle().ItemSpacing.x;
   const float half=(codeWidth-gap)*.5f;
   const float requiredWidth=std::max(ImGui::CalcTextSize(leave).x,ImGui::CalcTextSize(invite).x)+2*ImGui::GetStyle().FramePadding.x;
   const bool inlineInvite=canInvite&&requiredWidth<=half;
   if(canInvite){
    if(ImGui::Button(invite,ImVec2(inlineInvite?half:-1.f,px(36))))lobbyInvites=true;
    if(inlineInvite)ImGui::SameLine();
   }
   ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(ImVec4(.48f,.19f,.21f,1),.82f));
   ImGui::PushStyleColor(ImGuiCol_ButtonActive,ControlFill(ImVec4(.55f,.22f,.24f,1),.86f));
   if(ImGui::Button(leave,ImVec2(inlineInvite?half:-1.f,px(36))))dispatchSocial("close");
   ImGui::PopStyleColor(2);
  }else{
   ImGui::TextWrapped("%s",state.preGameLobby?OverlayText("Соберитесь в лобби и подготовьте сборку. Мир можно открыть для сети позже."):OverlayText("Откройте одиночный мир для сети в Minecraft"));
   ImGui::PushStyleColor(ImGuiCol_Button,ControlFill(SintetOverlayStyle::Accent(.26f)));
   ImGui::PushStyleColor(ImGuiCol_ButtonHovered,ControlFill(SintetOverlayStyle::Accent(.40f),.82f));
   ImGui::PushStyleColor(ImGuiCol_ButtonActive,ControlFill(SintetOverlayStyle::Accent(.52f),.86f));
   if(ImGui::Button(OverlayText("Создать комнату"),ImVec2(-1,px(38))))dispatchSocial("host");
   ImGui::PopStyleColor(3);
  }
  if(state.room.empty()&&!state.localPort){
  ImGui::Dummy(ImVec2(0,px(16)));ImGui::Separator();ImGui::Dummy(ImVec2(0,px(10)));
  static char code[7]={0};ImGui::TextUnformatted(OverlayText("Войти по коду"));ImGui::SetNextItemWidth(-1);
  ImGui::InputTextWithHint("##room-code",OverlayText("6 цифр"),code,sizeof(code),ImGuiInputTextFlags_CharsDecimal);
  if(ImGui::Button(OverlayText("Подключиться"),ImVec2(-1,px(38))))dispatchSocial("join",code);
  }
 }
 if(content==Lobby&&!state.hostNeedsPack&&!state.connection.empty()){
  ImGui::Spacing();ImGui::TextWrapped("%s",OverlayText(state.connection.c_str()));
 }
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
 const float textSize=SintetOverlayStyle::secondaryTextSize*scale;
 auto* font=ImGui::GetFont();
 const float width=std::min(display.x-24*scale,46*scale+font->CalcTextSizeA(textSize,FLT_MAX,0,label.c_str()).x);
 if(width<46*scale)return false;
 const ImVec2 top(display.x-width-12*scale,12*scale);
 const ImVec2 bottom(top.x+width,top.y+28*scale);
 auto* draw=ImGui::GetForegroundDrawList();
 const auto color=linkColor(quality);
 draw->AddRectFilled(top,bottom,IM_COL32(19,19,20,228),7*scale);
 drawWifi(draw,ImVec2(top.x+18*scale,top.y+19*scale),.72f*scale,ImGui::GetColorU32(color));
 drawFittedText(draw,font,textSize,ImVec2(top.x+33*scale,top.y+5*scale),width-42*scale,IM_COL32(231,231,233,255),label);
 return true;
}

void drawOverlayNotice(){
 const char* notice=SintetScreenshot::Notice();
 if(!notice)notice=SintetSocial::InviteNotice();
 if(!notice)return;
 const float scale=interfaceScale,textSize=SintetOverlayStyle::secondaryTextSize*scale;
 const auto display=ImGui::GetIO().DisplaySize;
 auto* font=ImGui::GetFont();
 const float width=std::min(display.x-20*scale,font->CalcTextSizeA(textSize,FLT_MAX,0,notice).x+30*scale);
 if(width<=24*scale)return;
 const ImVec2 top((display.x-width)*.5f,24*scale);
 auto* draw=ImGui::GetForegroundDrawList();
 draw->AddRectFilled(top,ImVec2(top.x+width,top.y+32*scale),IM_COL32(22,22,23,241),8*scale);
 drawFittedText(draw,font,textSize,ImVec2(top.x+15*scale,top.y+7*scale),width-28*scale,IM_COL32(235,235,236,255),notice);
}

void mediaControl(const SintetMedia::Track&,SintetMedia::Action,double seconds=0);
float approach(float value,float target,float rate,float dt);

void drawTransportButton(ImDrawList* draw,ImVec2 cursor,float scale,const SintetMedia::Track& track,
                         SintetMedia::Action action,bool enabled,bool expanded){
 using SintetMedia::Action;
 ImGui::SetCursorPos(cursor);
 ImGui::PushID(static_cast<int>(action));
 ImGui::BeginDisabled(!enabled);
 const float size=expanded?40.f:31.f;
 if(ImGui::InvisibleButton("##transport",ImVec2(size*scale,size*scale)))mediaControl(track,action);
 const auto start=ImGui::GetItemRectMin(),end=ImGui::GetItemRectMax();
 const ImVec2 center((start.x+end.x)*.5f,(start.y+end.y)*.5f);
 const bool primary=action==Action::Toggle;
 static std::array<float,6> hovers{};
 auto& hover=hovers[static_cast<int>(action)+(expanded?3:0)];
 hover=approach(hover,ImGui::IsItemHovered()?1.f:0.f,24.f,std::min(ImGui::GetIO().DeltaTime,.05f));
 if(primary)draw->AddCircleFilled(center,(size*.5f-.5f+hover)*scale,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::style.text,.90f)));
 else if(hover>.01f){
  auto tint=SintetOverlayStyle::Surface(.20f);tint.w=hover*.7f;
  draw->AddRectFilled(start,end,ImGui::GetColorU32(tint),6*scale);
 }
 const auto color=ImGui::GetColorU32(primary?SintetOverlayStyle::style.surface:SintetOverlayStyle::style.text);
 auto p=[&](float x,float y){return ImVec2(center.x+x*scale,center.y+y*scale);};
 if(action==Action::Toggle&&track.playing){
  draw->AddRectFilled(p(-4,-5),p(-1,5),color,scale);
  draw->AddRectFilled(p(1,-5),p(4,5),color,scale);
 }else{
  const float direction=action==Action::Previous?-1.f:1.f;
  draw->AddTriangleFilled(p(-4*direction,-5),p(4*direction,0),p(-4*direction,5),color);
  if(action!=Action::Toggle)draw->AddLine(p(6*direction,-5),p(6*direction,5),color,1.6f*scale);
 }
 ImGui::EndDisabled();
 ImGui::PopID();
}

void drawCover(ImDrawList* draw,const SintetMedia::Cover& cover,GLuint texture,ImVec2 start,ImVec2 end,float scale){
 const auto side=static_cast<float>(std::min(cover.width,cover.height));
 const ImVec2 inset((1-side/cover.width)*.5f,(1-side/cover.height)*.5f);
 draw->AddImageRounded((ImTextureID)(intptr_t)texture,start,end,inset,ImVec2(1-inset.x,1-inset.y),
                       ImGui::GetColorU32(IM_COL32_WHITE),7*scale);
}


SintetMedia::Track mediaTrack(){
 if(!SintetOverlayStyle::preview)return SintetMedia::Snapshot();
 static const auto album=[]{
  auto art=std::make_shared<SintetMedia::Cover>();
  art->width=art->height=80;art->rgba.resize(80*80*4);
  constexpr ImVec4 colors[]={{1,.88f,.79f,1},{.78f,.49f,.56f,1},{.43f,.30f,.53f,1},{.14f,.13f,.24f,1}};
  for(unsigned y=0;y<80;++y)for(unsigned x=0;x<80;++x){
   const float u=(x-55.f)/74.f,v=(y-18.f)/66.f;
   const float fraction=std::clamp(std::sqrt(u*u+v*v)*3.1f,0.f,2.999f);
   const auto index=static_cast<unsigned>(fraction);
   auto color=SintetOverlayStyle::Blend(colors[index],colors[index+1],fraction-index);
   const auto offset=(y*80+x)*4;
   art->rgba[offset]=static_cast<unsigned char>(color.x*255);
   art->rgba[offset+1]=static_cast<unsigned char>(color.y*255);
   art->rgba[offset+2]=static_cast<unsigned char>(color.z*255);
   art->rgba[offset+3]=255;
  }
  return art;
 }();
 SintetMedia::Track track;
 track.title=OverlayText("После заката");track.artist="Sintet";track.cover=album;
 track.playing=previewPlaying;track.canToggle=track.canSeek=true;
 track.generation=1;track.position=previewPosition;
 track.end=track.seekMax=204;track.measuredAt=GetTickCount64();
 return track;
}

void mediaControl(const SintetMedia::Track& track,SintetMedia::Action action,double seconds){
 if(!SintetOverlayStyle::preview){SintetMedia::Control(track.generation,action,seconds);return;}
 if(action==SintetMedia::Action::Toggle)previewPlaying=!previewPlaying;
 if(action==SintetMedia::Action::Seek)previewPosition=static_cast<float>(seconds);
}

GLuint mediaCoverTexture=0;
std::shared_ptr<const SintetMedia::Cover> mediaCover;
struct PlayedTrack {std::string title,artist;double duration;};
std::deque<PlayedTrack> recentMedia;
void rememberMedia(const SintetMedia::Track& track){
 if(track.title.empty()||(!recentMedia.empty()&&recentMedia.front().title==track.title&&recentMedia.front().artist==track.artist))return;
 recentMedia.erase(std::remove_if(recentMedia.begin(),recentMedia.end(),[&](const auto& item){
  return item.title==track.title&&item.artist==track.artist;
 }),recentMedia.end());
 recentMedia.push_front({track.title,track.artist,track.end-track.start});
 if(recentMedia.size()>3)recentMedia.pop_back();
}
std::string mediaTime(double seconds){
 const auto value=static_cast<int>(std::clamp(seconds,0.,359999.));
 char text[16];std::snprintf(text,sizeof(text),"%d:%02d",value/60,value%60);
 return text;
}
void drawMedia(float scale,const SintetMedia::Track& track,bool expanded,bool interactive){
 using SintetMedia::Action;
 static std::uint64_t seeking=0;
 if(track.title.empty()){
  ImGui::TextWrapped("%s",OverlayText("Запусти музыку в Windows, чтобы управлять ей здесь."));
  return;
 }
 if(!interactive||seeking!=track.generation)seeking=0;
 auto px=[scale](float value){return value*scale;};
 const auto start=ImGui::GetCursorScreenPos();
 const auto cursor=ImGui::GetCursorPos();
 const float width=ImGui::GetContentRegionAvail().x;
 if(width<px(150))return;
 auto* draw=ImGui::GetWindowDrawList();
 auto p=[&](float x,float y){return ImVec2(start.x+px(x),start.y+px(y));};
 const float cover=expanded?81.f:40.f;
 draw->AddRectFilled(p(0,0),p(cover,cover),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.14f))),px(8));
 const auto muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
 if(track.cover&&track.cover!=mediaCover){
  if(!mediaCoverTexture)glGenTextures(1,&mediaCoverTexture);
  if(mediaCoverTexture){
   glBindTexture(GL_TEXTURE_2D,mediaCoverTexture);
   glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
   glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
   glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,track.cover->width,track.cover->height,0,GL_RGBA,GL_UNSIGNED_BYTE,track.cover->rgba.data());
   glBindTexture(GL_TEXTURE_2D,0);
   mediaCover=track.cover;
  }
 }
 if(track.cover&&track.cover==mediaCover)drawCover(draw,*track.cover,mediaCoverTexture,p(0,0),p(cover,cover),scale);
 else icon(draw,p((cover-19)*.5f,(cover-19)*.5f),4,.8f*scale,muted);
 const float controlsWidth=expanded?138.f:93.f;
 const float textWidth=width-px(cover+12+(expanded?0:controlsWidth));
 drawFittedText(draw,headingFont,px(expanded?24:18),p(cover+9,expanded?9:2),textWidth,
  ImGui::GetColorU32(ImGuiCol_Text),track.title);
 drawFittedText(draw,ImGui::GetFont(),px(SintetOverlayStyle::secondaryTextSize),p(cover+9,expanded?36:21),textWidth,muted,track.artist);
 if(expanded){
  unsigned int levels[8]{};
  if(track.playing){
   if(SintetOverlayStyle::preview){
    constexpr unsigned int demoLevels[]={380,650,880,440,700,350,590,420};
    std::copy(std::begin(demoLevels),std::end(demoLevels),levels);
   }else SintetProfileSpectrum(levels,8);
  }
  for(int bar=0;bar<16;++bar){
   const float x=cover+12+4.f*bar;
   const float peak=1.f+7.f*levels[bar%8]/1000.f;
   draw->AddRectFilled(p(x,62-peak),p(x+1.8f,62+peak),ImGui::GetColorU32(SintetOverlayStyle::Surface(.50f)),px(1));
  }
 }
 if(!expanded&&interactive){
  ImGui::SetCursorPos(cursor);
  if(ImGui::InvisibleButton("##expand-media",ImVec2(std::max(px(36),width-px(controlsWidth)),px(40)))){
   selectSection(Music);
  }
 }
 const float controlStart=expanded?cursor.x+(width-px(132))*.5f:cursor.x+width-px(controlsWidth);
 const float controlsY=cursor.y+px(expanded?112:4);
 const float spacing=px(expanded?46:31);
 drawTransportButton(draw,ImVec2(controlStart,controlsY),scale,track,Action::Previous,interactive&&track.canPrevious,expanded);
 drawTransportButton(draw,ImVec2(controlStart+spacing,controlsY),scale,track,Action::Toggle,interactive&&track.canToggle,expanded);
 drawTransportButton(draw,ImVec2(controlStart+2*spacing,controlsY),scale,track,Action::Next,interactive&&track.canNext,expanded);

 if(track.end>track.start){
  const float seekY=expanded?93.f:48.f;
  double position=track.position+(GetTickCount64()-track.measuredAt)*.001*track.rate;
  auto* font=ImGui::GetFont();
  const float timeSize=px(SintetOverlayStyle::secondaryTextSize);
  const auto duration=mediaTime(track.end-track.start);
  const float timeWidth=font->CalcTextSizeA(timeSize,FLT_MAX,0,duration.c_str()).x;
  const float lineLeft=timeWidth+px(8),lineWidth=std::max(px(1),width-2*lineLeft);
  ImGui::SetCursorPos(ImVec2(cursor.x+lineLeft,cursor.y+px(seekY)));
  const bool canSeek=interactive&&track.canSeek;
  ImGui::BeginDisabled(!canSeek);
  ImGui::InvisibleButton("##media-seek",ImVec2(lineWidth,px(15)));
  if(ImGui::IsItemActivated())seeking=track.generation;
  const bool active=ImGui::IsItemActive();
  const bool released=ImGui::IsItemDeactivated()&&ImGui::IsMouseReleased(ImGuiMouseButton_Left);
  if(canSeek&&seeking&&(active||released)){
   const auto ratio=std::clamp((ImGui::GetIO().MousePos.x-start.x-lineLeft)/lineWidth,0.f,1.f);
   position=std::clamp(track.start+ratio*(track.end-track.start),track.seekMin,track.seekMax);
   if(released)mediaControl(track,Action::Seek,position);
  }
  if(!active)seeking=0;
  const bool hovered=canSeek&&(ImGui::IsItemHovered()||active);
  ImGui::EndDisabled();
  const float fill=static_cast<float>(std::clamp((position-track.start)/(track.end-track.start),0.0,1.0))*lineWidth;
  const ImVec2 first(start.x+lineLeft,start.y+px(seekY+6)),last(first.x+lineWidth,first.y+px(3));
  static float hover=0.f;
  hover=approach(hover,hovered?1.f:0.f,22.f,std::min(ImGui::GetIO().DeltaTime,.05f));
  const float center=first.y+px(1.5f),radius=px(2.f+hover);
  const ImVec2 lineStart(first.x,center-radius),lineEnd(last.x,center+radius);
  draw->AddRectFilled(lineStart,lineEnd,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.24f),.82f)),radius);
  if(fill>0)draw->AddRectFilled(lineStart,ImVec2(first.x+fill,lineEnd.y),ImGui::GetColorU32(SintetOverlayStyle::style.accent),radius);
  draw->AddCircleFilled(ImVec2(first.x+fill,center),px(5.f+1.5f*hover),ImGui::GetColorU32(ImGuiCol_Text));
  const auto elapsed=mediaTime(std::clamp(position,track.start,track.end)-track.start);
  draw->AddText(font,timeSize,p(0,seekY-2),muted,elapsed.c_str());
  draw->AddText(font,timeSize,ImVec2(start.x+width-timeWidth,p(0,seekY-2).y),muted,duration.c_str());
 }
 ImGui::SetCursorPos(cursor);
 ImGui::Dummy(ImVec2(width,px(expanded?157:62)));
 if(expanded){
  ImGui::Separator();
  ImGui::TextDisabled("%s",OverlayText("Недавние треки"));
  const PlayedTrack samples[]={
   {OverlayText("После заката"),"Sintet",204},
   {OverlayText("Северный свет"),"Sintet",232},
   {OverlayText("Снег в тишине"),"Sintet",193}
  };
  for(size_t i=0;i<(SintetOverlayStyle::preview?3:recentMedia.size());++i){
   const auto& item=SintetOverlayStyle::preview?samples[i]:recentMedia[i];
   const auto from=ImGui::GetCursorScreenPos();
   const auto to=ImVec2(from.x+width,from.y+px(25));
   const bool active=item.title==track.title&&item.artist==track.artist;
   if(active)draw->AddRectFilled(from,to,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.12f))),px(6));
   const auto ink=ImGui::GetColorU32(active?ImGuiCol_Text:ImGuiCol_TextDisabled);
   icon(draw,ImVec2(from.x+px(8),from.y+px(6)),4,.52f*scale,ink);
   auto* font=ImGui::GetFont();
   const float textSize=px(SintetOverlayStyle::secondaryTextSize);
   const auto duration=item.duration>0?mediaTime(item.duration):std::string{};
   const float durationWidth=font->CalcTextSizeA(textSize,FLT_MAX,0,duration.c_str()).x;
   drawFittedText(draw,font,textSize,ImVec2(from.x+px(28),from.y+px(4)),width-px(46)-durationWidth,ink,item.title);
   if(!duration.empty())draw->AddText(font,textSize,ImVec2(to.x-px(8)-durationWidth,from.y+px(4)),muted,duration.c_str());
   ImGui::Dummy(ImVec2(0,px(21)));
  }
 }
}

bool drawTURNNotice(const SintetSocial::State& state){
 if(state.turnNotice<=0)return false;
 const auto now=std::chrono::duration_cast<std::chrono::milliseconds>(
  std::chrono::system_clock::now().time_since_epoch()).count();
 if(now<state.turnNotice||now-state.turnNotice>8500)return false;
 const float elapsed=(now-state.turnNotice)/1000.0f;
 const float fade=elapsed>8.2f?1-easeOut((elapsed-8.2f)/.3f):easeOut(elapsed/.3f);
 const auto scale=interfaceScale;
 const float width=std::min(330*scale,ImGui::GetIO().DisplaySize.x-24*scale);
 const float size=.92f+.08f*fade;
 ImGui::SetNextWindowPos(ImVec2(ImGui::GetIO().DisplaySize.x-width*size-12*scale,24*scale-(1-fade)*35*scale));
 ImGui::SetNextWindowSize(ImVec2(width*size,0));
 ImGui::PushStyleVar(ImGuiStyleVar_WindowRounding,8*scale);
 ImGui::PushStyleVar(ImGuiStyleVar_Alpha,fade);
 ImGui::Begin("##turn-limit-notice",nullptr,ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|
  ImGuiWindowFlags_AlwaysAutoResize|ImGuiWindowFlags_NoFocusOnAppearing|ImGuiWindowFlags_NoInputs);
 ImGui::SetWindowFontScale(size);
 ImGui::TextWrapped("%s",OverlayText("Лимит TURN подключения закончился"));
 ImGui::End();
 ImGui::PopStyleVar(2);
 return true;
}

void drawInvitation(const SintetSocial::State& state,bool hasNetworkHUD){
 static std::string last;static ULONGLONG started=0,removed=0;static SintetSocial::Invite cached;
 const auto now=GetTickCount64();
 if(!state.invites.empty()){
  cached=state.invites[0];removed=0;
  if(last!=cached.id){last=cached.id;started=now;}
 }else if(!last.empty()&&!removed)removed=now;
 if(last.empty())return;
 const auto& invite=cached;
 float elapsed=(now-started)/1000.f;
 float fade=easeOut(elapsed/.3f);
 if(removed)fade=1-easeOut((now-removed)/300.f);
 else if(!visible&&elapsed>9.7f)fade=1-easeOut((elapsed-9.7f)/.3f);
 if(fade<=0){if(removed){last.clear();removed=0;}return;}
 float scale=interfaceScale,size=.92f+.08f*fade;auto& io=ImGui::GetIO();
 ImGui::SetNextWindowPos(ImVec2(io.DisplaySize.x-(310*size+20)*scale,(visible?70:hasNetworkHUD?48:24)*scale-(1-fade)*35*scale));ImGui::SetNextWindowSize(ImVec2(310*scale*size,0));
 ImGui::PushStyleVar(ImGuiStyleVar_WindowRounding,6*scale);ImGui::PushStyleVar(ImGuiStyleVar_Alpha,fade);
 ImGui::Begin("Game invitation",nullptr,ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|ImGuiWindowFlags_AlwaysAutoResize|ImGuiWindowFlags_NoFocusOnAppearing|(visible?0:ImGuiWindowFlags_NoInputs));
 ImGui::SetWindowFontScale(size);
 drawSocialName(headingFont,headingFont->FontSize*size,ImGui::GetContentRegionAvail().x,
                invite.sender.name,invite.sender.provider,invite.sender.badges);
 ImGui::TextUnformatted(OverlayText("Приглашает вас в лобби"));
 const auto details=invitationDetails(invite);
 if(!details.empty())ImGui::TextWrapped("%s",details.c_str());
 if(visible){ImGui::BeginDisabled(state.busy||removed);socialButton(OverlayText("Принять"),"invite-accept",invite.id);ImGui::SameLine();socialButton(OverlayText("Отклонить"),"invite-decline",invite.id);ImGui::EndDisabled();}
 else ImGui::TextDisabled(OverlayText("%s · Ответить"),overlayHotkey.c_str());
 ImGui::End();ImGui::PopStyleVar(2);
}
void drawSessionProfile(const SintetSocial::State& state,float scale){
 auto px=[scale](float value){return value*scale;};
 ImDrawList* draw=nullptr;
 if(state.turnActive){
  ImGui::Spacing();
  ImGui::PushStyleColor(ImGuiCol_ChildBg,ControlFill(SintetOverlayStyle::Surface(.05f)));
  ImGui::PushStyleVar(ImGuiStyleVar_ChildRounding,px(7));
  ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(9),px(5)));
  ImGui::BeginChild("turn-usage",ImVec2(0,px(43)),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::FixedCard);
  ImGui::PushFont(titleFont);
  ImGui::TextUnformatted(OverlayText("Лимит TURN"));
  ImGui::PopFont();
  if(state.turnLimit>0){
   const auto used=std::min(state.turnUsed,state.turnLimit);
   ImGui::SameLine();
   if(used>=1000000000LL)ImGui::TextDisabled("%.2f %s / %.0f %s",used/1e9,OverlayText("ГБ"),state.turnLimit/1e9,OverlayText("ГБ"));
   else if(used>=1000000LL)ImGui::TextDisabled("%.2f %s / %.0f %s",used/1e6,OverlayText("МБ"),state.turnLimit/1e9,OverlayText("ГБ"));
   else if(used>=1000LL)ImGui::TextDisabled("%.1f %s / %.0f %s",used/1e3,OverlayText("КБ"),state.turnLimit/1e9,OverlayText("ГБ"));
   else ImGui::TextDisabled("%lld B / %.0f %s",static_cast<long long>(used),state.turnLimit/1e9,OverlayText("ГБ"));
   ImGui::PushStyleColor(ImGuiCol_FrameBg,ControlFill(ImVec4(.22f,.22f,.22f,1)));
   const float fraction=std::clamp(static_cast<float>(used)/state.turnLimit,0.0f,1.0f);
   const float first=std::min(fraction*2.0f,1.0f);
   const float second=std::max(fraction*2.0f-1.0f,0.0f);
   const ImVec4 color(.45f+.48f*first,.78f+.02f*first-.42f*second,.54f-.25f*first-.06f*second,1);
   ImGui::PushStyleColor(ImGuiCol_PlotHistogram,color);
   ImGui::PushStyleVar(ImGuiStyleVar_FrameRounding,px(4));
   ImGui::ProgressBar(fraction,ImVec2(-1,px(4)),"");
   ImGui::PopStyleVar();
   ImGui::PopStyleColor(2);
  }
  ImGui::EndChild();
  ImGui::PopStyleVar(2);
  ImGui::PopStyleColor();
 }
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(px(16),px(10)));
 ImGui::PushStyleVar(ImGuiStyleVar_ItemSpacing,ImVec2(px(8),px(3)));
 ImGui::BeginChild("Player",ImVec2(0,px(70)),ImGuiChildFlags_AlwaysUseWindowPadding,SintetLayout::FixedCard);
 char nick[128]={0};GetEnvironmentVariableA("SINTET_OVERLAY_PLAYER",nick,128);
 static const auto gameProvider=[]{
  char provider[16]={0};GetEnvironmentVariableA("SINTET_OVERLAY_PROVIDER",provider,sizeof(provider));
  return SintetSocial::ParseAccountProvider(provider);
 }();
 ImVec2 avatar=ImGui::GetCursorScreenPos();avatar.y+=px(6);
 draw=ImGui::GetWindowDrawList();drawAvatar(draw,avatar,scale);
 ImGui::SetCursorPos(ImVec2(px(66),px(10)));
 const bool currentProfile=[&]{
  if(state.self.id.empty()||state.self.provider!=gameProvider)return false;
  if(gameProvider==SintetSocial::AccountProvider::Sintet){
   char id[80]={0};
   return GetEnvironmentVariableA("SINTET_OVERLAY_ID",id,sizeof(id))==36&&state.self.id==id;
  }
  return *nick&&state.self.name==nick;
 }();
 static const std::vector<std::string> noBadges;
 drawSocialName(headingFont,px(20),ImGui::GetWindowWidth()-px(116),SintetOverlayStyle::preview?state.self.name:(*nick?nick:"Player"),gameProvider,
                currentProfile?state.self.badges:noBadges);
 ImGui::SetCursorPosX(px(66));ImGui::TextDisabled(OverlayText("В игре"));
 ImGui::SetCursorPos(ImVec2(ImGui::GetWindowWidth()-px(40),px(20)));
 ImVec2 copy=ImGui::GetCursorScreenPos();
 if(ImGui::InvisibleButton("Copy player ID",ImVec2(px(26),px(26)))){
  char id[80]={0};GetEnvironmentVariableA("SINTET_OVERLAY_ID",id,sizeof(id));
  if(*id){ImGui::SetClipboardText(id);copiedUntil=(float)ImGui::GetTime()+2;}
 }
 bool hovered=ImGui::IsItemHovered();
 const bool copied=(float)ImGui::GetTime()<copiedUntil;
 ImU32 copyColor=ImGui::GetColorU32(copied?SintetOverlayStyle::Accent(1):hovered?ImVec4(.9f,.9f,.9f,1):ImVec4(.55f,.55f,.55f,1));
 draw->AddRect(ImVec2(copy.x+px(6),copy.y+px(5)),ImVec2(copy.x+px(17),copy.y+px(17)),copyColor,px(2),0,px(1.4f));
 draw->AddRect(ImVec2(copy.x+px(10),copy.y+px(9)),ImVec2(copy.x+px(21),copy.y+px(21)),copyColor,px(2),0,px(1.4f));
 ImGui::EndChild();ImGui::PopStyleVar(2);
 drawGameProfile(scale);
}

void selectSection(int next){
 if(next==Screenshots&&section!=Screenshots&&!SintetOverlayStyle::preview)SintetScreenshot::RefreshGallery();
 section=next;
 lobbyInvites=false;
 selectedFriend.clear();
 friendProfileOpen=confirmFriendRemoval=false;
 friendPageProgress=0;
 selectedScreenshot.clear();
}

constexpr ImGuiWindowFlags floatingFlags=ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|
 ImGuiWindowFlags_NoMove|ImGuiWindowFlags_NoBackground|ImGuiWindowFlags_NoScrollWithMouse;

bool beginFloatingCard(const char* id,ImVec2 position,ImVec2 size,float alpha,
                       SintetAcrylic::Backdrop& glass,bool interactive=true){
 const auto& material=SintetOverlayStyle::style;
 const float scale=interfaceScale;
 ImGui::SetNextWindowPos(position);
 ImGui::SetNextWindowSize(size);
 ImGui::PushStyleVar(ImGuiStyleVar_Alpha,alpha);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(16*scale,13*scale));
 ImGui::PushStyleVar(ImGuiStyleVar_WindowBorderSize,0);
 const bool content=ImGui::Begin(id,nullptr,floatingFlags|(visible&&interactive?0:ImGuiWindowFlags_NoInputs));
 if(content)SintetAcrylic::DrawSurface(glass,ImGui::GetWindowDrawList(),position,size,scale,alpha,material.rounding*scale,overlaySceneDim());
 return content;
}
void endFloatingCard(){ImGui::End();ImGui::PopStyleVar(3);}

enum class CardBack { None, Screenshot, Friend };

void floatingTitle(const char* name,CardBack back=CardBack::None,const char* meta=nullptr,float inset=0.f){
 const float scale=interfaceScale;
 if(inset>0)ImGui::SetCursorPosY(ImGui::GetCursorPosY()+inset*scale);
 const auto cursor=ImGui::GetCursorPos();
 if(back==CardBack::Screenshot){
  if(ImGui::SmallButton(OverlayText("Назад")))selectedScreenshot.clear();
  ImGui::SameLine();
 }else if(back==CardBack::Friend){
  const auto iconTop=ImGui::GetCursorScreenPos();
  if(ImGui::InvisibleButton("##back-to-friends",ImVec2(30*scale,30*scale))){
   friendProfileOpen=false;
   confirmFriendRemoval=false;
  }
  const bool hovered=ImGui::IsItemHovered();
  auto* draw=ImGui::GetWindowDrawList();
  const auto fill=ControlFill(SintetOverlayStyle::Surface(hovered?.20f:.10f),hovered?.82f:.76f);
  draw->AddRectFilled(iconTop,ImGui::GetItemRectMax(),ImGui::GetColorU32(fill),9*scale);
  const auto ink=ImGui::GetColorU32(hovered?ImGuiCol_Text:ImGuiCol_TextDisabled);
  const ImVec2 arrow[]={{iconTop.x+17*scale,iconTop.y+10*scale},{iconTop.x+12*scale,iconTop.y+15*scale},{iconTop.x+17*scale,iconTop.y+20*scale}};
  draw->AddPolyline(arrow,3,ink,0,1.65f*scale);
  if(hovered)ImGui::SetTooltip("%s",OverlayText("Назад"));
  ImGui::SameLine();
 }
 if(back!=CardBack::None)ImGui::SetCursorPosY(cursor.y);
 const auto start=ImGui::GetCursorScreenPos();
 const float titleSize=(inset>0?30.f:25.f)*scale;
 drawFittedText(ImGui::GetWindowDrawList(),titleFont,titleSize,start,ImGui::GetContentRegionAvail().x-46*scale,
  ImGui::GetColorU32(ImGuiCol_Text),name);
 ImGui::Dummy(ImVec2(0,titleSize));
 const auto next=ImGui::GetCursorPos();
 ImGui::SetCursorPos(ImVec2(ImGui::GetWindowWidth()-43*scale,cursor.y));
 if(ImGui::InvisibleButton("##card-close",ImVec2(27*scale,27*scale))){
  if(back==CardBack::Screenshot)selectedScreenshot.clear();
  else selectSection(NoSection);
 }
 const auto p=ImGui::GetItemRectMin();
 auto* draw=ImGui::GetWindowDrawList();
 const auto ink=ImGui::GetColorU32(ImGui::IsItemHovered()?ImGuiCol_Text:ImGuiCol_TextDisabled);
 draw->AddLine(ImVec2(p.x+9*scale,p.y+9*scale),ImVec2(p.x+18*scale,p.y+18*scale),ink,1.4f*scale);
 draw->AddLine(ImVec2(p.x+18*scale,p.y+9*scale),ImVec2(p.x+9*scale,p.y+18*scale),ink,1.4f*scale);
 if(meta&&*meta){
  const float size=SintetOverlayStyle::secondaryTextSize*scale,y=p.y+(27*scale-size)*.5f;
  const float w=ImGui::GetFont()->CalcTextSizeA(size,FLT_MAX,0,meta).x;
  const float left=p.x-w-15*scale;
  if(left>ImGui::GetWindowPos().x+120*scale){
   auto fill=SintetOverlayStyle::Surface(.13f);fill.w=.58f;
   draw->AddRectFilled(ImVec2(left-18*scale,y-3*scale),ImVec2(left+w+8*scale,y+size+3*scale),ImGui::GetColorU32(fill),10*scale);
   const ImVec4 status(.50f,.84f,.67f,1);
   draw->AddCircleFilled(ImVec2(left-9*scale,y+size*.5f+1*scale),2.8f*scale,ImGui::GetColorU32(status));
   draw->AddText(ImGui::GetFont(),size,ImVec2(left,y),ImGui::GetColorU32(ImGuiCol_TextDisabled),meta);
  }
 }
 ImGui::SetCursorPos(ImVec2(cursor.x,std::max(next.y,cursor.y+(inset>0?44.f:33.f)*scale)));
}


constexpr std::array<int,6> dockSections{Friends,Lobby,Music,Mixer,Screenshots,Settings};

float approach(float value,float target,float rate,float dt){
 return value+(target-value)*(1.f-std::exp(-rate*dt));
}

struct PanelMotion {
 int shown=NoSection;
 float progress=0,direction=1;
 void update(int next,float slideDirection,float seconds){
  if(shown!=next){
   direction=slideDirection;
   progress=std::max(0.f,progress-seconds/.12f);
   if(progress==0.f)shown=next;
  }
  if(shown==next&&shown!=NoSection)progress=std::min(1.f,progress+seconds/.19f);
 }
 float opacity() const {return progress*progress*(3.f-2.f*progress);}
 float offset(int next) const {return (shown==next?direction:-direction)*(1.f-opacity());}
};
PanelMotion workspaceMotion, playerMotion;

float drawDock(float alpha,float margin,float height){
 const auto& io=ImGui::GetIO();
 const auto screen=io.DisplaySize;
 const float scale=interfaceScale,seconds=std::min(io.DeltaTime,.05f);
 const char* names[]={OverlayText("Друзья"),OverlayText("Лобби"),OverlayText("Музыка"),
  OverlayText("Звук"),OverlayText("Снимки"),OverlayText("Настройки")};
 constexpr int icons[]={0,1,4,6,5,2};
 constexpr float referenceWidth[]={85.5f,81.25f,89.66f,69.83f,88.92f,106.42f};
 const float labelSize=18*scale,gap=5*scale;
 std::array<float,dockSections.size()> widths{}, starts{};
 float width=107*scale+5*gap;
 for(size_t i=0;i<widths.size();++i){
  const float textWidth=headingFont->CalcTextSizeA(labelSize,FLT_MAX,0,names[i]).x;
  widths[i]=std::max(referenceWidth[i]*scale,textWidth+39*scale);
  width+=widths[i];
 }
 const bool compact=width+2*margin>screen.x;
 if(compact){widths.fill(40*scale);width=107*scale+5*gap+40*scale*widths.size();}
 if(width+margin*2>screen.x)return screen.x;
 const ImVec2 position((screen.x-width)*.5f,screen.y-margin-height+(1-alpha)*(height+margin));
 if(beginFloatingCard("##floating-dock",position,ImVec2(width,height),alpha,dockAcrylic,!SintetWidgets::GhostMode())){
  auto* draw=ImGui::GetWindowDrawList();
  const float buttonHeight=height-14*scale;
  float x=51*scale;
  static std::array<float,dockSections.size()> hover{};
  size_t active=widths.size();
  for(size_t i=0;i<widths.size();++i){
   starts[i]=x;
   ImGui::SetCursorPos(ImVec2(x,7*scale));
   if(ImGui::InvisibleButton(names[i],ImVec2(widths[i],buttonHeight)))
    selectSection(section==dockSections[i]?NoSection:dockSections[i]);
   hover[i]=approach(hover[i],ImGui::IsItemHovered()?1.f:0.f,23.f,seconds);
   if(section==dockSections[i])active=i;
   x+=widths[i]+gap;
  }
  const auto origin=[&](float relative,float y){return ImVec2(position.x+relative,position.y+y*scale);};
  const auto divider=ImGui::GetColorU32(ImVec4(1,1,1,.13f));
  draw->AddLine(origin(43*scale,17),origin(43*scale,37),divider,scale);
  draw->AddLine(origin(x+2*scale,17),origin(x+2*scale,37),divider,scale);
  logo(draw,origin(13*scale,17),.56f*scale,ImGui::GetColorU32(ImGuiCol_Text));
  static float activeX=0,activeWidth=0,activeAlpha=0;
  if(active<widths.size()){
   if(activeAlpha<.015f){activeX=starts[active];activeWidth=widths[active];}
   activeX=approach(activeX,starts[active],25.f,seconds);
   activeWidth=approach(activeWidth,widths[active],25.f,seconds);
  }
  activeAlpha=approach(activeAlpha,active<widths.size()?1.f:0.f,22.f,seconds);
  auto highlight=SintetOverlayStyle::Accent(.20f);highlight.w=activeAlpha;
  draw->AddRectFilled(origin(activeX,7),origin(activeX+activeWidth,47),ImGui::GetColorU32(ControlFill(highlight,.82f)),8*scale);
  for(size_t i=0;i<widths.size();++i){
   const auto top=origin(starts[i],7),end=origin(starts[i]+widths[i],47);
   if(i!=active&&hover[i]>.01f){
    auto fill=SintetOverlayStyle::Surface(.14f);fill.w=hover[i]*.64f;
    draw->AddRectFilled(top,end,ImGui::GetColorU32(fill),8*scale);
   }
   const auto ink=i==active?ImGui::GetColorU32(ImGuiCol_Text):ImGui::GetColorU32(SintetOverlayStyle::Surface(.88f));
   icon(draw,ImVec2(top.x+(compact?11.f:10.f)*scale,top.y+(buttonHeight-16*scale)*.5f),icons[i],.69f*scale,ink);
   if(!compact)draw->AddText(headingFont,labelSize,ImVec2(top.x+32*scale,top.y+(buttonHeight-labelSize)*.5f),ink,names[i]);
  }
  ImGui::SetCursorPos(ImVec2(x+10*scale,10*scale));
  if(ImGui::InvisibleButton("##return-to-game",ImVec2(34*scale,34*scale)))closePanel();
  const auto p=ImGui::GetItemRectMin();
  const auto ink=ImGui::GetColorU32(ImGui::IsItemHovered()?ImGuiCol_Text:ImGuiCol_TextDisabled);
  const float cy=p.y+17*scale;
  draw->AddLine(ImVec2(p.x+13*scale,cy-4*scale),ImVec2(p.x+21*scale,cy+4*scale),ink,1.4f*scale);
  draw->AddLine(ImVec2(p.x+21*scale,cy-4*scale),ImVec2(p.x+13*scale,cy+4*scale),ink,1.4f*scale);
 }
 endFloatingCard();
 return position.x+width;
}

struct GalleryTextures {
 std::shared_ptr<const SintetScreenshot::Gallery> images;
 std::shared_ptr<const SintetScreenshot::Image> image;
 std::vector<GLuint> previews;
 size_t page=0;
 GLuint detail=0;
 void clear(){
  if(!previews.empty())glDeleteTextures(static_cast<GLsizei>(previews.size()),previews.data());
  if(detail)glDeleteTextures(1,&detail);
  *this={};
 }
};
GalleryTextures screenshotTextures;
GLuint uploadScreenshot(const SintetScreenshot::Image& image){
 if(image.rgba.empty())return 0;
 GLuint texture=0;glGenTextures(1,&texture);glBindTexture(GL_TEXTURE_2D,texture);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MIN_FILTER,GL_LINEAR);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_MAG_FILTER,GL_LINEAR);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_S,0x812F);
 glTexParameteri(GL_TEXTURE_2D,GL_TEXTURE_WRAP_T,0x812F);
 glTexImage2D(GL_TEXTURE_2D,0,GL_RGBA,image.width,image.height,0,GL_RGBA,GL_UNSIGNED_BYTE,image.rgba.data());
 glBindTexture(GL_TEXTURE_2D,0);
 return texture;
}

void drawScreenshotImage(const SintetScreenshot::Image& image,GLuint texture,ImVec2 position,ImVec2 bounds,float rounding){
 if(!texture||!image.width||!image.height||bounds.x<=0||bounds.y<=0)return;
 const float fit=std::min(bounds.x/image.width,bounds.y/image.height);
 const ImVec2 size(image.width*fit,image.height*fit);
 const ImVec2 top(position.x+(bounds.x-size.x)*.5f,position.y+(bounds.y-size.y)*.5f);
 ImGui::GetWindowDrawList()->AddImageRounded((ImTextureID)(intptr_t)texture,top,ImVec2(top.x+size.x,top.y+size.y),
  ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(ImVec4(1,1,1,1)),rounding);
}

size_t screenshotPage=0;

void drawScreenshotGallery(float scale,const std::shared_ptr<const SintetScreenshot::Gallery>& catalog){
 const size_t count=catalog?catalog->size():0;
 const size_t pages=count?(count-1)/SintetScreenshot::PageSize+1:1;
 screenshotPage=std::min(screenshotPage,pages-1);
 const float width=ImGui::GetContentRegionAvail().x;
 if(ImGui::Button(OverlayText("Сделать снимок"))&&!SintetOverlayStyle::preview)SintetScreenshot::Request();
 ImGui::SameLine();
 if(ImGui::Button(OverlayText("Обновить"))&&!SintetOverlayStyle::preview)SintetScreenshot::RefreshGallery();
 if(count==0){
  ImGui::TextWrapped("%s",OverlayText(catalog?"Здесь будут снимки Minecraft и оверлея.":"Загружаем снимки…"));
  return;
 }
 if(pages>1){
  char position[48];std::snprintf(position,sizeof(position),"%zu / %zu",screenshotPage+1,pages);
  const float navWidth=ImGui::CalcTextSize(position).x+2*ImGui::GetFrameHeight()+10*scale;
  const float left=ImGui::GetCursorPosX()+width-navWidth;
  if(ImGui::GetItemRectMax().x+navWidth+12*scale<=ImGui::GetWindowPos().x+ImGui::GetCursorPosX()+width)
   ImGui::SameLine(left);
  else ImGui::SetCursorPosX(left);
  ImGui::BeginDisabled(screenshotPage==0);
  if(ImGui::ArrowButton("##previous-page",ImGuiDir_Left))--screenshotPage;
  ImGui::EndDisabled();
  ImGui::SameLine(0,5*scale);ImGui::TextUnformatted(position);
  ImGui::SameLine(0,5*scale);
  ImGui::BeginDisabled(screenshotPage+1>=pages);
  if(ImGui::ArrowButton("##next-page",ImGuiDir_Right))++screenshotPage;
  ImGui::EndDisabled();
 }
 const bool changed=screenshotTextures.images!=catalog||screenshotTextures.page!=screenshotPage;
 if(changed){
  screenshotTextures.clear();
  screenshotTextures.images=catalog;
  screenshotTextures.page=screenshotPage;
  ImGui::SetNextWindowScroll(ImVec2(0,0));
 }
 const auto thumbs=SintetScreenshot::Thumbnails(catalog,screenshotPage);
 const size_t first=screenshotPage*SintetScreenshot::PageSize,end=std::min(count,first+SintetScreenshot::PageSize);
 screenshotTextures.previews.resize(end-first,0);
 ImGui::Spacing();
 ImGui::SetCursorPosX(ImGui::GetCursorPosX()-scrollInset*scale);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(scrollInset*scale,5*scale));
 if(ImGui::BeginChild("##screenshot-grid",ImVec2(0,0),ImGuiChildFlags_AlwaysUseWindowPadding,ImGuiWindowFlags_NoBackground)){
  const int columns=end-first==1?1:width>=510*scale&&end-first>2?3:2;
  ImGui::PushStyleVar(ImGuiStyleVar_CellPadding,ImVec2(5*scale,5*scale));
  if(ImGui::BeginTable("##screenshot-tiles",columns,ImGuiTableFlags_SizingStretchSame|ImGuiTableFlags_NoPadOuterX)){
   for(size_t i=first;i<end;++i){
    ImGui::TableNextColumn();ImGui::PushID(static_cast<int>(i-first));
    const auto& item=(*catalog)[i];
    const float tile=ImGui::GetContentRegionAvail().x;
    const float height=std::min(tile*9.f/16,278*scale);
    const auto at=ImGui::GetCursorScreenPos();
    if(tile>1&&ImGui::InvisibleButton("##snapshot",ImVec2(tile,height+42*scale))){
     selectedScreenshot=item.filename;SintetScreenshot::OpenImage(selectedScreenshot);
    }
    if(tile>1&&ImGui::IsItemVisible()){
     auto* draw=ImGui::GetWindowDrawList();
     const bool hovered=ImGui::IsItemHovered();
     const auto color=ControlFill(SintetOverlayStyle::Surface(hovered?.14f:.07f),.74f);
     draw->AddRectFilled(at,ImVec2(at.x+tile,at.y+height+42*scale),ImGui::GetColorU32(color),10*scale);
     const size_t index=i-first;
     if(index<thumbs.size()&&thumbs[index]){
      auto& texture=screenshotTextures.previews[index];
      if(!texture)texture=uploadScreenshot(*thumbs[index]);
      drawScreenshotImage(*thumbs[index],texture,at,ImVec2(tile,height),8*scale);
     }
     auto* font=ImGui::GetFont();
     const auto muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
     const auto label=item.label.empty()?item.date:item.label;
     drawFittedText(draw,font,17*scale,ImVec2(at.x+9*scale,at.y+height+(item.label.empty()?12:3)*scale),
      tile-18*scale,item.label.empty()?muted:ImGui::GetColorU32(ImGuiCol_Text),label);
     if(!item.label.empty())drawFittedText(draw,font,15*scale,ImVec2(at.x+9*scale,at.y+height+22*scale),tile-18*scale,muted,item.date);
    }
    ImGui::PopID();
   }
   ImGui::EndTable();
  }
  ImGui::PopStyleVar();
 }
 ImGui::EndChild();
 ImGui::PopStyleVar();
}
void drawScreenshotViewer(float scale,float margin,float dockHeight,float alpha,float offset){
 const auto screen=ImGui::GetIO().DisplaySize;
 const ImVec2 size(std::min(screen.x-2*margin,940*scale),std::min(screen.y-dockHeight-4*margin,650*scale));
 if(size.x<=100*scale||size.y<=100*scale)return;
 if(beginFloatingCard("##snapshot-viewer",ImVec2((screen.x-size.x)*.5f,(screen.y-size.y)*.5f+offset),size,alpha,acrylic)){
  floatingTitle(OverlayText("Скриншоты"),CardBack::Screenshot);
  const auto image=SintetScreenshot::Selected();
  if(selectedScreenshot.empty()){
   endFloatingCard();
   return;
  }
  if(image&&image->filename==selectedScreenshot&&!image->rgba.empty()){
   if(ImGui::Button(OverlayText("Скопировать снимок")))SintetScreenshot::CopyImage(window,selectedScreenshot);
   ImGui::SameLine();
   drawFittedText(ImGui::GetWindowDrawList(),ImGui::GetFont(),17*scale,ImGui::GetCursorScreenPos(),ImGui::GetContentRegionAvail().x,
    ImGui::GetColorU32(ImGuiCol_TextDisabled),image->label);
   ImGui::Dummy(ImVec2(0,ImGui::GetFrameHeight()));
   if(image!=screenshotTextures.image){
    if(screenshotTextures.detail)glDeleteTextures(1,&screenshotTextures.detail);
    screenshotTextures.image=image;
    screenshotTextures.detail=uploadScreenshot(*image);
   }
   drawScreenshotImage(*image,screenshotTextures.detail,ImGui::GetCursorScreenPos(),ImGui::GetContentRegionAvail(),8*scale);
  }else ImGui::TextDisabled("%s",OverlayText(image&&image->filename==selectedScreenshot?"Не удалось открыть снимок":"Загружаем снимок…"));
 }
 endFloatingCard();
}


void drawFriendsPages(const SintetSocial::State& state,const SintetSocial::Friend* profile){
 const auto origin=ImGui::GetCursorScreenPos();
 const auto available=ImGui::GetContentRegionAvail();
 const float inset=scrollInset*interfaceScale;
 const ImVec2 size(available.x+inset,available.y);
 const float progress=friendPageProgress*friendPageProgress*(3.f-2.f*friendPageProgress);
 const float distance=36*interfaceScale;
 const float alpha=ImGui::GetStyle().Alpha;
 const auto page=[&](const char* id,const SintetSocial::Friend* entry,float opacity,float offset){
  if(opacity<=0)return;
  ImGui::SetCursorScreenPos(ImVec2(origin.x+offset-inset,origin.y));
  ImGui::PushStyleVar(ImGuiStyleVar_Alpha,alpha*opacity);
  ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(inset,5*interfaceScale));
  const auto flags=ImGuiWindowFlags_NoBackground|(opacity<1.f?ImGuiWindowFlags_NoInputs:0);
  if(ImGui::BeginChild(id,size,ImGuiChildFlags_AlwaysUseWindowPadding,flags))drawSocialContent(state,size.y,Friends,entry);
  ImGui::EndChild();
  ImGui::PopStyleVar(2);
 };
 page("##friend-list",nullptr,1.f-progress,-distance*progress);
 if(profile){
  ImGui::PushID(profile->person.id.c_str());
  page("##friend-profile",profile,progress,distance*(1.f-progress));
  ImGui::PopID();
 }
}

float drawWorkspace(const SintetSocial::State& state,float alpha,float seconds,bool open) {
 const auto& io=ImGui::GetIO();
 const float scale=interfaceScale;
 const float margin=23*scale,dockHeight=54*scale;
 const auto& material=SintetOverlayStyle::style;
 ImGui::GetBackgroundDrawList()->AddRectFilled(ImVec2(0,0),io.DisplaySize,
  IM_COL32(0,0,0,static_cast<int>(255*overlaySceneDim())));
 const float dockRight=drawDock(alpha,margin,dockHeight);

 const int workspaceRequest=section==Music||section==Mixer?NoSection:section;
 const auto previous=std::find(dockSections.begin(),dockSections.end(),workspaceMotion.shown);
 const auto next=std::find(dockSections.begin(),dockSections.end(),workspaceRequest);
 workspaceMotion.update(workspaceRequest,next>=previous?1.f:-1.f,seconds);
 const auto content=workspaceMotion.shown;
 const float panelAlpha=alpha*workspaceMotion.opacity();
 const float bodyWidth=std::min((content==Screenshots?std::max(690.f,material.width):material.width)*scale,io.DisplaySize.x-2*margin);
 const float availableHeight=io.DisplaySize.y-dockHeight-3*margin;
 static float viewerReveal=0.f;
 static float measuredLobbyHeight=0.f;
 static float measuredSettingsHeight=0.f;
 if(content==Screenshots&&!selectedScreenshot.empty()){
  viewerReveal=std::min(1.f,viewerReveal+seconds/.19f);
  const float reveal=easeOut(viewerReveal);
  drawScreenshotViewer(scale,margin,dockHeight,panelAlpha*reveal,12*scale*(1-reveal));
 }else if(panelAlpha>.003f&&content!=NoSection&&availableHeight>100*scale&&bodyWidth>150*scale){
  viewerReveal=0;
  const float lobbyBaseHeight=lobbyInvites?std::clamp(128.f+50.f*static_cast<float>(state.friends.size()),210.f,480.f):
   state.room.empty()?348.f:std::clamp(277.f+60.f*static_cast<float>(state.members.size()),370.f,670.f);
  const float lobbyHeight=std::max(lobbyBaseHeight,measuredLobbyHeight);
  static const auto emptyPreviewGallery=std::make_shared<const SintetScreenshot::Gallery>();
  const auto recent=content==Screenshots?(SintetOverlayStyle::preview?emptyPreviewGallery:SintetScreenshot::Catalog()):nullptr;
  const size_t screenshotCount=recent?recent->size():0;
  const float galleryHeight=screenshotCount==0?152.f:screenshotCount<=2?470.f:640.f;
  const SintetSocial::Friend* profile=nullptr;
  if(content==Friends){
   if(!selectedFriend.empty()){
    const auto entry=std::find_if(state.friends.begin(),state.friends.end(),[](const auto& item){return item.person.id==selectedFriend;});
    if(entry!=state.friends.end())profile=&*entry;
    else friendProfileOpen=false;
   }
   friendPageProgress=std::clamp(friendPageProgress+(friendProfileOpen?1.f:-1.f)*seconds/.20f,0.f,1.f);
   if(friendPageProgress==0&&!friendProfileOpen)selectedFriend.clear();
  }
  const float listHeight=std::clamp(139.f+59.f*static_cast<float>(state.friends.size())+115.f*static_cast<float>(state.invites.size()),246.f,432.f);
  const float profileHeight=profile?friendProfileExtra(*profile)+(confirmFriendRemoval||!profile->presence.empty()||profile->incoming?275.f:234.f):234.f;
  const float pageReveal=friendPageProgress*friendPageProgress*(3.f-2.f*friendPageProgress);
  const float friendsHeight=listHeight+(profileHeight-listHeight)*pageReveal;
  const float settingsHeight=std::max(state.room.empty()?448.f:state.leader?496.f:524.f,measuredSettingsHeight);
  const float preferredHeight=content==Settings?settingsHeight:content==Screenshots?galleryHeight:content==Friends?friendsHeight:lobbyHeight;
  const ImVec2 size(bodyWidth,std::min(preferredHeight*scale,availableHeight));
  const float slide=workspaceMotion.offset(workspaceRequest);
  const ImVec2 position(margin-(1-alpha)*33*scale+slide*19*scale,margin);
  if(beginFloatingCard("##floating-workspace",position,size,panelAlpha,acrylic,content==workspaceRequest&&open)){
   ImGui::PushID(content);
   const bool profileTitle=content==Friends&&friendPageProgress>.5f&&profile;
   const char* title=content==Friends?OverlayText(profileTitle?"Профиль":"Друзья"):content==Lobby?OverlayText(lobbyInvites?"Пригласить в лобби":"Лобби"):
    content==Screenshots?OverlayText("Скриншоты"):OverlayText("Настройки");
   char meta[48]{};
   if(content==Friends&&!profileTitle){
    const auto online=std::count_if(state.friends.begin(),state.friends.end(),[](const auto& entry){return entry.status!="pending"&&!entry.presence.empty();});
    std::snprintf(meta,sizeof(meta),OverlayText("В сети: %d"),static_cast<int>(online));
   }
   ImGui::SetCursorPosX(ImGui::GetCursorPosX()+4*scale);
   floatingTitle(title,profileTitle?CardBack::Friend:CardBack::None,meta,8.f);
   ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(scrollInset*scale,5*scale));
   ImGui::PushStyleVar(ImGuiStyleVar_ItemSpacing,ImVec2(8*scale,6*scale));
   const bool nestedScroll=content==Friends||content==Screenshots;
   if(!nestedScroll)ImGui::SetCursorPosX(ImGui::GetCursorPosX()-scrollInset*scale);
   const auto childFlags=ImGuiWindowFlags_NoBackground|(content==workspaceRequest&&open?0:ImGuiWindowFlags_NoInputs)|
    (nestedScroll?ImGuiWindowFlags_NoScrollbar|ImGuiWindowFlags_NoScrollWithMouse:0);
   if(ImGui::BeginChild("##workspace-content",ImVec2(-4*scale,0),nestedScroll?ImGuiChildFlags_None:ImGuiChildFlags_AlwaysUseWindowPadding,childFlags)){
    if(content==Friends)drawFriendsPages(state,profile);
    else if(content==Screenshots)drawScreenshotGallery(scale,recent);
    else {
     if(content==Settings)drawSessionProfile(state,scale);
     if(content==Lobby&&SintetSocial::MixedProfileLobby(state))drawMixedProfileWarning(scale);
     drawSocialContent(state,ImGui::GetContentRegionAvail().y,content);
     if(content==workspaceRequest){
      const float header=ImGui::GetWindowPos().y-position.y;
      const float measured=std::ceil((header+ImGui::GetCursorPosY())/scale)+25.f;
      if(content==Settings)measuredSettingsHeight=measured;
      if(content==Lobby&&state.enabled&&!state.offline)measuredLobbyHeight=measured;
     }
    }
   }
   ImGui::EndChild();ImGui::PopStyleVar(2);
   ImGui::PopID();
  }
  endFloatingCard();
 }
 return dockRight;
}

void drawPanel(const SintetSocial::State& state) {
 const auto& io=ImGui::GetIO();
 const float seconds=std::min(io.DeltaTime,.05f);
 const bool ghost=SintetWidgets::GhostMode();
 const bool open=visible&&!ghost;
 const bool pinned=SintetWidgets::MiniPlayerPinned()&&!ghost;
 const float step=seconds/(open?.23f:.17f);
 panelAnimation=std::clamp(panelAnimation+(open?step:-step),0.f,1.f);
 const float alpha=easeOut(panelAnimation),scale=interfaceScale;
 const float margin=23*scale,dockHeight=54*scale;
 const float dockRight=alpha>0?drawWorkspace(state,alpha,seconds,open):0.f;
 if(alpha<=0&&!pinned&&playerMotion.shown==NoSection)return;

 const auto track=mediaTrack();
 if(!SintetOverlayStyle::preview)rememberMedia(track);
 constexpr int miniPlayer=0,fullPlayer=1;
 const bool showMini=!track.title.empty()&&(pinned||(open&&showMiniPlayer));
 const int playerRequest=open&&section==Music?fullPlayer:showMini?miniPlayer:NoSection;
 playerMotion.update(playerRequest,1.f,seconds);
 const bool expanded=playerMotion.shown==fullPlayer;
 const auto& material=SintetOverlayStyle::style;
 const float mediaAlpha=pinned&&!expanded?1.f:alpha;
 const float mediaWidth=std::min((expanded?350.f:286.f)*scale,io.DisplaySize.x-2*margin);
 const float mediaHeight=(expanded?(track.title.empty()?120.f:299.f+30.f*(SintetOverlayStyle::preview?3:recentMedia.size())):87.f)*scale;
 const bool fitsBeside=io.DisplaySize.x-margin-mediaWidth>=dockRight+9*scale;
 const float bottomInset=fitsBeside?0.f:(dockHeight+9*scale)*alpha;
 const float mediaY=io.DisplaySize.y-margin-bottomInset-(material.bottom?65.f:0.f)*scale-mediaHeight+
  (1-mediaAlpha)*13*scale+playerMotion.offset(playerRequest)*13*scale;
 if(playerMotion.shown!=NoSection&&mediaAlpha*playerMotion.opacity()>.003f&&mediaWidth>=180*scale&&mediaY>margin){
  const ImVec2 mediaPosition(io.DisplaySize.x-margin-mediaWidth,mediaY);
  const bool interactive=playerMotion.shown==playerRequest&&open;
  if(beginFloatingCard("##floating-media",mediaPosition,ImVec2(mediaWidth,mediaHeight),mediaAlpha*playerMotion.opacity(),mediaAcrylic,interactive)){
   if(expanded)floatingTitle(OverlayText("Плеер"));
   drawMedia(scale,track,expanded,interactive);
  }
  endFloatingCard();
 }
 if(alpha>0&&io.DisplaySize.x>920*scale){
  const std::string shortcut=overlayHotkey+"  ·  "+OverlayText("Вернуться в игру");
  auto* draw=ImGui::GetForegroundDrawList();
  const float shortcutSize=16*scale;
  const auto text=ImGui::GetFont()->CalcTextSizeA(shortcutSize,FLT_MAX,0,shortcut.c_str());
  const ImVec2 pos(margin,io.DisplaySize.y-margin-24*scale+(1-alpha)*10*scale);
  draw->AddRectFilled(pos,ImVec2(pos.x+text.x+14*scale,pos.y+24*scale),IM_COL32(24,24,24,static_cast<int>(220*alpha)),6*scale);
  draw->AddText(ImGui::GetFont(),shortcutSize,ImVec2(pos.x+7*scale,pos.y+5*scale),IM_COL32(235,235,235,static_cast<int>(235*alpha)),shortcut.c_str());
 }
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

void applyStyle(){
  auto& style=ImGui::GetStyle();
  style=ImGuiStyle();
  ImGui::StyleColorsDark(&style);
  const auto& theme=SintetOverlayStyle::style;
  auto surface=[&](float mix){return SintetOverlayStyle::Blend(theme.surface,theme.text,mix);};
  style.WindowRounding=theme.rounding;style.ChildRounding=theme.rounding*.78f;style.FrameRounding=theme.rounding*.72f;
  style.WindowBorderSize=0;style.ChildBorderSize=0;style.PopupBorderSize=0;
  style.WindowPadding=ImVec2(22,22);style.FramePadding=ImVec2(14,9);style.ItemSpacing=ImVec2(10,10);
  style.ScrollbarSize=5.f;style.ScrollbarRounding=5.f;
  style.AntiAliasedLines=true;style.AntiAliasedFill=true;
  style.Colors[ImGuiCol_WindowBg]=theme.surface;
  style.Colors[ImGuiCol_ChildBg]=ControlFill(surface(0.04f));
  style.Colors[ImGuiCol_PopupBg]=ControlFill(theme.surface,.92f);
  style.Colors[ImGuiCol_Button]=ControlFill(surface(0.1f));
  style.Colors[ImGuiCol_ButtonHovered]=ControlFill(surface(0.17f),.82f);
  style.Colors[ImGuiCol_ButtonActive]=ControlFill(surface(0.23f),.86f);
  style.Colors[ImGuiCol_FrameBg]=ControlFill(surface(0.08f));
  style.Colors[ImGuiCol_FrameBgHovered]=ControlFill(surface(0.14f),.82f);
  style.Colors[ImGuiCol_FrameBgActive]=ControlFill(surface(0.2f),.86f);
  style.Colors[ImGuiCol_Header]=ControlFill(surface(0.11f));
  style.Colors[ImGuiCol_HeaderHovered]=ControlFill(surface(0.18f),.82f);
  style.Colors[ImGuiCol_HeaderActive]=ControlFill(surface(0.22f),.86f);
  style.Colors[ImGuiCol_TextSelectedBg]=SintetOverlayStyle::Accent(.45f);
  style.Colors[ImGuiCol_NavCursor]=theme.accent;
  style.Colors[ImGuiCol_Text]=theme.text;
  style.Colors[ImGuiCol_TextDisabled]=surface(.83f);
  style.Colors[ImGuiCol_CheckMark]=theme.accent;
  style.Colors[ImGuiCol_SliderGrab]=theme.accent;
  style.Colors[ImGuiCol_ScrollbarBg]=ImVec4(0,0,0,0);
  auto scrollThumb=theme.text;scrollThumb.w=.32f;
  style.Colors[ImGuiCol_ScrollbarGrab]=scrollThumb;
  scrollThumb.w=.50f;style.Colors[ImGuiCol_ScrollbarGrabHovered]=scrollThumb;
  scrollThumb.w=.68f;style.Colors[ImGuiCol_ScrollbarGrabActive]=scrollThumb;
  style.ScaleAllSizes(interfaceScale);
}

float overlayScale(ImVec2 viewport){
 const float fit=std::clamp(std::min(viewport.x/1440.f,viewport.y/810.f),.64f,1.f);
 return fit*SintetOverlayStyle::style.scale;
}

bool initializeRenderer(const wchar_t* fontPath,bool preview){
 context=ImGui::CreateContext();
  ImGuiIO& io=ImGui::GetIO();io.IniFilename=nullptr;io.LogFilename=nullptr;
  RECT client{};
  GetClientRect(window,&client);
  interfaceScale=overlayScale(ImVec2(static_cast<float>(client.right),static_cast<float>(client.bottom)));
  fontRasterScale=std::max(interfaceScale,1.2f);
  io.FontGlobalScale=interfaceScale/fontRasterScale;
  WCHAR fonts[MAX_PATH];
  GetWindowsDirectoryW(fonts,MAX_PATH);
  WCHAR semibold[MAX_PATH],bold[MAX_PATH],selected[32768];
  swprintf_s(semibold,L"%s\\Fonts\\seguisb.ttf",fonts);
  swprintf_s(bold,L"%s\\Fonts\\segoeuib.ttf",fonts);
  if(!fontPath)fontPath=selected;
  if(fontPath==selected){
   const DWORD length=GetEnvironmentVariableW(L"SINTET_OVERLAY_FONT",selected,32768);
   if(!length||length>=32768)selected[0]=0;
  }
  const bool hasSelected=*fontPath!=0;
  auto loadFont=[&](const wchar_t* path,float size,ImFontConfig config,const ImWchar* ranges){
   const int count=WideCharToMultiByte(CP_UTF8,0,path,-1,nullptr,0,nullptr,nullptr);
   if(count<=0)return static_cast<ImFont*>(nullptr);
   std::vector<char> utf8(count);
   WideCharToMultiByte(CP_UTF8,0,path,-1,utf8.data(),count,nullptr,nullptr);
   if(GetFileAttributesW(path)==INVALID_FILE_ATTRIBUTES)return static_cast<ImFont*>(nullptr);
   return io.Fonts->AddFontFromFileTTF(utf8.data(),size*fontRasterScale,&config,ranges);
  };
  auto overlayFont=[&](const wchar_t* fallback,float size){
   ImFontConfig config;
   config.OversampleH=2;
   config.OversampleV=2;
   const ImWchar* ranges=io.Fonts->GetGlyphRangesCyrillic();
   if(hasSelected){
    ImFont* chosen=loadFont(fontPath,size,config,ranges);
    if(chosen){
     config.MergeMode=true;
     loadFont(fallback,size,config,ranges);
     return chosen;
    }
   }
   return loadFont(fallback,size,config,ranges);
  };
  ImFont* body=overlayFont(semibold,20);
  if(!body)body=io.Fonts->AddFontDefault();
  io.FontDefault=body;
  headingFont=overlayFont(bold,22);
  titleFont=overlayFont(bold,29);
  if(!headingFont)headingFont=body;
  if(!titleFont)titleFont=body;
  applyStyle();
  if(!ImGui_ImplWin32_InitForOpenGL(window))return false;
  if(!ImGui_ImplOpenGL3_Init(preview?"#version 110":"#version 150")){
   ImGui_ImplWin32_Shutdown();
   return false;
  }
  return true;
}

void drawOverlayFrame(){
 ImGui::SetCurrentContext(context);
 ImGui::GetIO().ConfigFlags |= ImGuiConfigFlags_NoMouseCursorChange;
 ImGui_ImplOpenGL3_NewFrame();
 if(SintetOverlayStyle::preview&&previewEmbedded){
  auto& io=ImGui::GetIO();
  io.DisplaySize=ImVec2(previewWidth,previewHeight);
  io.DeltaTime=1.f/18;
  std::deque<PreviewEvent> events;
  {std::lock_guard<std::mutex> lock(previewInputMutex);events.swap(previewEvents);}
  for(const auto& event:events){
   if(event.kind==0)io.AddMousePosEvent(static_cast<float>(event.x),static_cast<float>(event.y));
   if(event.kind==1||event.kind==2){
    io.AddMousePosEvent(static_cast<float>(event.x),static_cast<float>(event.y));
    io.AddMouseButtonEvent(0,event.kind==1);
   }
   if(event.kind==3)io.AddMouseWheelEvent(0,static_cast<float>(event.value)/120.f);
   if(event.kind==4){io.AddMousePosEvent(-FLT_MAX,-FLT_MAX);io.AddMouseButtonEvent(0,false);}
   if(event.kind==5)io.AddInputCharacter(static_cast<unsigned int>(event.value));
   if(event.kind==6||event.kind==7){
    const ImGuiKey keys[]={ImGuiKey_Backspace,ImGuiKey_Delete,ImGuiKey_LeftArrow,ImGuiKey_RightArrow,ImGuiKey_Home,ImGuiKey_End};
    if(event.value>=0&&event.value<6)io.AddKeyEvent(keys[event.value],event.kind==6);
   }
  }
 }else ImGui_ImplWin32_NewFrame();
 const float renderScale=overlayScale(ImGui::GetIO().DisplaySize);
 if(SintetOverlayStyle::preview||std::abs(renderScale-interfaceScale)>.0001f){
  interfaceScale=renderScale;
  ImGui::GetIO().FontGlobalScale=interfaceScale/fontRasterScale;
  applyStyle();
 }
 const float wheelX=(float)pendingWheelX.exchange(0)/WHEEL_DELTA,wheelY=(float)pendingWheelY.exchange(0)/WHEEL_DELTA;
 if(visible&&(wheelX!=0||wheelY!=0))ImGui::GetIO().AddMouseWheelEvent(wheelX,wheelY);
 ImGui::NewFrame();
 const auto state=SintetOverlayStyle::preview?previewSocial:SintetSocial::Snapshot();
 static bool dispatched=false;
 const bool nowDispatched=state.connection=="Minecraft подключается к миру";
 if(nowDispatched&&!dispatched&&visible)closePanel();
 dispatched=nowDispatched;
 if(state.badgeCatalog!=badgeCatalog){
  for(const auto& item:badgeTextures)if(item.second)glDeleteTextures(1,&item.second);
  badgeTextures.clear();
  badgeCatalog=state.badgeCatalog;
 }
 if(openLobbyOnStart&&state.localPort>0&&!state.room.empty()){
  openLobbyOnStart=false;
  selectSection(Lobby);
  if(!visible)openPanel();
 }
 const bool showPassive=!SintetWidgets::GhostMode();
 if(showPassive&&!SintetOverlayStyle::preview)drawWelcome();
 drawPanel(state);
 const bool networkHUD=!visible&&showPassive&&SintetWidgets::NetworkPinned()&&drawNetworkHUD(state);
 const auto gameSeconds=(GetTickCount64()-sessionStarted)/1000;
 SintetWidgets::Draw(visible,section==Mixer,interfaceScale,overlaySceneDim(),headingFont,state,gpuRenderer.c_str(),gameSeconds);
 if(visible||showPassive){
  if(!drawTURNNotice(state))drawInvitation(state,networkHUD);
 }
 // ImGui has only recorded vertices so far. The back buffer still contains just Minecraft.
 if(!SintetOverlayStyle::preview){SintetScreenshot::Capture(window);drawOverlayNotice();}
 ImGui::Render();
 ImGui_ImplOpenGL3_RenderDrawData(ImGui::GetDrawData());
}

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
  if(escape&&!previousEscape&&visible){
   if(!selectedScreenshot.empty())selectedScreenshot.clear();
   else if(friendProfileOpen){friendProfileOpen=false;confirmFriendRemoval=false;}
   else closePanel();
  }
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
  window=currentWindow;game=current;SintetOverlayStyle::Load();
  if(!initializeRenderer(nullptr,false)){stopping=true;return;}
  originalProc=(WNDPROC)SetWindowLongPtrW(window,GWLP_WNDPROC,(LONG_PTR)input);
  oldCursor=game&&getMode?getMode(game,CursorMode):CursorNormal;
  loadGameProfile();
  SintetWidgets::LoadPreferences();
  openLobbyOnStart=environmentText(L"SINTET_OVERLAY_OPEN_LOBBY")=="true";
  welcomeStarted=GetTickCount64();
  fputs("Sintet Overlay: isolated OpenGL renderer initialized\n",stderr);
 }
 if(visible){if(usingGLFWCursor)inputMode(game,CursorMode,CursorNormal);else{ClipCursor(nullptr);SetCursor(LoadCursorW(nullptr,IDC_ARROW));}}
 drawOverlayFrame();
}

struct PreviewConfig {
 SintetOverlayStyle::Appearance appearance;
 std::wstring font;
 int language=0;
};
std::mutex previewControl,previewConfigLock;
PreviewConfig previewConfig;
std::atomic<HWND> previewWindow{nullptr};
HANDLE previewThread=nullptr,previewReady=nullptr;
constexpr UINT previewResize=WM_APP+40;
bool previewFullscreen=false;
bool previewInlineActive=false;

void setPreviewFullscreen(HWND hwnd,bool fullscreen){
 if(previewFullscreen==fullscreen&&(!previewInlineActive||previewEmbedded))return;
 previewFrameAvailable=false;
 {std::lock_guard<std::mutex> lock(previewInputMutex);previewEvents.clear();}
 if(fullscreen){
  POINT pointer{};GetCursorPos(&pointer);
  MONITORINFO monitor{sizeof(monitor)};
  if(!GetMonitorInfoW(MonitorFromPoint(pointer,MONITOR_DEFAULTTONEAREST),&monitor))return;
  const auto& r=monitor.rcMonitor;
  SetWindowPos(hwnd,HWND_TOP,r.left,r.top,r.right-r.left,r.bottom-r.top,SWP_SHOWWINDOW);
  SetForegroundWindow(hwnd);
 }else{
  ShowWindow(hwnd,SW_HIDE);
  SetWindowPos(hwnd,nullptr,0,0,previewWidth,previewHeight,SWP_NOZORDER|SWP_NOACTIVATE);
 }
 previewFullscreen=fullscreen;
 previewEmbedded=!fullscreen;
 auto& io=ImGui::GetIO();
 io.AddMouseButtonEvent(0,false);io.AddMousePosEvent(-FLT_MAX,-FLT_MAX);
 io.AddFocusEvent(true);
}

LRESULT CALLBACK previewInput(HWND hwnd,UINT message,WPARAM wp,LPARAM lp){
 if(message==WM_CLOSE){PostQuitMessage(0);return 0;}
 if(message==WM_KEYDOWN&&wp==VK_ESCAPE){
  if(!(lp&(1L<<30))){
   if(!selectedScreenshot.empty())selectedScreenshot.clear();
   else if(friendProfileOpen){friendProfileOpen=false;confirmFriendRemoval=false;}
   else setPreviewFullscreen(hwnd,false);
  }
  return 0;
 }
 if(message==WM_KEYDOWN&&wp==VK_F11){
  setPreviewFullscreen(hwnd,!previewFullscreen);
  return 0;
 }
 if(message==previewResize){
  if(wp==3){
   previewInlineActive=false;previewFrameAvailable=false;
  }else if(wp==1){
   previewInlineActive=true;
   if(!previewFullscreen)visible=true;
  }else if(wp==2){
   visible=true;setPreviewFullscreen(hwnd,true);
  }
  return 0;
 }
 if(message==WM_DESTROY){previewWindow=nullptr;PostQuitMessage(0);return 0;}
 if(context&&!previewEmbedded&&ImGui_ImplWin32_WndProcHandler(hwnd,message,wp,lp))return 1;
 return DefWindowProcW(hwnd,message,wp,lp);
}

void previewBackdrop(int width,int height){
 glViewport(0,0,width,height);
 glClearColor(.15f,.21f,.26f,1);
 glClear(GL_COLOR_BUFFER_BIT);
 glBegin(GL_QUADS);
 glColor3f(.11f,.19f,.28f);glVertex2f(-1,1);glVertex2f(1,1);
 glColor3f(.49f,.30f,.23f);glVertex2f(1,-.15f);glVertex2f(-1,-.15f);
 glColor3f(.20f,.30f,.23f);glVertex2f(-1,-.15f);glVertex2f(1,-.15f);
 glColor3f(.08f,.14f,.12f);glVertex2f(1,-1);glVertex2f(-1,-1);
 glEnd();
 glBegin(GL_LINES);
 glColor3f(.24f,.33f,.28f);
 for(float x=-3;x<=3;x+=.17f){glVertex2f(x*.24f,-.15f);glVertex2f(x,-1);}
 for(float y=-.20f;y>-1;y=y*1.23f){glVertex2f(-1,y);glVertex2f(1,y);}
 glEnd();
}

DWORD WINAPI previewRenderer(void* mode){
 PreviewConfig config;
 {std::lock_guard<std::mutex> lock(previewConfigLock);config=previewConfig;}
 const HINSTANCE module=GetModuleHandleW(nullptr);
 const wchar_t* className=L"Sintet.Overlay.LivePreview";
 WNDCLASSW cls{};
 cls.style=CS_OWNDC;cls.lpfnWndProc=previewInput;cls.hInstance=module;
 cls.hCursor=LoadCursorW(nullptr,IDC_ARROW);cls.lpszClassName=className;
 if(!RegisterClassW(&cls)){SetEvent(previewReady);return 1;}
 const HWND host=CreateWindowW(className,L"Sintet Overlay",
  WS_POPUP,0,0,previewWidth,previewHeight,nullptr,nullptr,module,nullptr);
 HDC dc=host?GetDC(host):nullptr;
 PIXELFORMATDESCRIPTOR format{};
 format.nSize=sizeof(format);format.nVersion=1;
 format.dwFlags=PFD_DRAW_TO_WINDOW|PFD_SUPPORT_OPENGL|PFD_DOUBLEBUFFER;format.iPixelType=PFD_TYPE_RGBA;format.cColorBits=24;format.cAlphaBits=8;
 const int pixel=dc?ChoosePixelFormat(dc,&format):0;
 HGLRC gl=pixel&&SetPixelFormat(dc,pixel,&format)?wglCreateContext(dc):nullptr;
 bool initialized=false;
 const auto applyPreviewLanguage=[&]{
   OverlayPreviewLanguage=config.language;
   wchar_t title[160]{};
   if(MultiByteToWideChar(CP_UTF8,0,OverlayText("Sintet Overlay – предпросмотр (F11)"),-1,title,160))
    SetWindowTextW(host,title);
 };
 if(gl&&wglMakeCurrent(dc,gl)){
  window=host;SintetOverlayStyle::style=config.appearance;SintetOverlayStyle::preview=true;
  applyPreviewLanguage();
  initialized=initializeRenderer(config.font.c_str(),true);
 }
 if(initialized){
  visible=true;panelAnimation=0;workspaceMotion={};playerMotion={};selectSection(Friends);previewPlaying=true;previewPosition=72;showMiniPlayer=true;sessionStarted=GetTickCount64();
  previewSocial={};previewSocial.enabled=true;previewSocial.gameReady=true;previewSocial.preGameLobby=true;
  previewSocial.self={"preview","Player"};
  previewSocial.friends={{{"alex","Alex"},"accepted","world"},{{"steve","Steve"},"accepted","launcher"},{{"sam","Sam"},"accepted"}};
  auto& previewFriend=previewSocial.friends.front();
  previewFriend.person.publicID="412853909164";previewFriend.person.handle="alex";
  previewFriend.version="1.21.4";previewFriend.hasTotals=true;previewFriend.minecraftMs=14'400'000;previewFriend.launcherMs=32'400'000;
  previewFriend.musicTitle=OverlayText("После заката");previewFriend.musicArtist="Sintet";
  buildName.clear();gameVersionLine="Minecraft Java Edition";
  SintetWidgets::ResetPreview();
  const auto* renderer=glGetString(GL_RENDERER);
  gpuRenderer=renderer?reinterpret_cast<const char*>(renderer):"";
  previewFullscreen=false;
  previewEmbedded=true;
  previewInlineActive=reinterpret_cast<uintptr_t>(mode)==1;
  if(reinterpret_cast<uintptr_t>(mode)==2)setPreviewFullscreen(host,true);
  std::vector<unsigned char> pixels(previewWidth*previewHeight*4);
  previewWindow=host;
  SetEvent(previewReady);
  bool running=true;
  while(running){
   MSG msg;
   while(PeekMessageW(&msg,nullptr,0,0,PM_REMOVE)){
    if(msg.message==WM_QUIT){running=false;break;}
    TranslateMessage(&msg);DispatchMessageW(&msg);
   }
   if(!running)break;
   {
    std::lock_guard<std::mutex> lock(previewConfigLock);
    config.appearance=previewConfig.appearance;
    config.language=previewConfig.language;
   }
   if(OverlayPreviewLanguage!=config.language){
    applyPreviewLanguage();
    previewFrameAvailable=false;
   }
   SintetOverlayStyle::style=config.appearance;
   RECT client{};
   if((previewFullscreen||previewInlineActive)&&!IsIconic(host)&&GetClientRect(host,&client)&&client.right>0&&client.bottom>0){
    previewBackdrop(client.right,client.bottom);
    drawOverlayFrame();
    if(previewEmbedded){
     glReadBuffer(GL_BACK);
     glReadPixels(0,0,previewWidth,previewHeight,GL_RGBA,GL_UNSIGNED_BYTE,pixels.data());
     std::lock_guard<std::mutex> lock(previewFrameMutex);
     previewPixels.swap(pixels);
     pixels.resize(previewWidth*previewHeight*4);
     if(++previewFrameID==0)++previewFrameID;
     previewFrameAvailable=true;
    }else SwapBuffers(dc);
   }
   MsgWaitForMultipleObjects(0,nullptr,FALSE,previewFullscreen?16:previewInlineActive?55:250,QS_ALLINPUT);
  }
  previewWindow=nullptr;
  previewFrameAvailable=false;
  acrylic.release();mediaAcrylic.release();dockAcrylic.release();welcomeAcrylic.release();
  SintetWidgets::ReleaseGraphics();
  screenshotTextures.clear();
  if(mediaCoverTexture)glDeleteTextures(1,&mediaCoverTexture);
  mediaCoverTexture=0;mediaCover.reset();
  ImGui_ImplOpenGL3_Shutdown();
  ImGui_ImplWin32_Shutdown();
 }else SetEvent(previewReady);
 if(context){ImGui::DestroyContext(context);context=nullptr;}
 if(gl){
  wglMakeCurrent(nullptr,nullptr);
  wglDeleteContext(gl);
 }
 brandTexture=0;
 badgeTextures.clear();badgeCatalog.reset();
 visible=false;window=nullptr;SintetOverlayStyle::preview=false;OverlayPreviewLanguage=-1;
 if(dc)ReleaseDC(host,dc);
 if(IsWindow(host))DestroyWindow(host);
 UnregisterClassW(className,module);
 return initialized?0:1;
}

BOOL WINAPI nativeSwapFrame(HDC dc) {
 {
  // Multiple windows can swap buffers at the same time. ImGui has one context.
  std::unique_lock<std::recursive_mutex> lock(renderMutex,std::try_to_lock);
  if(lock.owns_lock()&&!stopping){
   try{
    if(!currentGame){
     HMODULE glfw=GetModuleHandleW(L"glfw.dll");if(!glfw)glfw=GetModuleHandleW(L"glfw3.dll");
     if(glfw){currentGame=(CurrentGame)GetProcAddress(glfw,"glfwGetCurrentContext");nativeWindow=(WindowHandle)GetProcAddress(glfw,"glfwGetWin32Window");inputMode=(InputMode)GetProcAddress(glfw,"glfwSetInputMode");getMode=(GetMode)GetProcAddress(glfw,"glfwGetInputMode");getCursorPos=(GlfwGetCursorPos)GetProcAddress(glfw,"glfwGetCursorPos");setCursorPos=(GlfwSetCursorPos)GetProcAddress(glfw,"glfwSetCursorPos");}
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

float SintetBadgeMark::DrawName(ImDrawList* draw,ImFont* font,float fontSize,ImVec2 top,float available,
                               ImU32 color,const std::string& name,float iconSize,SintetSocial::AccountProvider provider,
                               const std::vector<std::string>& badges){
 const float measured=font->CalcTextSizeA(fontSize,FLT_MAX,0,name.c_str()).x;
 const float slot=1.4f*iconSize;
 const bool providerVisible=SintetProviderMark::Label(provider)&&available>=slot;
 const float providerWidth=providerVisible?slot:0;
 std::array<SintetSocial::BadgeCatalog::const_iterator,32> icons;
 size_t count=0;
 if(badgeCatalog)for(const auto& id:badges){
  if(count==icons.size())break;
  const auto it=badgeCatalog->find(id);
  if(it!=badgeCatalog->end())icons[count++]=it;
 }
 const float reservedName=std::min(measured,4*fontSize);
 const size_t slots=static_cast<size_t>(std::max(0.f,available-providerWidth-reservedName)/slot);
 const bool overflow=count>slots&&slots>1;
 const size_t shown=std::min(count,slots)-(overflow?1:0);
 const float nameWidth=std::max(0.f,available-providerWidth-slot*(shown+(overflow?1:0)));
 drawFittedText(draw,font,fontSize,top,nameWidth,color,name);
 const float nameEnd=std::min(measured,nameWidth);
 float offset=nameEnd;
 const float gap=slot-iconSize;
 const float y=top.y+(fontSize-iconSize)*.5f;
 if(providerVisible){
  SintetProviderMark::Draw(draw,ImVec2(top.x+offset+gap,y),iconSize,provider);
  offset+=slot;
 }
 for(size_t i=0;i<shown;++i){
  const auto& badge=*icons[i];
  auto [texture,inserted]=badgeTextures.try_emplace(badge.first,0);
  if(inserted)texture->second=uploadIcon<24>(badge.second.pixels);
  const ImVec2 min(top.x+offset+gap,y),max(min.x+iconSize,y+iconSize);
  if(texture->second)
   draw->AddImage((ImTextureID)(intptr_t)texture->second,min,max,ImVec2(0,0),ImVec2(1,1),ImGui::GetColorU32(IM_COL32_WHITE));
  offset+=slot;
 }
 if(overflow){
  const ImVec2 min(top.x+offset+gap,y);
  const std::string more="+"+std::to_string(count-shown);
  drawFittedText(draw,font,iconSize*.75f,ImVec2(min.x,min.y+iconSize*.125f),iconSize,color,more);
  offset+=slot;
 }
 return offset;
}

extern "C" __declspec(dllexport) unsigned int WINAPI SintetOverlayPreview(const char* appearance,const char* language,const wchar_t* font,unsigned int mode){
 if(!appearance||!language||!font||mode>3)return 0;
 PreviewConfig next;
 if(!SintetOverlayStyle::Parse(appearance,next.appearance))return 0;
 next.font=font;
 const char* codes[]={"en","ru","uk","pt","es"};
 for(int i=0;i<5;++i)if(std::strcmp(language,codes[i])==0)next.language=i;
 std::lock_guard<std::mutex> lock(previewControl);
 {std::lock_guard<std::mutex> configLock(previewConfigLock);previewConfig=std::move(next);}
 if(HWND host=previewWindow.load()){
  if(mode)PostMessageW(host,previewResize,mode,0);
  return 1;
 }
 if(!mode||mode==3)return 1;
 if(previewThread){
  if(WaitForSingleObject(previewThread,1000)!=WAIT_OBJECT_0)return 0;
  CloseHandle(previewThread);CloseHandle(previewReady);previewThread=nullptr;previewReady=nullptr;
 }
 previewReady=CreateEventW(nullptr,TRUE,FALSE,nullptr);
 if(!previewReady)return 0;
 previewThread=CreateThread(nullptr,0,previewRenderer,reinterpret_cast<void*>(static_cast<uintptr_t>(mode)),0,nullptr);
 if(!previewThread){CloseHandle(previewReady);previewReady=nullptr;return 0;}
 return WaitForSingleObject(previewReady,3000)==WAIT_OBJECT_0&&previewWindow.load()!=nullptr;
}

extern "C" __declspec(dllexport) unsigned int WINAPI SintetOverlayPreviewFrame(unsigned char* output,unsigned int size,unsigned int previous){
 if(!output||size!=previewWidth*previewHeight*4||!previewFrameAvailable)return 0;
 std::lock_guard<std::mutex> lock(previewFrameMutex);
 if(!previewFrameAvailable||previewFrameID==previous||previewPixels.size()!=size)return 0;
 std::memcpy(output,previewPixels.data(),size);
 return previewFrameID;
}

extern "C" __declspec(dllexport) void WINAPI SintetOverlayPreviewInput(int kind,int x,int y,int value){
 if(!previewFrameAvailable||kind<0||kind>7)return;
 std::lock_guard<std::mutex> lock(previewInputMutex);
 if(previewEvents.size()>=64)return;
 if(kind==0&&!previewEvents.empty()&&previewEvents.back().kind==0)previewEvents.back()={kind,x,y,value};
 else previewEvents.push_back({kind,x,y,value});
}

extern "C" unsigned int SintetRenderStopPreview(){
 std::lock_guard<std::mutex> lock(previewControl);
 if(!previewThread)return 1;
 if(HWND host=previewWindow.load())PostMessageW(host,WM_CLOSE,0,0);
 else PostThreadMessageW(GetThreadId(previewThread),WM_QUIT,0,0);
 if(WaitForSingleObject(previewThread,5000)!=WAIT_OBJECT_0)return 0;
 CloseHandle(previewThread);CloseHandle(previewReady);previewThread=nullptr;previewReady=nullptr;
 return 1;
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
