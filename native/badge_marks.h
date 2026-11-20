#pragma once

#include "social.h"
#include "vendor/imgui/imgui.h"

namespace SintetBadgeMark {
float DrawName(ImDrawList* draw,ImFont* font,float fontSize,ImVec2 top,float available,
               ImU32 color,const std::string& name,float iconSize,SintetSocial::AccountProvider provider,
               const std::vector<std::string>& badges,bool tooltip);
}
