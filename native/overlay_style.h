#pragma once

#include <algorithm>
#include <sstream>
#include <string>
#include <windows.h>
#include "vendor/imgui/imgui.h"

namespace SintetOverlayStyle {
struct Appearance {
 bool vertical=false;
 bool bottom=false;
 bool acrylic=true;
 float opacity=.75f;
 float blur=8.f;
 float dim=.4f;
 float scale=1.f;
 float width=414.f;
 float rounding=16.f;
 ImVec4 surface{18.f/255,18.f/255,20.f/255,1};
 ImVec4 text{237.f/255,237.f/255,237.f/255,1};
 ImVec4 accent{142.f/255,197.f/255,162.f/255,1};
};
inline Appearance style;
inline bool preview=false;

inline ImVec4 Blend(ImVec4 a,ImVec4 b,float amount){
 return {a.x+(b.x-a.x)*amount,a.y+(b.y-a.y)*amount,a.z+(b.z-a.z)*amount,a.w};
}

inline ImVec4 Surface(float amount){return Blend(style.surface,style.text,amount);}
inline ImVec4 Accent(float amount){return Blend(style.surface,style.accent,amount);}

inline bool Parse(const char* raw,Appearance& result){
 std::stringstream input(raw);
 std::string field[11];
 for(int i=0;i<8;++i)if(!std::getline(input,field[i],','))return false;
 const bool extended=std::getline(input,field[8],',').good();
 if(extended&&(!std::getline(input,field[9],',')||!std::getline(input,field[10],',')))return false;
 if((field[0]!="top"&&field[0]!="right"&&field[0]!="bottom")||
    (field[1]!="acrylic"&&field[1]!="tint"&&field[1]!="solid"))return false;
 auto parseColor=[](const std::string& text,ImVec4& color){
  if(text.size()!=7||text[0]!='#'||text.find_first_not_of("0123456789abcdefABCDEF",1)!=std::string::npos)return false;
  const auto value=std::stoul(text.substr(1),nullptr,16);
  color=ImVec4(((value>>16)&255)/255.f,((value>>8)&255)/255.f,(value&255)/255.f,1);
  return true;
 };
 auto parseNumber=[](const std::string& text,int max,float& value){
  if(text.empty()||text.size()>3||text.find_first_not_of("0123456789")!=std::string::npos)return false;
  const int parsed=std::stoi(text);
  if(parsed>max)return false;
  value=static_cast<float>(parsed);
  return true;
 };
 Appearance next;
 if(!parseColor(field[2],next.surface)||!parseColor(field[3],next.text)||!parseColor(field[4],next.accent)||
    !parseNumber(field[5],100,next.opacity)||!parseNumber(field[6],24,next.blur)||!parseNumber(field[7],100,next.dim))return false;
 if(extended){
  float scale=0,width=0,rounding=0;
  if(!parseNumber(field[8],125,scale)||!parseNumber(field[9],520,width)||!parseNumber(field[10],28,rounding)||
     (scale&&scale<75)||(width&&width<350)||(rounding&&rounding<4))return false;
  if(scale)next.scale=scale/100;
  if(width)next.width=width;
  if(rounding)next.rounding=rounding;
 }
 next.vertical=field[0]=="right";
 next.bottom=field[0]=="bottom";
 next.acrylic=field[1]=="acrylic";
 next.opacity=field[1]=="solid"?1.f:next.opacity/100;
 next.dim/=100;
 result=next;
 return true;
}

inline void Load(){
 char raw[160]{};
 const DWORD length=GetEnvironmentVariableA("SINTET_OVERLAY_APPEARANCE",raw,sizeof(raw));
 if(length&&length<sizeof(raw))Parse(raw,style);
}
}
