package main

import (
	"path/filepath"
	"sintet/internal/launcher"
	"testing"
)

func TestSavedSettingsAreBackendSourceOfTruth(t *testing.T) {
	root := t.TempDir()
	defaults := launcher.DefaultSettings(filepath.Join(root, "Minecraft"))
	a := &App{service: launcher.New(defaults.GameDirectory), settings: defaults, defaults: defaults, settingsPath: filepath.Join(root, "settings.json")}
	value := defaults
	value.Width = 1440
	value.Height = 900
	value.AutoMemory = false
	value.MemoryMB = 3072
	value.Fullscreen = true
	value.Snapshots = true
	result, e := a.SaveSettings(value, true)
	if e != nil {
		t.Fatal(e)
	}
	if result.EffectiveMemoryMB != 3072 || !result.Settings.Fullscreen {
		t.Fatal(result)
	}
	read, e := launcher.ReadSettings(a.settingsPath, defaults)
	if e != nil || read != value {
		t.Fatal(read, e)
	}
	value.GameDirectory = filepath.Join(root, "new Minecraft")
	result, e = a.SaveSettings(value, true)
	if e != nil || a.service.Root != value.GameDirectory {
		t.Fatal(result, e)
	}
	value.Height = 0
	if _, e = a.SaveSettings(value, true); e == nil {
		t.Fatal("invalid resolution saved")
	}
}
