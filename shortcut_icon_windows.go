//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"sintet/internal/accountfiles"
)

//go:generate go run ./cmd/launcher-icon

type launcherInstallMarker struct {
	XMLName         xml.Name `xml:"InstallMarker"`
	Product         string
	Directory       string
	LauncherHash    string
	UninstallerHash string
	Version         string
}

func refreshLauncherInstallation() {
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
	var marker launcherInstallMarker
	err = xml.NewDecoder(io.LimitReader(markerFile, 16*1024)).Decode(&marker)
	_ = markerFile.Close()
	if err != nil || marker.Product != "Sintet.Launcher.8E6E8495-DB20-44FC-963E-567A8AC82181" ||
		!strings.EqualFold(filepath.Clean(marker.Directory), dir) {
		return
	}

	if err := refreshLauncherInstallMarker(executable, &marker, launcherVersion); err != nil {
		log.Printf("refresh Sintet installation metadata: %v", err)
	} else {
		refreshLauncherDisplayVersion(dir, launcherVersion)
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

func refreshLauncherInstallMarker(executable string, marker *launcherInstallMarker, version string) error {
	if marker.Version == version {
		return nil
	}
	file, err := os.Open(executable)
	if err != nil {
		return err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err := io.Copy(hash, file); err != nil {
		return err
	}
	marker.Version = version
	marker.LauncherHash = hex.EncodeToString(hash.Sum(nil))
	data, err := xml.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return accountfiles.Write(filepath.Join(filepath.Dir(executable), "install.xml"), append([]byte(xml.Header), data...))
}

func refreshLauncherDisplayVersion(directory, version string) {
	const uninstallKey = `Software\Microsoft\Windows\CurrentVersion\Uninstall\Sintet`
	key, err := registry.OpenKey(registry.CURRENT_USER, uninstallKey, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()
	if err := syncLauncherDisplayVersion(key, directory, version); err != nil {
		log.Printf("refresh Sintet Windows display version: %v", err)
	}
}

func syncLauncherDisplayVersion(key registry.Key, directory, version string) error {
	location, _, err := key.GetStringValue("InstallLocation")
	if err != nil || !strings.EqualFold(filepath.Clean(location), directory) {
		return nil
	}
	current, _, _ := key.GetStringValue("DisplayVersion")
	if current != version {
		return key.SetStringValue("DisplayVersion", version)
	}
	return nil
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
