//go:build windows

package main

import (
	"fmt"
	"golang.org/x/sys/windows/registry"
	"testing"
	"time"
	"unsafe"
)

func TestTrayWindowsABIAndStartupCommand(t *testing.T) {
	if unsafe.Sizeof(trayNotification{}) != 976 || unsafe.Sizeof(trayMSG{}) != 48 || unsafe.Sizeof(trayWindowClass{}) != 80 ||
		unsafe.Sizeof(trayIconIdentifier{}) != 40 || unsafe.Sizeof(trayPopupParams{}) != 20 || unsafe.Sizeof(trayMenuInfo{}) != 40 ||
		unsafe.Sizeof(trayMenuItemInfo{}) != 80 || unsafe.Sizeof(trayMeasureItem{}) != 32 || unsafe.Sizeof(trayDrawItem{}) != 64 {
		t.Fatal("incorrect Win32 structure layout")
	}
	exe := `C:\Users\Sample Person\Programs\Sintet\Sintet-launcher.exe`
	want := `"C:\Users\Sample Person\Programs\Sintet\Sintet-launcher.exe" --startup`
	if got := autostartCommand(exe); got != want {
		t.Fatal(got)
	}
	icon := createTrayIcon()
	if icon == 0 {
		t.Fatal("embedded Sintet icon could not be loaded")
	}
	trayUser32.NewProc("DestroyIcon").Call(icon)
}

func TestAutostartWritesOnlyItsOwnValueAndCanBeDisabled(t *testing.T) {
	path := fmt.Sprintf(`Software\SintetTests\Autostart-%d`, time.Now().UnixNano())
	defer registry.DeleteKey(registry.CURRENT_USER, path)
	exe := `C:\Programs With Spaces\Sintet.exe`
	if err := writeAutostartValue(path, exe, true); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	got, _, err := key.GetStringValue("Sintet")
	if err != nil || got != autostartCommand(exe) {
		t.Fatal(got, err)
	}
	key.SetStringValue("OtherApp", "preserve")
	if err = writeAutostartValue(path, exe, false); err != nil {
		t.Fatal(err)
	}
	if _, _, err = key.GetStringValue("Sintet"); err != registry.ErrNotExist {
		t.Fatal("autostart not removed", err)
	}
	got, _, err = key.GetStringValue("OtherApp")
	if err != nil || got != "preserve" {
		t.Fatal("unrelated startup value modified")
	}
	if err = writeAutostartValue(path, exe, false); err != nil {
		t.Fatal("disabling was not idempotent", err)
	}
}

func TestTrayCallbackAndShutdown(t *testing.T) {
	actions := make(chan string, 2)
	tray, err := newLauncherTray(func(action string) { actions <- action })
	if err != nil {
		t.Fatal(err)
	}
	if !tray.available() {
		t.Fatal("tray unavailable after startup")
	}
	trayUser32.NewProc("PostMessageW").Call(tray.hwnd.Load(), trayMessage, 0, 0x400)
	select {
	case action := <-actions:
		if action != "open" {
			t.Fatalf("unexpected tray action: %s", action)
		}
	case <-time.After(2 * time.Second):
		tray.close()
		t.Fatal("tray activation did not open launcher")
	}
	tray.close()
	if tray.available() {
		t.Fatal("tray icon retained after shutdown")
	}
}
