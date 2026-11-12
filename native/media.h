#pragma once
#include <cstdint>
#include <memory>
#include <string>
#include <vector>

namespace SintetMedia {
struct Cover {
 std::uint32_t width, height;
 std::vector<std::uint8_t> rgba;
};

struct Track {
 std::string title, artist;
 std::shared_ptr<const Cover> cover;
 std::uint64_t generation=0, measuredAt=0;
 bool playing=false, canToggle=false, canSeek=false;
 bool canPrevious=false, canNext=false;
 double position=0, start=0, end=0, seekMin=0, seekMax=0, rate=0;
};

void Start();
void Stop();
Track Snapshot();
enum class Action { Previous, Toggle, Next, Seek };
void Control(std::uint64_t generation,Action action,double seconds=0);
}
