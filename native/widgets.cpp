#include "language.h"
#define WIN32_LEAN_AND_MEAN
#define NOMINMAX
#include <windows.h>
#include <psapi.h>
#include <algorithm>
#include <array>
#include <cmath>
#include <cstdio>
#include <cfloat>
#include <string>
#include "vendor/imgui/imgui.h"
#include "audio.h"
#include "screenshot.h"
#include "social.h"
#include "widgets.h"
#include "overlay_style.h"
#include "acrylic.h"
#include "badge_marks.h"

namespace SintetWidgets {
namespace {
using SintetOverlayStyle::ControlFill;
enum class Icon { Audio, Muted, Performance, Network, Camera, Mic, MicOff, Eye, EyeOff, Pin, Close };

enum class Popup { None, Sound, Network };
Popup popup=Popup::None;
bool performanceOpen=false,pinned=false,ghost=false;
bool networkPinned=false,miniPlayerPinned=false;
std::wstring networkPinFile,miniPlayerPinFile;
float barMotion=0,soundMotion=0,networkMotion=0,performanceMotion=0;
bool soundAtDock=false;
bool performanceMoved=false;
ImVec2 performancePosition;
std::array<SintetAudio::Volume,4> previewVolumes;
constexpr float soundCardHeight=203;
SintetAcrylic::Backdrop barBackdrop,soundBackdrop,networkBackdrop,performanceBackdrop;
float sceneDim=0;

bool loadPin(const wchar_t* key,std::wstring& path){
 const auto size=GetEnvironmentVariableW(key,nullptr,0);
 if(!size){path.clear();return false;}
 path.resize(size);
 const auto length=GetEnvironmentVariableW(key,path.data(),size);
 if(!length||length>=size){path.clear();return false;}
 path.resize(length);
 const auto attributes=GetFileAttributesW(path.c_str());
 return attributes!=INVALID_FILE_ATTRIBUTES&&!(attributes&FILE_ATTRIBUTE_DIRECTORY);
}

void savePin(const std::wstring& path,bool enabled){
 if(path.empty()||SintetOverlayStyle::preview)return;
 if(!enabled){DeleteFileW(path.c_str());return;}
 const auto file=CreateFileW(path.c_str(),GENERIC_WRITE,FILE_SHARE_READ,nullptr,OPEN_ALWAYS,FILE_ATTRIBUTE_NORMAL,nullptr);
 if(file!=INVALID_HANDLE_VALUE)CloseHandle(file);
}

std::array<SintetAudio::Volume,3> volumes(){
 if(SintetOverlayStyle::preview)return {previewVolumes[0],previewVolumes[1],previewVolumes[2]};
 return SintetAudio::Snapshot();
}
void setMuted(SintetAudio::Target target,bool muted){
 if(SintetOverlayStyle::preview)previewVolumes[static_cast<size_t>(target)].muted=muted;
 else if(target==SintetAudio::Target::Voice)SintetSocial::Action("voice-volume-mute",muted?"true":"false");
 else SintetAudio::SetMuted(target,muted);
}
void setVolume(SintetAudio::Target target,float level){
 if(SintetOverlayStyle::preview)previewVolumes[static_cast<size_t>(target)].level=level;
 else if(target==SintetAudio::Target::Voice)SintetSocial::Action("voice-volume",std::to_string(std::lround(level*100)));
 else SintetAudio::SetLevel(target,level);
}

float animate(float& progress,bool show,float delta){
 progress=std::clamp(progress+(show?delta:-delta)/.23f,0.f,1.f);
 return progress*progress*(3-2*progress);
}

bool beginCard(const char* name,ImVec2 position,ImVec2 size,float alpha,float scale,bool interactive,SintetAcrylic::Backdrop& backdrop){
 ImGui::SetNextWindowPos(position);
 ImGui::SetNextWindowSize(size);
 ImGui::PushStyleVar(ImGuiStyleVar_Alpha,alpha);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowBorderSize,0);
 ImGui::PushStyleVar(ImGuiStyleVar_WindowPadding,ImVec2(0,0));
 constexpr ImGuiWindowFlags flags=ImGuiWindowFlags_NoDecoration|ImGuiWindowFlags_NoSavedSettings|ImGuiWindowFlags_NoMove|
     ImGuiWindowFlags_NoFocusOnAppearing|ImGuiWindowFlags_NoBringToFrontOnFocus|ImGuiWindowFlags_NoNav|ImGuiWindowFlags_NoScrollWithMouse|ImGuiWindowFlags_NoBackground;
 const bool content=ImGui::Begin(name,nullptr,flags|(interactive?0:ImGuiWindowFlags_NoInputs));
 if(content)SintetAcrylic::DrawSurface(backdrop,ImGui::GetWindowDrawList(),position,size,scale,alpha,SintetOverlayStyle::style.rounding*.75f*scale,sceneDim);
 return content;
}

void endCard(){ImGui::End();ImGui::PopStyleVar(3);}

void drawIcon(ImDrawList* draw,Icon icon,ImVec2 center,float scale,ImU32 ink){
 auto p=[&](float x,float y){return ImVec2(center.x+x*scale,center.y+y*scale);};
 const float line=1.5f*scale;
 if(icon==Icon::Audio||icon==Icon::Muted){
  const ImVec2 speaker[]={p(-8,-3),p(-5,-3),p(-1,-7),p(-1,7),p(-5,3),p(-8,3)};
  draw->AddPolyline(speaker,6,ink,ImDrawFlags_Closed,line);
  if(icon==Icon::Audio){
   for(float radius:{4.f,8.f}){
    draw->PathArcTo(p(-1,0),radius*scale,-.77f,.77f,8);
    draw->PathStroke(ink,0,line);
   }
  }else{
   draw->AddLine(p(3,-2),p(8,3),ink,line);draw->AddLine(p(3,3),p(8,-2),ink,line);
  }
 }else if(icon==Icon::Performance){
  draw->AddRect(p(-8,-7),p(8,5),ink,2.5f*scale,0,line);
  const ImVec2 wave[]={p(-5,-1),p(-3,-1),p(-1,-4),p(1,2),p(3,-1),p(5,-1)};
  draw->AddPolyline(wave,6,ink,0,line);
  draw->AddLine(p(-3,8),p(3,8),ink,line);
 }else if(icon==Icon::Pin){
  const ImVec2 pin[]={p(-2,-7),p(5,0),p(2,1),p(1,6),p(-6,-1),p(-1,-2),p(-2,-7)};
  draw->AddPolyline(pin,7,ink,0,line);
  draw->AddLine(p(-2,2),p(-7,7),ink,line);
 }else if(icon==Icon::Network){
  for(float radius:{5.f,9.f,13.f}){
   draw->PathArcTo(p(0,6),radius*scale,-2.32f,-.82f,10);
   draw->PathStroke(ink,0,line);
  }
  draw->AddCircleFilled(p(0,6),1.45f*scale,ink,10);
 }else if(icon==Icon::Camera){
  draw->AddRect(p(-9,-5),p(9,7),ink,2.5f*scale,0,line);
  draw->AddCircle(p(0,1),3.2f*scale,ink,16,line);
  const ImVec2 cap[]={p(-5,-5),p(-3,-8),p(3,-8),p(5,-5)};
  draw->AddPolyline(cap,4,ink,0,line);
 }else if(icon==Icon::Mic||icon==Icon::MicOff){
  draw->AddRect(p(-3,-9),p(3,2),ink,3*scale,0,line);
  draw->PathArcTo(p(0,0),6*scale,0,3.14159f,12);
  draw->PathStroke(ink,0,line);
  draw->AddLine(p(0,6),p(0,9),ink,line);
  draw->AddLine(p(-3,9),p(3,9),ink,line);
  if(icon==Icon::MicOff)draw->AddLine(p(-8,8),p(8,-8),ink,2*scale);
 }else if(icon==Icon::Eye||icon==Icon::EyeOff){
  draw->PathLineTo(p(-9,0));
  draw->PathBezierCubicCurveTo(p(-4,-7),p(4,-7),p(9,0));
  draw->PathBezierCubicCurveTo(p(4,7),p(-4,7),p(-9,0));
  draw->PathStroke(ink,0,line);
  draw->AddCircle(p(0,0),2.4f*scale,ink,14,line);
  if(icon==Icon::EyeOff)draw->AddLine(p(-8,8),p(8,-8),ink,2*scale);
 }else{
  draw->AddLine(p(-4,-4),p(4,4),ink,line);draw->AddLine(p(4,-4),p(-4,4),ink,line);
 }
}

bool iconButton(const char* id,Icon icon,ImVec2 top,float size,float scale,bool active=false,bool enabled=true,const ImVec4* tint=nullptr,bool quiet=false){
 auto* draw=ImGui::GetWindowDrawList();
 ImGui::SetCursorScreenPos(top);
 ImGui::BeginDisabled(!enabled);
 const bool clicked=ImGui::InvisibleButton(id,ImVec2(size,size));
 const bool hovered=ImGui::IsItemHovered();
 const auto& theme=SintetOverlayStyle::style;
 const ImVec4 fill=active?SintetOverlayStyle::Blend(theme.surface,theme.accent,.27f):
     SintetOverlayStyle::Blend(theme.surface,theme.text,hovered?.18f:.10f);
 if(!quiet||active||hovered)draw->AddRectFilled(top,ImVec2(top.x+size,top.y+size),ImGui::GetColorU32(ControlFill(fill,hovered||active?.82f:.76f)),7*scale);
 const ImVec4 ink=tint?*tint:active?theme.accent:theme.text;
 drawIcon(draw,icon,ImVec2(top.x+size*.5f,top.y+size*.5f),scale,ImGui::GetColorU32(ink));
 ImGui::EndDisabled();
 return clicked;
}

ImVec4 networkColor(const SintetSocial::LinkQuality& quality){
 if(quality.level=="poor")return {.97f,.44f,.43f,1};
 if(quality.level=="unstable")return {.97f,.77f,.40f,1};
 if(quality.level=="good")return {.59f,.83f,.66f,1};
 return {.62f,.63f,.65f,1};
}

std::string ping(const SintetSocial::LinkQuality& quality){
 return quality.ping<0?OverlayText("– мс"):std::to_string(quality.ping)+" "+OverlayText("мс");
}

struct Clock {
 SYSTEMTIME local{};
 int battery=-1;
 bool powered=false;
 ULONGLONG next=0;
 void read(){
  const auto now=GetTickCount64();
  if(now<next)return;
  next=now+700;
  GetLocalTime(&local);
  SYSTEM_POWER_STATUS power{};
  battery=-1;
  if(GetSystemPowerStatus(&power)&&power.BatteryFlag!=255&&!(power.BatteryFlag&128)&&power.BatteryLifePercent<=100){
   battery=power.BatteryLifePercent;
   powered=power.ACLineStatus==1;
  }
 }
};
Clock clock;

void drawBattery(ImDrawList* draw,ImVec2 top,float scale,float space){
 auto p=[&](float x,float y){return ImVec2(top.x+x*scale,top.y+y*scale);};
 const auto outline=ImGui::GetColorU32(ImVec4(.69f,.70f,.72f,1));
 const auto fill=ImGui::GetColorU32(clock.powered?ImVec4(.59f,.83f,.66f,1):
     clock.battery<=20?ImVec4(.98f,.67f,.39f,1):ImVec4(.77f,.79f,.80f,1));
 draw->AddRect(p(0,0),p(18,10),outline,2*scale,0,scale);
 draw->AddRectFilled(p(19,3),p(21,7),outline,scale);
 if(clock.battery>0)draw->AddRectFilled(p(2,2),p(2+14*clock.battery*.01f,8),fill,scale);
 if(clock.powered){
  const ImVec2 bolt[]={p(10,-4),p(6,4),p(11,4),p(8,13)};
  draw->AddPolyline(bolt,4,ImGui::GetColorU32(ImGuiCol_Text),0,1.5f*scale);
 }
 char percent[8];std::snprintf(percent,sizeof(percent),"%d%%",clock.battery);
 if(space>29)draw->AddText(ImGui::GetFont(),(space<61?14.f:16.f)*scale,p(24,-3),fill,percent);
}

void drawBar(ImVec2 position,float width,float height,float scale,float alpha,bool open,bool compact,bool vertical,
             float linkWidth,float batteryWidth,ImFont* heading,const SintetSocial::State& network,std::uint64_t gameSeconds){
 auto p=[&](float x,float y){return ImVec2(position.x+x*scale,position.y+y*scale);};
 if(beginCard("##widget-bar",position,ImVec2(width,height),alpha,scale,open,barBackdrop)){
  const float button=compact?28.f:32.f,top=(48-button)*.5f;
  const float step=button+4;
  float x=8,y=8;
  auto place=[&](float size){
   const ImVec2 point=vertical?p((width/scale-size)*.5f,y):p(x,top);
   if(vertical)y+=step;
   else x+=size+4;
   return point;
  };
  if(iconButton("##sound",Icon::Audio,place(button),button*scale,.86f*scale,popup==Popup::Sound,true,nullptr,true))
   popup=popup==Popup::Sound?Popup::None:Popup::Sound;
  const auto mic=volumes()[2];
  const ImVec4 red(.98f,.51f,.50f,1);
  if(iconButton("##microphone",mic.muted?Icon::MicOff:Icon::Mic,place(button),button*scale,.86f*scale,
                false,mic.available,mic.muted?&red:nullptr,true))
   setMuted(SintetAudio::Target::Microphone,!mic.muted);
  if(iconButton("##screenshot",Icon::Camera,place(button),button*scale,.86f*scale,
                false,!SintetOverlayStyle::preview,nullptr,true))SintetScreenshot::Request();
  if(iconButton("##ghost",ghost?Icon::EyeOff:Icon::Eye,place(button),button*scale,.86f*scale,ghost,true,nullptr,true))ghost=!ghost;
  if(!vertical){
   ImGui::GetWindowDrawList()->AddLine(p(x+1,12),p(x+1,36),ImGui::GetColorU32(SintetOverlayStyle::Surface(.24f)),scale);
   x+=6;
  }
  const auto quality=networkColor(network.quality);
  auto* draw=ImGui::GetWindowDrawList();
  const auto link=place(linkWidth);
  ImGui::SetCursorScreenPos(link);
  if(ImGui::InvisibleButton("##network",ImVec2(linkWidth*scale,button*scale)))
   popup=popup==Popup::Network?Popup::None:Popup::Network;
  const bool hovered=ImGui::IsItemHovered();
  if(hovered||popup==Popup::Network)draw->AddRectFilled(link,ImVec2(link.x+linkWidth*scale,link.y+button*scale),
      ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.18f),.82f)),7*scale);
  drawIcon(draw,Icon::Network,ImVec2(link.x+button*scale*.5f,link.y+button*scale*.5f),.81f*scale,ImGui::GetColorU32(quality));
  const auto delay=ping(network.quality);
  const float pingSize=16*scale;
  const float pingInset=(compact?25.f:30.f)*scale;
  if(linkWidth>button){
   const ImVec2 text(link.x+pingInset,link.y+(button*scale-pingSize)*.5f);
   const ImVec4 clip(text.x,text.y,link.x+(linkWidth-2)*scale,text.y+pingSize+2*scale);
   draw->AddText(ImGui::GetFont(),pingSize,text,ImGui::GetColorU32(quality),delay.c_str(),nullptr,0,&clip);
  }
 const auto divider=ImGui::GetColorU32(SintetOverlayStyle::Surface(.28f));
  if(vertical){
   draw->AddLine(p(10,y+1),p(width/scale-10,y+1),divider,scale);
   y+=7;
   x=10;
  }
  if(batteryWidth>0&&!vertical)drawBattery(draw,p(x,21),scale,batteryWidth);
  char time[12],duration[24];
  std::snprintf(time,sizeof(time),"%02d:%02d",clock.local.wHour,clock.local.wMinute);
  const auto hours=static_cast<unsigned long long>(gameSeconds/3600);
  const unsigned minutes=static_cast<unsigned>(gameSeconds/60%60),seconds=static_cast<unsigned>(gameSeconds%60);
  if(hours)std::snprintf(duration,sizeof(duration),"%llu:%02u:%02u",hours,minutes,seconds);
  else std::snprintf(duration,sizeof(duration),"%u:%02u",minutes,seconds);
  const float clockSize=(vertical?20.f:18.f)*scale,captionSize=16*scale;
  auto* font=ImGui::GetFont();
  const float clockWidth=heading->CalcTextSizeA(clockSize,FLT_MAX,0,time).x;
  const float right=position.x+width-12*scale;
  const float captionWidth=font->CalcTextSizeA(captionSize,FLT_MAX,0,duration).x;
  const float clockX=vertical?position.x+(width-clockWidth)*.5f:right-clockWidth;
  const float captionX=vertical?position.x+(width-captionWidth)*.5f:right-captionWidth;
  draw->AddText(heading,clockSize,ImVec2(clockX,p(0,vertical?y:8).y),ImGui::GetColorU32(ImGuiCol_Text),time);
  draw->AddText(font,captionSize,ImVec2(captionX,p(0,vertical?y+21:26).y),ImGui::GetColorU32(ImGuiCol_TextDisabled),duration);
  if(vertical&&batteryWidth>0)drawBattery(draw,p(x,y+40),scale,batteryWidth);
 }
 endCard();
}

void labelClipped(ImDrawList* draw,ImFont* font,float size,ImVec2 pos,float width,ImU32 color,const char* label){
 if(width<=0)return;
 const ImVec4 clip(pos.x,pos.y,pos.x+width,pos.y+size+3);
 draw->AddText(font,size,pos,color,label,nullptr,0,&clip);
}

void drawNetwork(ImVec2 position,float width,float scale,float alpha,bool open,ImFont* heading,const SintetSocial::State& network){
 const auto& quality=network.quality;
 auto* font=ImGui::GetIO().FontDefault;
 const float textSize=SintetOverlayStyle::secondaryTextSize*scale;
 const float statusWidth=width-28*scale;
 const char* status=network.offline?OverlayText("Без доступа к интернету"):network.room.empty()?OverlayText("Нет активного подключения"):OverlayText("Ожидаем участников");
 const char* labels[]={network.leader?OverlayText("Макс. пинг"):OverlayText("Пинг до хоста"),OverlayText("Потери пакетов"),OverlayText("Туннель")};
 const float section=statusWidth/3,metricSize=16*scale;
 float metricHeight=metricSize;
 if(quality.available)for(const auto* label:labels)
  metricHeight=std::max(metricHeight,font->CalcTextSizeA(metricSize,FLT_MAX,section-4*scale,label).y);
 const float metricExtra=metricHeight-metricSize;
 const float height=quality.available?(108+44*static_cast<float>(network.networkPeers.size()))*scale+metricExtra:
  std::max(82*scale,58*scale+font->CalcTextSizeA(textSize,FLT_MAX,statusWidth,status).y);
 if(beginCard("##widget-network",position,ImVec2(width,height),alpha,scale,open,networkBackdrop)){
  auto p=[&](float x,float y){return ImVec2(position.x+x*scale,position.y+y*scale);};
  auto* draw=ImGui::GetWindowDrawList();
  const auto ink=ImGui::GetColorU32(ImGuiCol_Text),muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
  draw->AddText(heading,19*scale,p(14,12),ink,OverlayText("Сеть P2P"));
  if(iconButton("##network-pin",Icon::Pin,p(width/scale-38,8),24*scale,.76f*scale,networkPinned)){
   networkPinned=!networkPinned;
   savePin(networkPinFile,networkPinned);
  }
  if(!quality.available){
   draw->AddText(font,textSize,p(14,42),muted,status,nullptr,statusWidth);
  }else{
   char loss[24]="–",speed[36];
   if(quality.loss>=0)std::snprintf(loss,sizeof(loss),"%.0f%%",quality.loss);
   std::snprintf(speed,sizeof(speed),OverlayText("%.1f КБ/с"),quality.kbPerSec);
   for(int i=0;i<3;++i){
    const ImVec2 label(position.x+14*scale+section*i,position.y+43*scale);
    draw->AddText(font,metricSize,label,muted,labels[i],nullptr,section-4*scale);
   }
   const auto delay=ping(quality);
   const char* values[]={delay.c_str(),loss,speed};
   for(int i=0;i<3;++i){
    const ImVec2 value(position.x+14*scale+section*i,position.y+63*scale+metricExtra);
    labelClipped(draw,heading,textSize,value,section-4*scale,i==0?ImGui::GetColorU32(networkColor(quality)):ink,values[i]);
   }
   for(size_t i=0;i<network.networkPeers.size();++i){
    const auto& peer=network.networkPeers[i];
    const float y=98+metricExtra/scale+44*static_cast<float>(i);
    const auto color=ImGui::GetColorU32(networkColor(peer.quality));
    draw->AddRectFilled(p(12,y-3),p(width/scale-12,y+36),ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.04f))),7*scale);
    draw->AddCircleFilled(p(23,y+8),3*scale,color);
    const auto namePos=p(32,y+1);
    const float nameWidth=width-48*scale,iconSize=12*scale;
    SintetBadgeMark::DrawName(draw,heading,16*scale,namePos,nameWidth,ink,peer.name,iconSize,peer.provider,peer.badges);
    char values[80];
    if(peer.quality.loss>=0)std::snprintf(values,sizeof(values),OverlayText("%s  ·  %.0f%%  ·  %.1f КБ/с"),ping(peer.quality).c_str(),peer.quality.loss,peer.quality.kbPerSec);
    else std::snprintf(values,sizeof(values),OverlayText("%s  ·  –  ·  %.1f КБ/с"),ping(peer.quality).c_str(),peer.quality.kbPerSec);
    labelClipped(draw,font,metricSize,p(32,y+17),width-48*scale,muted,values);
   }
  }
 }
 endCard();
}

void volumeRow(SintetAudio::Target target,const SintetAudio::Volume& volume,const char* label,ImVec2 top,float width,float scale,float delta){
 static std::array<float,4> animated{};
 auto& fill=animated[static_cast<size_t>(target)];
 auto p=[&](float x,float y){return ImVec2(top.x+x*scale,top.y+y*scale);};
 auto* draw=ImGui::GetWindowDrawList();
 const auto ink=ImGui::GetColorU32(ImGuiCol_Text);
 const auto mutedInk=ImGui::GetColorU32(ImGuiCol_TextDisabled);
 draw->AddText(ImGui::GetFont(),SintetOverlayStyle::secondaryTextSize*scale,top,ink,label);
 ImGui::PushID(static_cast<int>(target));
 if(iconButton("##mute",volume.muted?Icon::Muted:Icon::Audio,p(width/scale-24,-4),24*scale,.77f*scale,
               volume.muted,volume.available))setMuted(target,!volume.muted);
 const auto slider=p(0,21);
 const float length=width-34*scale;
 ImGui::SetCursorScreenPos(slider);
 ImGui::BeginDisabled(!volume.available);
 ImGui::InvisibleButton("##volume",ImVec2(length,18*scale));
 float level=volume.muted?0.f:volume.level;
 const bool released=ImGui::IsItemDeactivated()&&ImGui::IsMouseReleased(ImGuiMouseButton_Left);
 const bool editing=volume.available&&(ImGui::IsItemActive()||released);
 if(editing){
  level=std::clamp((ImGui::GetIO().MousePos.x-slider.x)/length,0.f,1.f);
  if(target!=SintetAudio::Target::Voice||released){
   setVolume(target,level);
   if(volume.muted&&(target!=SintetAudio::Target::Voice||SintetOverlayStyle::preview))setMuted(target,false);
  }
 }
 char value[28];
 if(volume.available)std::snprintf(value,sizeof(value),"%d%%",static_cast<int>(std::lround((editing?level:volume.level)*100)));
 else std::snprintf(value,sizeof(value),"–");
 const float valueSize=16*scale;
 const float valueWidth=ImGui::GetFont()->CalcTextSizeA(valueSize,80*scale,0,value).x;
 draw->AddText(ImGui::GetFont(),valueSize,ImVec2(top.x+width-34*scale-valueWidth,top.y),mutedInk,value);
 fill+=(level-fill)*(1-std::exp(-delta*22));
 const auto line=p(0,27);
 const ImVec2 end(line.x+length,line.y+5*scale);
 draw->AddRectFilled(line,end,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.18f),.82f)),3*scale);
 if(fill>0)draw->AddRectFilled(line,ImVec2(line.x+length*fill,end.y),ImGui::GetColorU32(SintetOverlayStyle::style.accent),3*scale);
 if(ImGui::IsItemActive()||ImGui::IsItemHovered())
  draw->AddCircleFilled(ImVec2(line.x+length*fill,line.y+2.5f*scale),4*scale,ink);
 ImGui::EndDisabled();
 ImGui::PopID();
}

void drawSound(ImVec2 position,float width,float scale,float alpha,float delta,bool open,ImFont* heading,const SintetSocial::State& social){
 if(beginCard("##widget-sound",position,ImVec2(width,soundCardHeight*scale),alpha,scale,open,soundBackdrop)){
  auto* draw=ImGui::GetWindowDrawList();
  const ImVec2 title(position.x+14*scale,position.y+11*scale);
  draw->AddText(heading,20*scale,title,ImGui::GetColorU32(ImGuiCol_Text),OverlayText("Звук"));
  const auto current=volumes();
  volumeRow(SintetAudio::Target::Windows,current[0],"Windows",ImVec2(title.x,position.y+42*scale),width-28*scale,scale,delta);
  volumeRow(SintetAudio::Target::Minecraft,current[1],"Minecraft",ImVec2(title.x,position.y+96*scale),width-28*scale,scale,delta);
  const auto voice=SintetOverlayStyle::preview?previewVolumes[3]:SintetAudio::Volume{social.voiceMixerAvailable,social.voiceMuted,std::clamp(social.voiceVolume/100.f,0.f,1.f)};
  volumeRow(SintetAudio::Target::Voice,voice,OverlayText("Голосовой чат"),ImVec2(title.x,position.y+150*scale),width-28*scale,scale,delta);
 }
 endCard();
}

struct Performance {
 std::array<float,42> history{};
 std::array<float,360> timings{};
 size_t next=0,points=0;
 size_t timingNext=0,timingCount=0;
 float fps=0,frameMs=0,low=0,elapsed=0;
 int frames=0;
 ULONGLONG readAt=0,lastSample=0,lastCPU=0;
 double cpu=-1;
 SIZE_T memory=0;
 ULONGLONG availableMemory=0;

 void recordFrame(float delta){
  if(delta>2){elapsed=0;frames=0;fps=frameMs=low=0;timingCount=timingNext=0;return;}
  timings[timingNext]=delta;
  timingNext=(timingNext+1)%timings.size();
  timingCount=std::min(timingCount+1,timings.size());
  elapsed+=delta;
  ++frames;
  if(elapsed<.5f)return;
  fps=frames/elapsed;
  frameMs=elapsed*1000/frames;
  if(timingCount>=30){
   auto sorted=timings;
   const size_t slowCount=std::max(size_t(1),(timingCount+99)/100);
   auto slow=sorted.begin()+(timingCount-slowCount);
   auto end=sorted.begin()+timingCount;
   std::nth_element(sorted.begin(),slow,end);
   float time=0;
   for(auto item=slow;item!=end;++item)time+=*item;
   if(time>0)low=slowCount/time;
  }
  history[next]=fps;
  next=(next+1)%history.size();
  points=std::min(points+1,history.size());
  frames=0;elapsed=0;
 }

 void readProcess(){
  const auto now=GetTickCount64();
  if(now<readAt)return;
  readAt=now+1000;
  const auto proc=GetCurrentProcess();
  FILETIME created,exited,kernel,user;
  if(GetProcessTimes(proc,&created,&exited,&kernel,&user)){
   const auto ticks=[](FILETIME value){return (static_cast<ULONGLONG>(value.dwHighDateTime)<<32)|value.dwLowDateTime;};
   const auto total=ticks(kernel)+ticks(user);
   const auto processors=std::max(GetActiveProcessorCount(ALL_PROCESSOR_GROUPS),DWORD(1));
   if(lastSample&&now>lastSample&&now-lastSample<3000&&total>=lastCPU)
    cpu=std::clamp((total-lastCPU)*.01/((now-lastSample)*processors),0.,100.);
   else cpu=-1;
   lastSample=now;lastCPU=total;
  }else cpu=-1;
  PROCESS_MEMORY_COUNTERS counters{};counters.cb=sizeof(counters);
  memory=K32GetProcessMemoryInfo(proc,&counters,sizeof(counters))?counters.WorkingSetSize:0;
  MEMORYSTATUSEX system{};system.dwLength=sizeof(system);
  availableMemory=GlobalMemoryStatusEx(&system)?system.ullAvailPhys:0;
 }
};
Performance metrics;

void drawPerformance(ImVec2 position,ImVec2 size,float scale,float alpha,bool open,ImFont* heading,const char* renderer){
 if(beginCard("##widget-performance",position,size,alpha,scale,open,performanceBackdrop)){
  auto* draw=ImGui::GetWindowDrawList();
  auto p=[&](float x,float y){return ImVec2(position.x+x*scale,position.y+y*scale);};
  const auto ink=ImGui::GetColorU32(ImGuiCol_Text),muted=ImGui::GetColorU32(ImGuiCol_TextDisabled);
  const auto width=size.x/scale;
  labelClipped(draw,heading,17*scale,p(13,12),size.x-76*scale,ink,OverlayText("Производительность"));
  if(open){
   ImGui::SetCursorScreenPos(p(8,6));
   ImGui::InvisibleButton("##move-performance",ImVec2(size.x-74*scale,28*scale));
   if(ImGui::IsItemActive()&&ImGui::IsMouseDragging(ImGuiMouseButton_Left)){
    const auto delta=ImGui::GetIO().MouseDelta;
    performanceMoved=true;
    performancePosition=ImVec2(position.x+delta.x,position.y+delta.y);
   }
   if(iconButton("##pin",Icon::Pin,p(width-60,6),23*scale,.76f*scale,pinned))pinned=!pinned;
   if(iconButton("##close-performance",Icon::Close,p(width-31,6),23*scale,.82f*scale)){performanceOpen=false;pinned=false;}
  }

  char fps[16]="–",low[16]="–",frame[20]="–",cpu[96],memory[96];
  std::snprintf(cpu,sizeof(cpu),"%s",OverlayText("ЦП игры  –"));
  std::snprintf(memory,sizeof(memory),"%s",OverlayText("ОЗУ игры  –"));
  if(metrics.fps>0)std::snprintf(fps,sizeof(fps),"%.0f",metrics.fps);
  if(metrics.low>0)std::snprintf(low,sizeof(low),"%.0f",metrics.low);
  if(metrics.frameMs>0)std::snprintf(frame,sizeof(frame),"%.1f",metrics.frameMs);
  if(metrics.cpu>=0)std::snprintf(cpu,sizeof(cpu),OverlayText("ЦП игры  %.0f%%"),metrics.cpu);
  if(metrics.memory)std::snprintf(memory,sizeof(memory),OverlayText("ОЗУ игры  %.1f ГБ"),metrics.memory/1073741824.0);
  const float column=(width-26)/3;
  draw->AddText(heading,25*scale,p(13,39),ink,fps);
  draw->AddText(heading,25*scale,p(13+column,39),ink,low);
  draw->AddText(heading,25*scale,p(13+2*column,39),ink,frame);
  const float labelSize=16*scale;
  auto* font=ImGui::GetFont();
  draw->AddText(font,labelSize,p(13,66),muted,"FPS");
  draw->AddText(font,labelSize,p(13+column,66),muted,"1% low");
  labelClipped(draw,font,labelSize,p(13+2*column,66),column*scale,muted,OverlayText("мс / кадр"));

  const auto plotStart=p(13,86),plotEnd=p(width-13,125);
  draw->AddRectFilled(plotStart,plotEnd,ImGui::GetColorU32(ControlFill(SintetOverlayStyle::Surface(.04f))),6*scale);
  draw->AddLine(p(17,113),p(width-17,113),ImGui::GetColorU32(ImVec4(.23f,.23f,.25f,1)),scale);
  const float peak=std::max(60.f,*std::max_element(metrics.history.begin(),metrics.history.end()));
  const auto capacity=metrics.history.size();
  const auto start=(metrics.next+capacity-metrics.points)%capacity;
  ImVec2 previous;
  for(size_t i=0;i<metrics.points;++i){
   const float x=17+(width-34)*static_cast<float>(capacity-metrics.points+i)/(capacity-1);
   const float y=120-29*metrics.history[(start+i)%capacity]/peak;
   const auto point=p(x,y);
   if(i)draw->AddLine(previous,point,ImGui::GetColorU32(ImVec4(.63f,.79f,.69f,1)),1.5f*scale);
   previous=point;
  }
  const float columnWidth=(width*.5f-17)*scale;
  labelClipped(draw,font,labelSize,p(13,137),columnWidth,ink,cpu);
  labelClipped(draw,font,labelSize,p(width*.5f+4,137),columnWidth,ink,memory);
  const auto display=ImGui::GetIO().DisplaySize;
  char resolution[36],freeMemory[96];
  std::snprintf(freeMemory,sizeof(freeMemory),"%s",OverlayText("Свободно  –"));
  std::snprintf(resolution,sizeof(resolution),"%d × %d",static_cast<int>(display.x),static_cast<int>(display.y));
  if(metrics.availableMemory)std::snprintf(freeMemory,sizeof(freeMemory),OverlayText("Свободно  %.1f ГБ"),metrics.availableMemory/1073741824.0);
  labelClipped(draw,font,labelSize,p(13,160),columnWidth,muted,resolution);
  labelClipped(draw,font,labelSize,p(width*.5f+4,160),columnWidth,muted,freeMemory);
  draw->AddLine(p(13,185),p(width-13,185),ImGui::GetColorU32(ImVec4(.23f,.23f,.25f,1)),scale);
  draw->AddText(font,labelSize,p(13,194),muted,"GPU");
  labelClipped(draw,heading,labelSize,p(51,194),size.x-64*scale,ink,renderer&&*renderer?renderer:"–");
 }
 endCard();
}
}

void LoadPreferences(){
 networkPinned=loadPin(L"SINTET_OVERLAY_NETWORK_PIN_FILE",networkPinFile);
 miniPlayerPinned=loadPin(L"SINTET_OVERLAY_MINI_PLAYER_PIN_FILE",miniPlayerPinFile);
}

bool GhostMode(){return ghost;}
bool NetworkPinned(){return networkPinned;}
bool MiniPlayerPinned(){return miniPlayerPinned;}
void SetMiniPlayerPinned(bool enabled){miniPlayerPinned=enabled;savePin(miniPlayerPinFile,enabled);}
void TogglePerformance(){performanceOpen=!performanceOpen;if(!performanceOpen)pinned=false;}

void ResetPreview(){
 popup=Popup::None;
 performanceOpen=pinned=ghost=networkPinned=miniPlayerPinned=performanceMoved=false;
 barMotion=soundMotion=networkMotion=performanceMotion=0;
 soundAtDock=false;
 previewVolumes={{{true,false,.65f},{true,false,.45f},{true,false,.80f},{true,false,1.f}}};
}

void ReleaseGraphics(){
 barBackdrop.release();soundBackdrop.release();networkBackdrop.release();performanceBackdrop.release();
}

void Draw(bool open,bool dockMixer,float scale,float backdropDim,ImFont* heading,const SintetSocial::State& network,const char* renderer,
          std::uint64_t gameSeconds){
 sceneDim=backdropDim;
 const auto& io=ImGui::GetIO();
 metrics.recordFrame(io.DeltaTime);
 const float delta=std::min(io.DeltaTime,.05f);
 const auto display=io.DisplaySize;
 const float bar=animate(barMotion,open,delta);
 if(bar>0)clock.read();
 const bool battery=clock.battery>=0;
 const bool compact=display.x<(battery?443.f:382.f)*scale;
 const bool iconsOnly=compact&&display.x<(battery?398.f:344.f)*scale;
 const float linkWidth=compact?(iconsOnly?28.f:59.f):77.f;
 const float batteryWidth=battery?(compact?(iconsOnly?29.f:54.f):61.f):0.f;
 const float margin=12*scale;
 const auto& theme=SintetOverlayStyle::style;
 const bool vertical=theme.vertical&&display.y>=(battery?306.f:286.f)*scale;
 const bool bottom=theme.bottom&&!vertical;
 const float barWidth=(vertical?91.f:(compact?216.f:232.f)+linkWidth+batteryWidth)*scale;
 const float barHeight=(vertical?(battery?253.f:224.f):48.f)*scale;
 const float barX=display.x-barWidth-23*scale+(vertical?(barWidth+margin)*(1-bar):0.f);
 const float barY=vertical?23*scale:bottom?display.y-82*scale-barHeight+(barHeight+margin)*(1-bar):(-52+75*bar)*scale;
 const bool canShowBar=display.x>=barWidth+2*margin;
 if(bar>0&&canShowBar)drawBar(ImVec2(barX,barY),barWidth,barHeight,scale,bar,open,compact||vertical,vertical,vertical?77.f:linkWidth,batteryWidth,heading,network,gameSeconds);

 if(dockMixer&&popup==Popup::Sound)popup=Popup::None;
 const bool sound=open&&!ghost&&(dockMixer||(popup==Popup::Sound&&canShowBar));
 if(sound)soundAtDock=dockMixer;
 const bool showNetwork=open&&!ghost&&!dockMixer&&popup==Popup::Network&&canShowBar;
 if(!SintetOverlayStyle::preview)SintetAudio::Watch(open&&canShowBar,sound);
 const float soundAlpha=animate(soundMotion,sound,delta);
 const float networkAlpha=animate(networkMotion,showNetwork,delta);
 if(soundAlpha>0&&display.x>2*margin){
  const float width=std::min((soundAtDock?theme.width:244.f)*scale,display.x-2*margin);
  const float x=soundAtDock?std::min(23*scale,display.x-width)-13*scale*(1-soundAlpha):
   std::clamp(vertical?barX-width-margin:barX+(barWidth-width)*.5f,margin,display.x-width-margin);
  const float y=soundAtDock?23*scale:vertical?barY:bottom?std::max(margin,barY-(soundCardHeight+7)*scale):barY+(62-9*(1-soundAlpha))*scale;
  drawSound(ImVec2(x,y),width,scale,soundAlpha*bar,delta,sound,heading,network);
 }
 if(networkAlpha>0&&canShowBar){
  const float width=std::min(320*scale,display.x-2*margin);
  const float x=std::clamp(vertical?barX-width-margin:barX+(barWidth-width)*.5f,margin,display.x-width-margin);
  const float y=vertical?barY:bottom?std::max(margin,barY-300*scale):barY+(62-9*(1-networkAlpha))*scale;
  drawNetwork(ImVec2(x,y),width,scale,networkAlpha*bar,showNetwork,heading,network);
 }

 const bool showPerformance=performanceOpen&&(open||pinned)&&!(ghost&&pinned);
 const float performance=animate(performanceMotion,showPerformance,delta);
 const ImVec2 size(296*scale,220*scale);
 if(performance<=0||display.x<size.x+2*margin||display.y<size.y+2*margin)return;
 metrics.readProcess();
 const float maxPerfX=std::max(margin,display.x-size.x-margin);
 const float maxPerfY=std::max(margin,display.y-size.y-margin-(open?163*scale:0)-(bottom?(barHeight+margin)*bar:0));
 const ImVec2 resting=performanceMoved?
     ImVec2(std::clamp(performancePosition.x,margin,maxPerfX),std::clamp(performancePosition.y,margin,maxPerfY)):
     ImVec2(maxPerfX,maxPerfY);
 if(performanceMoved)performancePosition=resting;
 drawPerformance(ImVec2(resting.x,resting.y+8*scale*(1-performance)),size,scale,performance,open&&showPerformance,heading,renderer);
}
}
