#pragma once
#include "vendor/imgui/imgui.h"
#include <cstdio>
#include <string>

namespace SintetLayout {
constexpr ImGuiWindowFlags FixedCard = ImGuiWindowFlags_NoScrollbar | ImGuiWindowFlags_NoScrollWithMouse;
// A child scrolls only when its contents exceed its available height.
constexpr ImGuiWindowFlags ScrollableContent = ImGuiWindowFlags_None;
inline std::string TransferSize(long long bytes){
 char text[64];
 if(bytes<1024)std::snprintf(text,sizeof(text),"%lld Б",bytes);
 else if(bytes<1048576)std::snprintf(text,sizeof(text),"%.1f КБ",bytes/1024.0);
 else std::snprintf(text,sizeof(text),"%.1f МБ",bytes/1048576.0);
 return text;
}
}
