//go:build windows

package main

import (
	"context"
	"crypto/sha256"
	"debug/pe"
	_ "embed"
	"encoding/hex"
	"fmt"
	"golang.org/x/sys/windows"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"unsafe"

	"sintet/internal/accountfiles"
	"sintet/internal/launcher"
	"sintet/internal/sldcfg"
)

//go:embed native/bin/sintet-overlay.dll
var overlayBinary []byte

//go:embed bridge/sintet-bridge.jar
var gameBridgeBinary []byte

type overlayBridge struct {
	mu                                    sync.Mutex
	handle                                windows.Handle
	locked                                *os.File
	start, stop, setGame, enable, preview uintptr
	previewFrame, previewInput            uintptr
	mediaEnable, mediaRead                uintptr
	mediaEnabled                          bool
	mediaCover                            []byte
	mediaPixels, mediaCoverDigest         [32]byte
}

func lockOverlayDLL(root string) (string, *os.File, error) {
	digest := sha256.Sum256(overlayBinary)
	dir := filepath.Join(root, "components", "overlay", hex.EncodeToString(digest[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", nil, err
	}
	path := filepath.Join(dir, "sintet-overlay.dll")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		file, e := os.CreateTemp(dir, "overlay-*.tmp")
		if e != nil {
			return "", nil, e
		}
		temp := file.Name()
		defer os.Remove(temp)
		if _, e = file.Write(overlayBinary); e == nil {
			e = file.Sync()
		}
		closeErr := file.Close()
		if e != nil {
			return "", nil, e
		}
		if closeErr != nil {
			return "", nil, closeErr
		}
		if e = os.Rename(temp, path); e != nil {
			return "", nil, e
		}
	}
	utf, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", nil, err
	}
	h, err := windows.CreateFile(utf, windows.GENERIC_READ, windows.FILE_SHARE_READ, nil, windows.OPEN_EXISTING, windows.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return "", nil, err
	}
	locked := os.NewFile(uintptr(h), path)
	actual, err := io.ReadAll(io.LimitReader(locked, int64(len(overlayBinary))+1))
	if err != nil || sha256.Sum256(actual) != digest {
		locked.Close()
		return "", nil, fmt.Errorf("проверка целостности Sintet Overlay не пройдена")
	}
	return path, locked, nil
}

func (a *App) prepareGameOverlay(javaPath string) (string, io.Closer, error) {
	executable, err := pe.Open(javaPath)
	if err != nil {
		return "", nil, err
	}
	defer executable.Close()
	if executable.Machine != pe.IMAGE_FILE_MACHINE_AMD64 {
		return "", nil, fmt.Errorf("Sintet Overlay требует Java для Windows x64")
	}
	path, locked, err := lockOverlayDLL(a.profiles.root)
	if err != nil {
		return "", nil, err
	}
	if strings.Contains(path, "=") {
		locked.Close()
		return "", nil, fmt.Errorf("путь Sintet Overlay содержит знак =")
	}
	return path, locked, nil
}

func openOverlay(root string) (*overlayBridge, error) {
	path, locked, err := lockOverlayDLL(root)
	if err != nil {
		return nil, err
	}
	b := &overlayBridge{locked: locked}

	b.handle, err = windows.LoadLibraryEx(path, 0, windows.LOAD_LIBRARY_SEARCH_DLL_LOAD_DIR|windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
	if err != nil {
		b.locked.Close()
		return b, err
	}
	symbols := map[string]*uintptr{"SintetOverlayStart": &b.start, "SintetOverlayStop": &b.stop, "SintetOverlaySetGame": &b.setGame, "SintetOverlayEnable": &b.enable, "SintetOverlayPreview": &b.preview}
	var abi uintptr
	abi, err = windows.GetProcAddress(b.handle, "SintetOverlayABI")
	for name, target := range symbols {
		if err == nil {
			*target, err = windows.GetProcAddress(b.handle, name)
		}
	}
	if err == nil {
		value, _, _ := syscall.SyscallN(abi)
		if value != 1 {
			err = fmt.Errorf("неподдерживаемая версия Sintet Overlay")
		}
	}
	if err == nil {
		b.previewFrame, err = windows.GetProcAddress(b.handle, "SintetOverlayPreviewFrame")
	}
	if err == nil {
		b.previewInput, err = windows.GetProcAddress(b.handle, "SintetOverlayPreviewInput")
	}
	if err != nil {
		windows.FreeLibrary(b.handle)
		b.locked.Close()
		b.handle = 0
		return b, err
	}
	b.mediaEnable, _ = windows.GetProcAddress(b.handle, "SintetPresenceMediaEnable")
	b.mediaRead, _ = windows.GetProcAddress(b.handle, "SintetPresenceMediaRead")
	value, _, _ := syscall.SyscallN(b.start)
	if value == 0 {
		b.close()
		return b, fmt.Errorf("не удалось запустить Sintet Overlay")
	}
	return b, nil
}
func (b *overlayBridge) close() {
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle == 0 {
		return
	}
	if b.mediaEnabled {
		syscall.SyscallN(b.mediaEnable, 0)
		b.mediaEnabled = false
	}
	done, _, _ := syscall.SyscallN(b.stop)
	if done != 0 {
		windows.FreeLibrary(b.handle)
		b.handle = 0
		if b.locked != nil {
			b.locked.Close()
		}
	}
}
func (b *overlayBridge) update(pid uint32, name, build string, enabled bool) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle == 0 {
		return
	}
	nick, _ := windows.UTF16PtrFromString(name)
	version, _ := windows.UTF16PtrFromString(build)
	flag := uintptr(0)
	if enabled {
		flag = 1
	}
	syscall.SyscallN(b.enable, flag)
	syscall.SyscallN(b.setGame, uintptr(pid), uintptr(unsafe.Pointer(nick)), uintptr(unsafe.Pointer(version)))
}
func (a *App) watchOverlay(ctx context.Context) {
	bridge, err := openOverlay(a.profiles.root)
	a.overlayMu.Lock()
	a.overlay = bridge
	a.overlayError = err
	a.overlayMu.Unlock()
	if err != nil {
		return
	}
	<-ctx.Done()
	bridge.close()
}

func (a *App) PreviewOverlay(style sldcfg.OverlayAppearance, accent string, mode int) error {
	if !style.Valid() || mode < 0 || mode > 3 {
		return fmt.Errorf("проверь параметры оформления оверлея")
	}
	a.mu.RLock()
	settings := a.settings
	a.mu.RUnlock()
	language := launcher.InterfaceLanguage(settings.InterfaceLanguage, launcher.SystemLanguage())
	data, err := syscall.BytePtrFromString(style.RendererSettings(accent))
	if err != nil {
		return err
	}
	locale, _ := syscall.BytePtrFromString(language)
	var fontPath string
	if mode == 1 || mode == 2 {
		fontPath = a.overlayFontPath(settings.ApplyOverlayFont)
	}
	font, err := windows.UTF16PtrFromString(fontPath)
	if err != nil {
		return err
	}
	a.overlayMu.Lock()
	defer a.overlayMu.Unlock()
	if a.overlayError != nil {
		return a.overlayError
	}
	if a.overlay == nil {
		return fmt.Errorf("оверлей ещё загружается")
	}
	b := a.overlay
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle == 0 {
		return fmt.Errorf("оверлей недоступен")
	}
	shown, _, _ := syscall.SyscallN(b.preview, uintptr(unsafe.Pointer(data)), uintptr(unsafe.Pointer(locale)), uintptr(unsafe.Pointer(font)), uintptr(mode))
	if shown == 0 {
		return fmt.Errorf("не удалось открыть предпросмотр оверлея")
	}
	return nil
}

const overlayPreviewFrameBytes = 704 * 396 * 4

var overlayPreviewFrames = sync.Pool{New: func() any {
	return new([overlayPreviewFrameBytes]byte)
}}

func (a *App) serveOverlayPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	previous, _ := strconv.ParseUint(r.URL.Query().Get("after"), 10, 32)
	a.overlayMu.Lock()
	b := a.overlay
	a.overlayMu.Unlock()
	if b == nil {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	frame := overlayPreviewFrames.Get().(*[overlayPreviewFrameBytes]byte)
	defer overlayPreviewFrames.Put(frame)
	b.mu.Lock()
	var sequence uintptr
	if b.handle != 0 {
		sequence, _, _ = syscall.SyscallN(b.previewFrame, uintptr(unsafe.Pointer(frame)), overlayPreviewFrameBytes, uintptr(previous))
	}
	b.mu.Unlock()
	if sequence == 0 {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("X-Preview-Frame", strconv.FormatUint(uint64(sequence), 10))
	w.Write(frame[:])
}

func (a *App) OverlayPreviewInput(events [][4]int) {
	if len(events) > 32 {
		return
	}
	a.overlayMu.Lock()
	b := a.overlay
	a.overlayMu.Unlock()
	if b == nil {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.handle == 0 {
		return
	}
	for _, event := range events {
		syscall.SyscallN(b.previewInput, uintptr(event[0]), uintptr(event[1]), uintptr(event[2]), uintptr(event[3]))
	}
}

func (a *App) prepareJavaBridge() (string, error) {
	hash := sha256.Sum256(gameBridgeBinary)
	dir := filepath.Join(a.profiles.root, "components", "game-bridge", hex.EncodeToString(hash[:]))
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "sintet-bridge.jar")
	if data, err := os.ReadFile(path); err == nil && sha256.Sum256(data) == hash {
		return path, nil
	}
	f, err := os.CreateTemp(dir, "bridge-*.tmp")
	if err != nil {
		return "", err
	}
	tmp := f.Name()
	defer os.Remove(tmp)
	_, err = f.Write(gameBridgeBinary)
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if err = os.Rename(tmp, path); err != nil {
		return "", err
	}
	return path, nil
}

// Use the launcher's selected font when ImGui can read its format.
func (a *App) overlayFontPath(enabled bool) string {
	if !enabled {
		return ""
	}
	appearance := a.GetAppearance()
	if appearance.Font == "custom" && appearanceFontFile.MatchString(appearance.FontFile) {
		switch filepath.Ext(appearance.FontFile) {
		case ".ttf", ".otf":
			return filepath.Join(a.backgroundDir, appearance.FontFile)
		}
	}
	windowsFonts := filepath.Join(os.Getenv("WINDIR"), "Fonts")
	switch appearance.Font {
	case "arial":
		return filepath.Join(windowsFonts, "arialbd.ttf")
	case "georgia":
		return filepath.Join(windowsFonts, "georgiab.ttf")
	case "verdana":
		return filepath.Join(windowsFonts, "verdanab.ttf")
	case "calibri":
		return filepath.Join(windowsFonts, "calibrib.ttf")
	case "mono":
		return filepath.Join(windowsFonts, "consolab.ttf")
	case "segoe", "custom":
		return ""
	}
	data, err := assets.ReadFile("frontend/assets/fonts/Inter-SemiBold.ttf")
	if err != nil {
		return ""
	}
	digest := sha256.Sum256(data)
	path := filepath.Join(a.profiles.root, "components", "fonts", hex.EncodeToString(digest[:]), "Inter-SemiBold.ttf")
	if cached, err := os.ReadFile(path); err == nil && sha256.Sum256(cached) == digest {
		return path
	}
	if err := accountfiles.Write(path, data); err != nil {
		return ""
	}
	return path
}

// OverlayBuildHash is the checksum of the DLL shipped in this launcher.
func (a *App) OverlayBuildHash() string {
	digest := sha256.Sum256(overlayBinary)
	return hex.EncodeToString(digest[:])
}
