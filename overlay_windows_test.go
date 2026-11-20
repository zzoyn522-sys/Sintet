//go:build windows

package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"sintet/internal/sldcfg"
)

func TestOverlayComponent(t *testing.T) {
	root := t.TempDir()
	bridge, err := openOverlay(root)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	bridge.update(0, "Sintet", "Minecraft Java Edition", true)
	if os.Getenv("SINTET_TEST_OVERLAY_PREVIEW") == "1" {
		app := &App{overlay: bridge}
		settings := sldcfg.DefaultOverlay()
		settings.Accent = "#ff0077"
		theme, _ := syscall.BytePtrFromString(settings.RendererSettings(""))
		language, _ := syscall.BytePtrFromString("ru")
		font, _ := windows.UTF16PtrFromString("")
		ready, _, _ := syscall.SyscallN(bridge.preview, uintptr(unsafe.Pointer(theme)), uintptr(unsafe.Pointer(language)), uintptr(unsafe.Pointer(font)), 1)
		if ready == 0 {
			t.Fatal("inline renderer failed to start")
		}
		for deadline := time.Now().Add(3 * time.Second); ; {
			response := httptest.NewRecorder()
			app.serveLauncherAssets(http.NotFoundHandler()).ServeHTTP(response, httptest.NewRequest("GET", "/overlay-preview/frame?after=0", nil))
			if response.Code == http.StatusOK && response.Body.Len() == overlayPreviewFrameBytes && bytes.Contains(response.Body.Bytes(), []byte{84, 13, 48}) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("inline preview produced no themed pixels, status %d, bytes %d", response.Code, response.Body.Len())
			}
			time.Sleep(60 * time.Millisecond)
		}
	}
	bridge.close()
	bridge, err = openOverlay(root)
	if err != nil {
		t.Fatal(err)
	}
	bridge.close()
	matches, err := filepath.Glob(filepath.Join(root, "components", "overlay", "*", "sintet-overlay.dll"))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	if err = os.WriteFile(matches[0], []byte("invalid DLL"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = openOverlay(root); err == nil {
		t.Fatal("modified component was accepted")
	}
}
