@echo off
setlocal
cd /d "%~dp0.."
call "C:\Program Files\Microsoft Visual Studio\2022\Community\VC\Auxiliary\Build\vcvars64.bat" >nul
cl /nologo /MT /O2 /utf-8 /DUNICODE /D_UNICODE native\test-host.c /Fo:native\bin\test-host.obj /Fe:native\bin\test-host.exe /link /SUBSYSTEM:WINDOWS user32.lib gdi32.lib
