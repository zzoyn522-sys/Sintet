package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOverlayHotkeyAllowlistAndEnvironment(t *testing.T) {
	for value, want := range map[string]string{"": "Shift + Tab", "Shift + Tab": "Shift + Tab", "Shift+Ctrl+F8": "Ctrl + Shift + F8", "Alt + M": "Alt + M", "Ctrl + 9": "Ctrl + 9"} {
		label, key, mods, err := ParseOverlayHotkey(value)
		if err != nil || label != want || key == 0 || mods == 0 {
			t.Fatalf("%q: %q %d %d %v", value, label, key, mods, err)
		}
	}
	for _, value := range []string{"Tab", "Win + A", "Alt + Tab", "Ctrl + Alt + Tab", "Alt + F4", "Ctrl + W", "Ctrl + Ctrl + A", "Ctrl + F13", "Ctrl + Escape", "Ctrl + ;", "Ctrl + A + B"} {
		if _, _, _, err := ParseOverlayHotkey(value); err == nil {
			t.Fatalf("accepted reserved/invalid shortcut %q", value)
		}
	}
	env := strings.Join(overlayEnvironment(Receipt{}, "Player", LaunchOptions{OverlayHotkey: "Ctrl + F8"}), "\n")
	for _, want := range []string{"SINTET_OVERLAY_HOTKEY=Ctrl + F8", "SINTET_OVERLAY_HOTKEY_KEY=119", "SINTET_OVERLAY_HOTKEY_MODS=2"} {
		if !strings.Contains(env, want) {
			t.Fatalf("missing %s", want)
		}
	}
}
func TestBehaviorSettingsPersistAndDefaultsForOldFiles(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "settings.json")
	defaults := DefaultSettings(filepath.Join(root, "Minecraft"))
	if err := os.WriteFile(path, []byte(`{"autoClose":false,"width":1600}`), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := ReadSettings(path, defaults)
	if err != nil || settings.GameStartAction != "tray" || settings.CloseAction != "quit" || settings.Width != 1600 || settings.OverlayHotkey != "Shift + Tab" {
		t.Fatal(settings, err)
	}
	settings.GameStartAction = "keep"
	settings.CloseAction = "tray"
	settings.StartWithWindows = true
	settings.StartInTray = true
	settings.OverlayHotkey = "Ctrl + F8"
	if err = WriteSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSettings(path, defaults)
	if err != nil || got != settings {
		t.Fatal(got, err)
	}
	settings.GameStartAction = "kill"
	if ValidateSettings(settings) == nil {
		t.Fatal("invalid launch behavior accepted")
	}
}
