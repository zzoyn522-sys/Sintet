//go:build windows

package main

import (
	_ "embed"
	"encoding/binary"
	"fmt"
	"os"
	goruntime "runtime"
	"sync/atomic"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

//go:embed installer/resources/Sintet.ico
var trayIconBytes []byte

var trayUser32 = windows.NewLazySystemDLL("user32.dll")
var trayShell32 = windows.NewLazySystemDLL("shell32.dll")

const trayMessage = 0x8000 + 77

type trayWindowClass struct {
	Size, Style                        uint32
	Proc                               uintptr
	ClassExtra, WindowExtra            int32
	Instance, Icon, Cursor, Background uintptr
	MenuName, ClassName                *uint16
	SmallIcon                          uintptr
}
type trayPoint struct{ X, Y int32 }
type trayMSG struct {
	Window         uintptr
	Message        uint32
	WParam, LParam uintptr
	Time           uint32
	Point          trayPoint
	Private        uint32
}
type trayNotification struct {
	Size                uint32
	Window              uintptr
	ID, Flags, Callback uint32
	Icon                uintptr
	Tip                 [128]uint16
	State, StateMask    uint32
	Info                [256]uint16
	Version             uint32
	InfoTitle           [64]uint16
	InfoFlags           uint32
	GUID                windows.GUID
	BalloonIcon         uintptr
}
type launcherTray struct {
	hwnd    atomic.Uintptr
	present atomic.Bool
	done    chan struct{}
}

func (t *launcherTray) available() bool { return t != nil && t.present.Load() && t.hwnd.Load() != 0 }

func (t *launcherTray) notify(title, body string) {
	if !t.available() {
		return
	}
	data := trayNotification{Window: t.hwnd.Load(), ID: 1, Flags: 0x10, InfoFlags: 0x81}
	data.Size = uint32(unsafe.Sizeof(data))
	copy(data.InfoTitle[:len(data.InfoTitle)-1], windows.StringToUTF16(title))
	copy(data.Info[:len(data.Info)-1], windows.StringToUTF16(body))
	trayShell32.NewProc("Shell_NotifyIconW").Call(1, uintptr(unsafe.Pointer(&data)))
}
func (t *launcherTray) close() {
	if hwnd := t.hwnd.Load(); hwnd != 0 {
		trayUser32.NewProc("PostMessageW").Call(hwnd, 0x10, 0, 0)
	}
	select {
	case <-t.done:
	case <-time.After(2 * time.Second):
	}
}
func createTrayIcon() uintptr {
	b := trayIconBytes
	if len(b) < 22 {
		return 0
	}
	count := int(binary.LittleEndian.Uint16(b[4:6]))
	entry := 6
	for i := 0; i < count && 6+(i+1)*16 <= len(b); i++ {
		if b[6+i*16] == 32 {
			entry = 6 + i*16
			break
		}
	}
	if entry+16 > len(b) {
		return 0
	}
	size := int(binary.LittleEndian.Uint32(b[entry+8:]))
	offset := int(binary.LittleEndian.Uint32(b[entry+12:]))
	if size <= 0 || offset < 0 || offset+size > len(b) {
		return 0
	}
	icon, _, _ := trayUser32.NewProc("CreateIconFromResourceEx").Call(uintptr(unsafe.Pointer(&b[offset])), uintptr(size), 1, 0x30000, 32, 32, 0)
	return icon
}
func newLauncherTray(run func(string)) (*launcherTray, error) {
	t := &launcherTray{done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		defer close(t.done)
		icon := createTrayIcon()
		if icon == 0 {
			ready <- fmt.Errorf("не удалось загрузить значок Sintet")
			return
		}
		defer trayUser32.NewProc("DestroyIcon").Call(icon)
		name, _ := windows.UTF16PtrFromString(fmt.Sprintf("Sintet.Tray.%d", os.Getpid()))
		taskbarName, _ := windows.UTF16PtrFromString("TaskbarCreated")
		taskbar, _, _ := trayUser32.NewProc("RegisterWindowMessageW").Call(uintptr(unsafe.Pointer(taskbarName)))
		data := trayNotification{ID: 1, Flags: 1 | 2 | 4, Callback: trayMessage, Icon: icon, Version: 4}
		data.Size = uint32(unsafe.Sizeof(data))
		copy(data.Tip[:], windows.StringToUTF16("Sintet"))
		notify := func(action uintptr) bool {
			r, _, _ := trayShell32.NewProc("Shell_NotifyIconW").Call(action, uintptr(unsafe.Pointer(&data)))
			return r != 0
		}
		var version4 bool
		add := func() bool {
			ok := notify(0)
			version4 = ok && notify(4)
			t.present.Store(ok)
			return ok
		}
		var menu trayMenu
		callback := syscall.NewCallback(func(hwnd uintptr, msg uint32, w, l uintptr) uintptr {
			if taskbar != 0 && msg == uint32(taskbar) {
				if !add() {
					go run("open")
				}
				return 0
			}
			switch msg {
			case 0x2c: // WM_MEASUREITEM
				if menu.tracking && w == 0 && l != 0 && menu.measure((*trayMeasureItem)(unsafe.Pointer(l))) {
					return 1
				}
			case 0x2b: // WM_DRAWITEM
				if menu.tracking && w == 0 && l != 0 && menu.paint((*trayDrawItem)(unsafe.Pointer(l))) {
					return 1
				}
			case 0x121: // WM_ENTERIDLE
				if menu.tracking && w == 2 { // MSGF_MENU
					menu.styleWindow(hwnd, l)
					return 0
				}
			case trayMessage:
				if menu.tracking {
					return 0
				}
				event := uint16(l)
				switch event {
				case 0x400, 0x401, 0x405, 0x203: // Includes NIN_BALLOONUSERCLICK.
					go run("open")
				case 0x202:
					if !version4 {
						go run("open")
					}
				case 0x7b, 0x205:
					// Explorer can send both WM_RBUTTONUP and WM_CONTEXTMENU for one click.
					if version4 && event == 0x205 {
						return 0
					}
					point := trayMessagePoint(w)
					if event != 0x7b || point == (trayPoint{-1, -1}) {
						trayUser32.NewProc("GetCursorPos").Call(uintptr(unsafe.Pointer(&point)))
					}
					if action := menu.show(hwnd, point); action != "" {
						go run(action)
					} else if t.available() {
						notify(3) // NIM_SETFOCUS
					}
				}
				return 0
			case 0x10:
				if menu.tracking {
					trayUser32.NewProc("EndMenu").Call()
				}
				t.present.Store(false)
				notify(2)
				trayUser32.NewProc("DestroyWindow").Call(hwnd)
				return 0
			case 0x2:
				t.hwnd.Store(0)
				trayUser32.NewProc("PostQuitMessage").Call(0)
				return 0
			}
			r, _, _ := trayUser32.NewProc("DefWindowProcW").Call(hwnd, uintptr(msg), w, l)
			return r
		})
		instance, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetModuleHandleW").Call(0)
		cls := trayWindowClass{Proc: callback, Instance: instance, ClassName: name}
		cls.Size = uint32(unsafe.Sizeof(cls))
		registered, _, e := trayUser32.NewProc("RegisterClassExW").Call(uintptr(unsafe.Pointer(&cls)))
		if registered == 0 {
			ready <- e
			return
		}
		defer trayUser32.NewProc("UnregisterClassW").Call(uintptr(unsafe.Pointer(name)), instance)
		hwnd, _, e := trayUser32.NewProc("CreateWindowExW").Call(0, uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(name)), 0, 0, 0, 0, 0, 0, 0, instance, 0)
		if hwnd == 0 {
			ready <- e
			return
		}
		t.hwnd.Store(hwnd)
		data.Window = hwnd
		if !add() {
			trayUser32.NewProc("DestroyWindow").Call(hwnd)
			ready <- fmt.Errorf("Windows не добавила значок в трей")
			return
		}
		ready <- nil
		var message trayMSG
		for {
			r, _, _ := trayUser32.NewProc("GetMessageW").Call(uintptr(unsafe.Pointer(&message)), 0, 0, 0)
			if int32(r) <= 0 {
				break
			}
			trayUser32.NewProc("TranslateMessage").Call(uintptr(unsafe.Pointer(&message)))
			trayUser32.NewProc("DispatchMessageW").Call(uintptr(unsafe.Pointer(&message)))
		}
		t.present.Store(false)
	}()
	if err := <-ready; err != nil {
		return nil, err
	}
	return t, nil
}

func autostartCommand(executable string) string { return syscall.EscapeArg(executable) + " --startup" }
func configureAutostart(enabled bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return writeAutostartValue(`Software\Microsoft\Windows\CurrentVersion\Run`, exe, enabled)
}
func writeAutostartValue(path, executable string, enabled bool) error {
	key, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("не удалось изменить автозапуск: %w", err)
	}
	defer key.Close()
	if !enabled {
		err = key.DeleteValue("Sintet")
		if err == registry.ErrNotExist {
			return nil
		}
		return err
	}
	return key.SetStringValue("Sintet", autostartCommand(executable))
}
