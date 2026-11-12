#define WIN32_LEAN_AND_MEAN
#include <windows.h>
#include <windowsx.h>
#include <dwmapi.h>
#include <stdio.h>
#include "overlay.h"
#include "brand.h"
static HINSTANCE module;
static HANDLE thread_handle,ready_event;
static DWORD thread_id;
static HWND overlay,game_window;
static volatile LONG game_pid,enabled=1;
static BOOL registered,shown,preview_mode,preview_focused;
static DWORD observed_game_pid;
static HWND startup_window;
static ULONGLONG startup_window_since;
static BOOL opened_at_startup;
static int tab;
static SRWLOCK profile_lock=SRWLOCK_INIT;
static WCHAR player[64]=L"Player",version[96]=L"Minecraft Java Edition";
static ATOM toggle_id;
#define TOGGLE_ID toggle_id
#define SHOW_PREVIEW (WM_APP+21)
#define HIDE_OVERLAY (WM_APP+22)
static void fill(HDC dc,int x,int y,int w,int h,int radius,COLORREF color){HBRUSH brush=CreateSolidBrush(color);HGDIOBJ old=SelectObject(dc,brush),pen=SelectObject(dc,GetStockObject(NULL_PEN));RoundRect(dc,x,y,x+w,y+h,radius,radius);SelectObject(dc,pen);SelectObject(dc,old);DeleteObject(brush);}
static void text(HDC dc,int x,int y,int w,int h,int size,int weight,COLORREF color,const WCHAR *value,UINT flags){HFONT font=CreateFontW(-size,0,0,0,weight,FALSE,FALSE,FALSE,DEFAULT_CHARSET,OUT_DEFAULT_PRECIS,CLIP_DEFAULT_PRECIS,CLEARTYPE_QUALITY,DEFAULT_PITCH,L"Segoe UI");HGDIOBJ old=SelectObject(dc,font);RECT r={x,y,x+w,y+h};SetTextColor(dc,color);SetBkMode(dc,TRANSPARENT);DrawTextW(dc,value,-1,&r,flags|DT_NOPREFIX);SelectObject(dc,old);DeleteObject(font);}
static void draw_brand(HDC dc,int x,int y,int width,COLORREF color){
 HBRUSH brush=CreateSolidBrush(color);
 if(!brush)return;
 HGDIOBJ previousBrush=SelectObject(dc,brush),previousPen=SelectObject(dc,GetStockObject(NULL_PEN));
 POINT points[sizeof sintetMarkCube / sizeof *sintetMarkCube];
 for(size_t i=0;i<sizeof sintetMarkContours / sizeof *sintetMarkContours;i++){
  const SintetMarkContour *contour=&sintetMarkContours[i];
  for(int j=0;j<contour->count;j++){
   points[j].x=x+MulDiv(contour->points[j].x+259,width,518);
   points[j].y=y+MulDiv(contour->points[j].y+210,width,518);
  }
  Polygon(dc,points,contour->count);
 }
 SelectObject(dc,previousPen);SelectObject(dc,previousBrush);DeleteObject(brush);
}
static void draw(HDC dc){WCHAR nick[64],build[96];AcquireSRWLockShared(&profile_lock);wcscpy_s(nick,64,player);wcscpy_s(build,96,version);ReleaseSRWLockShared(&profile_lock);
 fill(dc,0,0,820,520,28,RGB(17,17,18));fill(dc,1,1,818,518,26,RGB(22,22,23));
 draw_brand(dc,30,30,31,RGB(240,240,240));text(dc,72,29,140,32,23,600,RGB(235,235,235),L"Sintet",DT_LEFT);
 fill(dc,616,27,116,30,28,RGB(31,42,34));fill(dc,628,39,5,5,5,RGB(119,190,139));text(dc,641,32,83,24,12,500,RGB(157,195,168),preview_mode?L"Предпросмотр":L"В игре",DT_LEFT);
 fill(dc,756,26,32,32,32,RGB(41,41,43));text(dc,756,29,32,26,21,400,RGB(156,156,160),L"×",DT_CENTER);
 fill(dc,28,85,194,370,18,RGB(17,17,18));
 const WCHAR *tabs[3]={L"Друзья",L"Лобби",L"Подключения"};const WCHAR *glyphs[3]={L"○",L"◇",L"⌁"};
 for(int i=0;i<3;i++){int top=103+i*54;if(tab==i)fill(dc,40,top,170,44,16,RGB(43,43,45));text(dc,55,top+10,25,28,19,400,tab==i?RGB(157,193,167):RGB(110,110,113),glyphs[i],DT_LEFT);text(dc,87,top+13,118,26,14,tab==i?600:400,tab==i?RGB(237,237,239):RGB(138,138,142),tabs[i],DT_LEFT);}
 fill(dc,42,358,166,79,14,RGB(29,29,31));text(dc,57,371,138,24,14,600,RGB(214,214,217),nick,DT_LEFT|DT_END_ELLIPSIS|DT_SINGLELINE);text(dc,57,400,138,20,11,400,RGB(110,110,116),L"Профиль Minecraft",DT_LEFT);
 text(dc,251,94,490,40,26,650,RGB(237,237,240),tabs[tab],DT_LEFT);
 if(tab==0){text(dc,253,142,480,25,13,400,RGB(127,127,133),L"Всегда рядом, даже в игре",DT_LEFT);fill(dc,253,188,536,218,20,RGB(28,28,30));fill(dc,486,213,65,65,20,RGB(39,44,41));text(dc,486,225,65,45,31,400,RGB(139,170,148),L"○",DT_CENTER);text(dc,273,296,496,32,19,600,RGB(221,221,225),L"Пока нет друзей",DT_CENTER);text(dc,283,338,476,42,13,400,RGB(124,124,130),L"Список друзей появится после подключения сервиса",DT_CENTER|DT_WORDBREAK);}
 if(tab==1){text(dc,253,142,470,25,13,400,RGB(127,127,133),L"Один мир для вашей компании",DT_LEFT);fill(dc,253,188,536,218,20,RGB(28,28,30));text(dc,277,218,460,40,21,600,RGB(220,220,225),L"Играйте вместе",DT_LEFT);text(dc,277,268,458,53,14,400,RGB(131,131,138),L"Создание лобби и P2P-подключение будут доступны в следующем этапе разработки.",DT_LEFT|DT_WORDBREAK);fill(dc,277,346,220,38,12,RGB(40,40,43));text(dc,277,355,220,22,13,500,RGB(112,112,119),L"Создать лобби · скоро",DT_CENTER);}
 if(tab==2){text(dc,253,142,480,25,13,400,RGB(127,127,133),L"Состояние игровых сервисов",DT_LEFT);fill(dc,253,188,536,84,17,RGB(28,28,30));text(dc,274,207,350,25,15,600,RGB(215,215,220),L"Minecraft",DT_LEFT);text(dc,274,240,470,20,12,400,RGB(127,127,134),build,DT_LEFT|DT_SINGLELINE|DT_END_ELLIPSIS);fill(dc,253,285,536,100,17,RGB(28,28,30));text(dc,274,305,350,24,15,600,RGB(215,215,220),L"Sintet Connect",DT_LEFT);text(dc,274,340,470,25,13,400,RGB(127,127,134),L"Сервис подключений ещё не запущен",DT_LEFT);}
 text(dc,30,479,500,22,12,400,RGB(102,102,110),preview_mode?L"ESC   Закрыть предпросмотр":L"SHIFT + TAB   Вернуться в игру",DT_LEFT);text(dc,635,479,151,22,11,400,RGB(82,82,90),L"SINTET OVERLAY  0.1",DT_RIGHT);
}
static BOOL owns_foreground(HWND *target){HWND window=GetForegroundWindow();DWORD pid=0;GetWindowThreadProcessId(window,&pid);if(target)*target=window;return window&&window!=overlay&&game_pid>0&&pid==(DWORD)game_pid;}
static void hide_overlay(BOOL focus){if(!shown)return;shown=FALSE;ShowWindow(overlay,SW_HIDE);if(focus&&IsWindow(game_window))SetForegroundWindow(game_window);preview_mode=FALSE;}
static void show_overlay(BOOL preview){HWND target=NULL;if(!preview&&!owns_foreground(&target))return;game_window=target;preview_mode=preview;preview_focused=FALSE;RECT bounds;
 if(!target||!GetWindowRect(target,&bounds)){SystemParametersInfoW(SPI_GETWORKAREA,0,&bounds,0);}
 HMONITOR monitor=MonitorFromRect(&bounds,MONITOR_DEFAULTTONEAREST);MONITORINFO info={sizeof(info)};GetMonitorInfoW(monitor,&info);
 int width=820,height=520;UINT dpi=target?GetDpiForWindow(target):GetDpiForWindow(overlay);width=MulDiv(width,(int)dpi,96);height=MulDiv(height,(int)dpi,96);
 int maxw=info.rcWork.right-info.rcWork.left-32,maxh=info.rcWork.bottom-info.rcWork.top-32;if(width>maxw){height=MulDiv(height,maxw,width);width=maxw;}if(height>maxh){width=MulDiv(width,maxh,height);height=maxh;}
 int x=(bounds.left+bounds.right-width)/2,y=(bounds.top+bounds.bottom-height)/2;if(x<info.rcWork.left)x=info.rcWork.left;if(y<info.rcWork.top)y=info.rcWork.top;
 SetWindowPos(overlay,HWND_TOPMOST,x,y,width,height,SWP_SHOWWINDOW);shown=TRUE;InvalidateRect(overlay,NULL,FALSE);SetForegroundWindow(overlay);SetFocus(overlay);
}
static void update_game_overlay(void) {
 DWORD pid=(DWORD)InterlockedCompareExchange(&game_pid,0,0);
 HWND foreground=NULL;
 BOOL game_active=owns_foreground(&foreground);
 BOOL overlay_active=shown&&GetForegroundWindow()==overlay;
 if(pid!=observed_game_pid){
  if(shown&&!preview_mode)hide_overlay(FALSE);
  observed_game_pid=pid;
  startup_window=NULL;
  opened_at_startup=FALSE;
 }
 if(preview_mode&&overlay_active)preview_focused=TRUE;
 if(shown&&(!enabled||(!overlay_active&&!game_active&&(!preview_mode||preview_focused))))hide_overlay(FALSE);
 BOOL allow_hotkey=enabled&&(game_active||(shown&&overlay_active));
 if(allow_hotkey&&toggle_id&&!registered)registered=RegisterHotKey(NULL,TOGGLE_ID,MOD_SHIFT|MOD_NOREPEAT,VK_TAB);
 else if(!allow_hotkey&&registered){UnregisterHotKey(NULL,TOGGLE_ID);registered=FALSE;}
 if(opened_at_startup||!enabled||!game_active||shown){startup_window=NULL;return;}
 RECT client;
 if(!GetClientRect(foreground,&client)||client.right<320||client.bottom<240){startup_window=NULL;return;}
 ULONGLONG now=GetTickCount64();
 if(startup_window!=foreground){startup_window=foreground;startup_window_since=now;return;}
 if(now-startup_window_since<350)return;
 show_overlay(FALSE);
 if(shown)opened_at_startup=TRUE;
}
static LRESULT CALLBACK window_proc(HWND hwnd,UINT msg,WPARAM wp,LPARAM lp){switch(msg){case WM_ERASEBKGND:return 1;
 case WM_PAINT:{PAINTSTRUCT ps;HDC out=BeginPaint(hwnd,&ps);RECT r;GetClientRect(hwnd,&r);HDC dc=CreateCompatibleDC(out);HBITMAP bmp=CreateCompatibleBitmap(out,r.right,r.bottom);HGDIOBJ old=SelectObject(dc,bmp);SetMapMode(dc,MM_ANISOTROPIC);SetWindowExtEx(dc,820,520,NULL);SetViewportExtEx(dc,r.right,r.bottom,NULL);draw(dc);SetMapMode(dc,MM_TEXT);BitBlt(out,0,0,r.right,r.bottom,dc,0,0,SRCCOPY);SelectObject(dc,old);DeleteObject(bmp);DeleteDC(dc);EndPaint(hwnd,&ps);return 0;}
 case WM_KEYDOWN:if(wp==VK_ESCAPE){hide_overlay(TRUE);return 0;}break;
 case WM_LBUTTONUP:{RECT r;GetClientRect(hwnd,&r);int x=MulDiv(GET_X_LPARAM(lp),820,r.right),y=MulDiv(GET_Y_LPARAM(lp),520,r.bottom);if(x>745&&y<70){hide_overlay(TRUE);return 0;}if(x>=40&&x<=210&&y>=103&&y<265){tab=(y-103)/54;InvalidateRect(hwnd,NULL,FALSE);}return 0;}
 case WM_CLOSE:hide_overlay(TRUE);return 0;
 case WM_TIMER:if(wp==1)update_game_overlay();return 0;
 }return DefWindowProcW(hwnd,msg,wp,lp);}
static DWORD WINAPI ui_thread(LPVOID arg){(void)arg;toggle_id=GlobalAddAtomW(L"Sintet.Overlay.ShiftTab.v1");WNDCLASSW cls={0};cls.lpfnWndProc=window_proc;cls.hInstance=module;cls.lpszClassName=L"Sintet.SafeOverlay.v1";cls.hCursor=LoadCursorW(NULL,IDC_ARROW);RegisterClassW(&cls);
 overlay=CreateWindowExW(WS_EX_TOOLWINDOW|WS_EX_TOPMOST,cls.lpszClassName,L"Sintet Overlay",WS_POPUP,0,0,820,520,NULL,NULL,module,NULL);if(overlay){int round=2;DwmSetWindowAttribute(overlay,33,&round,sizeof(round));BOOL dark=TRUE;DwmSetWindowAttribute(overlay,20,&dark,sizeof(dark));MARGINS margins={1,1,1,1};DwmExtendFrameIntoClientArea(overlay,&margins);SetTimer(overlay,1,75,NULL);}SetEvent(ready_event);
 MSG msg;while(overlay&&GetMessageW(&msg,NULL,0,0)>0){if(msg.message==WM_HOTKEY&&msg.wParam==TOGGLE_ID){if(shown&&(GetForegroundWindow()==overlay||owns_foreground(NULL)))hide_overlay(TRUE);else if(enabled&&owns_foreground(NULL)){show_overlay(FALSE);if(shown)opened_at_startup=TRUE;}}else if(msg.message==SHOW_PREVIEW&&enabled)show_overlay(TRUE);else if(msg.message==HIDE_OVERLAY)hide_overlay(FALSE);else{TranslateMessage(&msg);DispatchMessageW(&msg);}}
 if(registered)UnregisterHotKey(NULL,TOGGLE_ID);registered=FALSE;if(overlay)DestroyWindow(overlay);overlay=NULL;shown=FALSE;UnregisterClassW(cls.lpszClassName,module);if(toggle_id)GlobalDeleteAtom(toggle_id);toggle_id=0;return 0;}
unsigned int WINAPI SintetOverlayABI(void){return 1;}
unsigned int WINAPI SintetOverlayStart(void){if(thread_handle)return 1;ready_event=CreateEventW(NULL,TRUE,FALSE,NULL);if(!ready_event)return 0;thread_handle=CreateThread(NULL,0,ui_thread,NULL,0,&thread_id);if(!thread_handle){CloseHandle(ready_event);ready_event=NULL;return 0;}if(WaitForSingleObject(ready_event,5000)!=WAIT_OBJECT_0){PostThreadMessageW(thread_id,WM_QUIT,0,0);return 0;}CloseHandle(ready_event);ready_event=NULL;return overlay!=NULL;}
unsigned int WINAPI SintetOverlayStop(void){if(!thread_handle)return 1;PostThreadMessageW(thread_id,WM_QUIT,0,0);if(WaitForSingleObject(thread_handle,5000)!=WAIT_OBJECT_0)return 0;CloseHandle(thread_handle);thread_handle=NULL;if(ready_event){CloseHandle(ready_event);ready_event=NULL;}return 1;}
void WINAPI SintetOverlaySetGame(unsigned int pid,const WCHAR *name,const WCHAR *build){AcquireSRWLockExclusive(&profile_lock);if(name&&*name)wcsncpy_s(player,64,name,_TRUNCATE);if(build&&*build)wcsncpy_s(version,96,build,_TRUNCATE);ReleaseSRWLockExclusive(&profile_lock);InterlockedExchange(&game_pid,(LONG)pid);if(overlay)InvalidateRect(overlay,NULL,FALSE);}
void WINAPI SintetOverlayEnable(unsigned int value){InterlockedExchange(&enabled,value?1:0);if(!value&&thread_id)PostThreadMessageW(thread_id,HIDE_OVERLAY,0,0);}
void WINAPI SintetOverlayPreview(void){if(thread_id)PostThreadMessageW(thread_id,SHOW_PREVIEW,0,0);}
/* The JVM calls these native agent entry points outside DllMain. */
typedef const struct JNIInvokeInterface_ *JavaVM;
void SintetRenderStart(void);
void SintetRenderStop(void);

__declspec(dllexport) int WINAPI Agent_OnLoad(JavaVM *vm, char *options, void *reserved) {
 (void)vm;
 (void)options;
 (void)reserved;
 HMODULE pinned;
 if(!GetModuleHandleExW(GET_MODULE_HANDLE_EX_FLAG_FROM_ADDRESS|GET_MODULE_HANDLE_EX_FLAG_PIN,
                       (LPCWSTR)&module,&pinned))return 0;

 SintetRenderStart();
 return 0;
}

__declspec(dllexport) void WINAPI Agent_OnUnload(JavaVM *vm) {
 (void)vm;
 SintetRenderStop();
}

BOOL WINAPI DllMain(HINSTANCE instance,DWORD reason,LPVOID reserved){(void)reserved;if(reason==DLL_PROCESS_ATTACH){module=instance;}return TRUE;}
