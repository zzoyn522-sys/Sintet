@echo off
setlocal
cd /d "%~dp0.."
set "VSWHERE=%ProgramFiles(x86)%\Microsoft Visual Studio\Installer\vswhere.exe"
if not exist "%VSWHERE%" exit /b 1
for /f "usebackq tokens=*" %%i in (`"%VSWHERE%" -latest -products * -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath`) do set "SINTET_VS=%%i"
if not defined SINTET_VS exit /b 1
call "%SINTET_VS%\VC\Auxiliary\Build\vcvars64.bat" >nul
if errorlevel 1 exit /b 1
if not exist native\bin mkdir native\bin
cl /nologo /c /MT /O2 /guard:cf /sdl /utf-8 /EHsc /std:c++17 /DUNICODE /D_UNICODE /I"%WindowsSdkDir%Include\%WindowsSDKVersion%cppwinrt" native\media.cpp /Fo:native\bin\media.obj
if errorlevel 1 exit /b 1
cl /nologo /c /MT /O2 /guard:cf /sdl /utf-8 /DUNICODE /D_UNICODE native\overlay.c native\vendor\minhook\src\buffer.c native\vendor\minhook\src\hook.c native\vendor\minhook\src\trampoline.c native\vendor\minhook\src\hde\hde64.c /Fo:native\bin\
if errorlevel 1 exit /b 1
cl /nologo /c /MT /O2 /guard:cf /sdl /utf-8 /EHsc /std:c++17 /DUNICODE /D_UNICODE /Inative\vendor\imgui native\render.cpp native\social.cpp native\audio.cpp native\widgets.cpp native\screenshot.cpp native\vendor\imgui\imgui.cpp native\vendor\imgui\imgui_draw.cpp native\vendor\imgui\imgui_tables.cpp native\vendor\imgui\imgui_widgets.cpp native\vendor\imgui\backends\imgui_impl_win32.cpp native\vendor\imgui\backends\imgui_impl_opengl3.cpp /Fo:native\bin\
if errorlevel 1 exit /b 1
link /nologo /DLL /OUT:native\bin\sintet-overlay.dll /DYNAMICBASE /NXCOMPAT /GUARD:CF native\bin\overlay.obj native\bin\render.obj native\bin\social.obj native\bin\media.obj native\bin\audio.obj native\bin\widgets.obj native\bin\screenshot.obj native\bin\buffer.obj native\bin\hook.obj native\bin\trampoline.obj native\bin\hde64.obj native\bin\imgui.obj native\bin\imgui_draw.obj native\bin\imgui_tables.obj native\bin\imgui_widgets.obj native\bin\imgui_impl_win32.obj native\bin\imgui_impl_opengl3.obj user32.lib gdi32.lib dwmapi.lib opengl32.lib imm32.lib winhttp.lib windowsapp.lib ole32.lib windowscodecs.lib
if errorlevel 1 exit /b 1
if "%~1"=="test" (
 cl /nologo /MT /O2 /guard:cf /sdl /utf-8 /EHsc /std:c++17 /DUNICODE /D_UNICODE native\social_test.cpp native\bin\social.obj /Fe:native\bin\social-test.exe /Fo:native\bin\social_test.obj /link winhttp.lib user32.lib
 if errorlevel 1 exit /b 1
 cl /nologo /MT /O2 /utf-8 /EHsc /std:c++17 native\scroll_test.cpp native\bin\imgui.obj native\bin\imgui_draw.obj native\bin\imgui_tables.obj native\bin\imgui_widgets.obj /Fe:native\bin\scroll-test.exe /Fo:native\bin\scroll_test.obj /link user32.lib imm32.lib
 if errorlevel 1 exit /b 1
 native\bin\scroll-test.exe
 if errorlevel 1 exit /b 1
 cl /nologo /MT /O2 /utf-8 /EHsc /std:c++17 native\language_test.cpp /Fe:native\bin\language-test.exe /Fo:native\bin\language_test.obj
 if errorlevel 1 exit /b 1
 for %%L in (en ru uk pt es) do (
  set "SINTET_OVERLAY_LANGUAGE=%%L"
  native\bin\language-test.exe %%L
  if errorlevel 1 exit /b 1
 )
 set "SINTET_OVERLAY_LANGUAGE="
 native\bin\language-test.exe en
 if errorlevel 1 exit /b 1
)
exit /b 0
