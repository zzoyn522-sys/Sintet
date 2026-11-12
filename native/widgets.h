#pragma once

struct ImFont;
namespace SintetSocial {struct State;}
namespace SintetWidgets {
void LoadPreferences();
bool GhostMode();
bool NetworkPinned();
void Draw(bool overlayOpen,float scale,ImFont* headingFont,const SintetSocial::State& network,const char* renderer);
}
