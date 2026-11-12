#pragma once
#include <windows.h>

namespace SintetScreenshot {
void Start();
void Stop();
void Request();
void Capture(HWND window);
const char* Notice();
}
