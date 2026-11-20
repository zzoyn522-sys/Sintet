package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/xml"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestRefreshLauncherInstallationVersion(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "Sintet-launcher.exe")
	payload := []byte("updated launcher")
	if err := os.WriteFile(exe, payload, 0600); err != nil {
		t.Fatal(err)
	}
	marker := launcherInstallMarker{Product: "Sintet.Launcher.8E6E8495-DB20-44FC-963E-567A8AC82181", Directory: dir, LauncherHash: "old", UninstallerHash: "original", Version: "0.8.4"}
	const version = "1.10.0.2"
	if err := refreshLauncherInstallMarker(exe, &marker, version); err != nil {
		t.Fatal(err)
	}
	saved, err := os.ReadFile(filepath.Join(dir, "install.xml"))
	if err != nil {
		t.Fatal(err)
	}
	var updated launcherInstallMarker
	if err := xml.Unmarshal(saved, &updated); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(payload)
	if updated.Version != version || updated.LauncherHash != hex.EncodeToString(sum[:]) || updated.UninstallerHash != "original" ||
		updated.Product != marker.Product || updated.Directory != dir {
		t.Fatalf("unexpected installation marker: %+v", updated)
	}

	keyPath := `Software\Sintet-install-version-test-` + filepath.Base(filepath.Dir(dir))
	key, opened, err := registry.CreateKey(registry.CURRENT_USER, keyPath, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	defer key.Close()
	if opened {
		t.Fatal("test registry key already exists")
	}
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, keyPath) })
	if err := key.SetStringValue("InstallLocation", dir); err != nil {
		t.Fatal(err)
	}
	if err := key.SetStringValue("DisplayVersion", "0.8.4"); err != nil {
		t.Fatal(err)
	}
	if err := syncLauncherDisplayVersion(key, filepath.Join(dir, "other"), version); err != nil {
		t.Fatal(err)
	}
	if value, _, err := key.GetStringValue("DisplayVersion"); err != nil || value != "0.8.4" {
		t.Fatalf("changed an unrelated installation: %q, %v", value, err)
	}
	if err := syncLauncherDisplayVersion(key, dir, version); err != nil {
		t.Fatal(err)
	}
	if value, _, err := key.GetStringValue("DisplayVersion"); err != nil || value != version {
		t.Fatalf("unexpected Windows version: %q, %v", value, err)
	}
}
