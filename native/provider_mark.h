#pragma once

#include <algorithm>
#include <cfloat>
#include <string>
#include "language.h"
#include "social.h"
#include "vendor/imgui/imgui.h"

namespace SintetProviderMark {
using SintetSocial::AccountProvider;

inline const char* Label(AccountProvider provider){
 switch(provider){
  case AccountProvider::Microsoft:return "Microsoft";
  case AccountProvider::Ely:return "Ely.by";
  case AccountProvider::Local:return OverlayText("Локальный профиль");
  default:return nullptr;
 }
}

inline void Draw(ImDrawList* draw,ImVec2 top,float size,AccountProvider provider,bool tooltip){
 const char* label=Label(provider);
 if(!label)return;
 auto p=[&](float x,float y){return ImVec2(top.x+x*size,top.y+y*size);};
 const auto tint=[](int r,int g,int b){return ImGui::GetColorU32(IM_COL32(r,g,b,255));};
 if(provider==AccountProvider::Microsoft){
  draw->AddRectFilled(p(.05f,.05f),p(.48f,.48f),tint(242,80,34));
  draw->AddRectFilled(p(.52f,.05f),p(.95f,.48f),tint(127,186,0));
  draw->AddRectFilled(p(.05f,.52f),p(.48f,.95f),tint(0,164,239));
  draw->AddRectFilled(p(.52f,.52f),p(.95f,.95f),tint(255,185,0));
 }else if(provider==AccountProvider::Ely){
  draw->AddRectFilled(top,p(1,1),tint(242,242,242),size*.08f);
  // Ely.by mark from frontend/assets/brand/ely-icon-light.svg.
  auto uv=[&](float x,float y){return p(x/1024,(960-y)/1024);};
  const ImU32 ink=tint(26,26,26);
  const ImVec2 e[]={uv(460.8f,243.2f),uv(197.973f,243.2f),uv(197.973f,724.48f),
    uv(453.973f,724.48f),uv(453.973f,673.28f),uv(259.413f,673.28f),
    uv(259.413f,516.267f),uv(430.08f,516.267f),uv(430.08f,465.067f),
    uv(259.413f,465.067f),uv(259.413f,294.4f),uv(460.8f,294.4f)};
  draw->AddConcavePolyFilled(e,12,ink);
  draw->AddRectFilled(uv(532.48f,751.787f),uv(590.507f,239.787f),ink);
  draw->PathLineTo(uv(788.48f,188.587f));
  draw->PathBezierCubicCurveTo(uv(778.24f,157.867f),uv(768,133.974f),uv(750.933f,116.907f),8);
  draw->PathBezierCubicCurveTo(uv(733.866f,99.84f),uv(716.8f,93.014f),uv(696.32f,93.014f),8);
  const ImVec2 y[]={uv(665.6f,99.841f),uv(665.6f,151.041f),uv(679.253f,151.041f),
    uv(720.213f,164.694f),uv(740.693f,212.481f),uv(634.88f,570.881f),uv(703.147f,570.881f),
    uv(768,304.641f),uv(832.853f,567.468f),uv(894.293f,567.468f)};
  for(const auto& point:y)draw->PathLineTo(point);
  draw->PathFillConcave(ink);
 }else{
  const ImU32 ink=tint(186,186,190);
  draw->AddCircle(p(.5f,.32f),size*.18f,ink,16,size*.09f);
  draw->PathLineTo(p(.14f,.87f));
  draw->PathBezierCubicCurveTo(p(.14f,.54f),p(.86f,.54f),p(.86f,.87f));
  draw->PathStroke(ink,0,size*.09f);
 }
 if(tooltip&&ImGui::IsWindowHovered()&&ImGui::IsMouseHoveringRect(top,p(1,1)))
  ImGui::SetTooltip("%s",label);
}

inline float NameWidth(float available,float iconSize,AccountProvider provider){
 return std::max(0.f,available-(Label(provider)?1.4f*iconSize:0.f));
}

inline void DrawAfterName(ImDrawList* draw,ImFont* font,float fontSize,ImVec2 top,float available,
                          const std::string& name,float iconSize,AccountProvider provider,bool tooltip=true){
 if(!Label(provider)||name.empty()||available<1.4f*iconSize)return;
 const float measured=font->CalcTextSizeA(fontSize,FLT_MAX,0,name.c_str()).x;
 const float x=std::min(measured,NameWidth(available,iconSize,provider))+.4f*iconSize;
 Draw(draw,ImVec2(top.x+x,top.y+(fontSize-iconSize)*.5f),iconSize,provider,tooltip);
}
}
