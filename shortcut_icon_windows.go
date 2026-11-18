//go:build windows

package main

import (
	"bytes"
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"sintet/internal/accountfiles"
)

//go:generate go run ./cmd/launcher-icon

func refreshLauncherShortcutIcon() {
	executable, err := os.Executable()
	if err != nil {
		return
	}
	if updateHealthMarker != "" {
		notifyShortcutItemChanged(executable)
	}

	// Setup-created shortcuts point to Sintet.ico beside the installed EXE.
	dir := filepath.Dir(executable)
	if !strings.EqualFold(filepath.Base(executable), "Sintet-launcher.exe") {
		return
	}
	markerFile, err := os.Open(filepath.Join(dir, "install.xml"))
	if err != nil {
		return
	}
	var marker struct {
		Product   string
		Directory string
	}
	err = xml.NewDecoder(io.LimitReader(markerFile, 16*1024)).Decode(&marker)
	_ = markerFile.Close()
	if err != nil || marker.Product != "Sintet.Launcher.8E6E8495-DB20-44FC-963E-567A8AC82181" ||
		!strings.EqualFold(filepath.Clean(marker.Directory), dir) {
		return
	}

	icon := filepath.Join(dir, "Sintet.ico")
	if info, err := os.Stat(icon); err == nil && info.Size() == int64(len(trayIconBytes)) {
		if current, err := os.ReadFile(icon); err == nil && bytes.Equal(current, trayIconBytes) {
			return
		}
	}
	if err := accountfiles.Write(icon, trayIconBytes); err != nil {
		return
	}
	notifyShortcutItemChanged(icon)
	if desktop, err := windows.KnownFolderPath(windows.FOLDERID_Desktop, 0); err == nil {
		notifyShortcutItemChanged(filepath.Join(desktop, "Sintet.lnk"))
	}
	if programs, err := windows.KnownFolderPath(windows.FOLDERID_Programs, 0); err == nil {
		notifyShortcutItemChanged(filepath.Join(programs, "Sintet", "Sintet.lnk"))
	}
	const shcneAssocChanged = 0x08000000
	trayShell32.NewProc("SHChangeNotify").Call(shcneAssocChanged, 0, 0, 0)
}

func notifyShortcutItemChanged(path string) {
	if _, err := os.Stat(path); err != nil {
		return
	}
	wide, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return
	}
	const shcneUpdateItem = 0x00002000
	const shcnfPathW = 0x0005
	trayShell32.NewProc("SHChangeNotify").Call(shcneUpdateItem, shcnfPathW, uintptr(unsafe.Pointer(wide)), 0)
}
