#pragma once
#include <array>

namespace SintetAudio {
enum class Target { Windows, Minecraft, Microphone };

struct Volume {
 bool available=false,muted=false;
 float level=0;
};

void Start();
void Stop();
void Watch(bool open,bool mixer=true);
std::array<Volume,3> Snapshot();
void SetLevel(Target target,float level);
void SetMuted(Target target,bool muted);
}
