#pragma once
#include <windows.h>
#define SINTET_API __declspec(dllexport)
SINTET_API unsigned int WINAPI SintetOverlayABI(void);
SINTET_API unsigned int WINAPI SintetOverlayStart(void);
SINTET_API unsigned int WINAPI SintetOverlayStop(void);
SINTET_API void WINAPI SintetOverlaySetGame(unsigned int pid,const WCHAR *name,const WCHAR *build);
SINTET_API void WINAPI SintetOverlayEnable(unsigned int enabled);
SINTET_API void WINAPI SintetOverlayPreview(void);
