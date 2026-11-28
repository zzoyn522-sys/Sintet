#pragma once

#include <cstdint>

struct ImFont;
namespace SintetSocial {struct State;}
namespace SintetWidgets {
void LoadPreferences();
void ResetPreview();
bool GhostMode();
bool NetworkPinned();
bool MiniPlayerPinned();
void SetMiniPlayerPinned(bool enabled);
void TogglePerformance();
void ReleaseGraphics();
void Draw(bool overlayOpen,bool dockMixer,float scale,float sceneDim,ImFont* headingFont,const SintetSocial::State& network,
          const char* renderer,std::uint64_t gameSeconds);
}
