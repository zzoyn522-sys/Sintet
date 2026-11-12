package launcher

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchPreferencesBecomeArguments(t *testing.T) {
	s := New(t.TempDir())
	r := Receipt{ID: "1.21.1", Base: "1.21.1", Loader: "vanilla", Java: "automatic-java", Classpath: []string{"library.jar", "client.jar"}}
	r.Profile.MainClass = "minecraft.Main"
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}", "--gameDir", "${game_directory}", "--width", "${resolution_width}", "--height", "${resolution_height}")
	options := LaunchOptions{MemoryMB: 6144, Width: 1600, Height: 900, Fullscreen: true, JavaPath: "chosen-java", InstanceMode: "family"}
	spec, e := s.buildLaunch(r, "Player", options)
	if e != nil {
		t.Fatal(e)
	}
	if spec.Java != "chosen-java" || spec.Directory != filepath.Join(s.Root, "instances", "family", "vanilla-1.21") {
		t.Fatal(spec)
	}
	value := func(flag string) string {
		for i, arg := range spec.Args {
			if arg == flag && i+1 < len(spec.Args) {
				return spec.Args[i+1]
			}
		}
		return ""
	}
	if value("--width") != "1600" || value("--height") != "900" || value("--gameDir") != spec.Directory {
		t.Fatal(spec.Args)
	}
	if !strings.Contains(strings.Join(spec.Args, " "), "-Xmx6144M") || spec.Args[len(spec.Args)-1] != "--fullscreen" {
		t.Fatal(spec.Args)
	}
	options.InstanceMode = "shared"
	options.JavaPath = ""
	spec, e = s.buildLaunch(r, "Player", options)
	if e != nil || spec.Directory != s.Root || spec.Java != r.Java {
		t.Fatal(spec, e)
	}
}
func TestSettingsValidationAndAutoMemory(t *testing.T) {
	settings := DefaultSettings(filepath.Join(t.TempDir(), "Minecraft"))
	if e := ValidateSettings(settings); e != nil {
		t.Fatal(e)
	}
	if EffectiveMemory(settings, 16384) != 4096 {
		t.Fatal("auto memory too large")
	}
	if EffectiveMemory(settings, 4096) > 1536 {
		t.Fatal("auto memory lacks headroom")
	}
	settings.AutoMemory = false
	settings.MemoryMB = 6144
	if EffectiveMemory(settings, 16384) != 6144 {
		t.Fatal("manual memory ignored")
	}
	settings.Width = 100
	if ValidateSettings(settings) == nil {
		t.Fatal("invalid resolution accepted")
	}
}

func TestOverlayFontPreferenceSurvivesSettingsRead(t *testing.T) {
	root := t.TempDir()
	defaults := DefaultSettings(filepath.Join(root, "Minecraft"))
	path := filepath.Join(root, "settings.json")
	if err := os.WriteFile(path, []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	settings, err := ReadSettings(path, defaults)
	if err != nil || !settings.ApplyOverlayFont {
		t.Fatal("older settings lost the existing overlay font behavior", err)
	}
	settings.ApplyOverlayFont = false
	if err = WriteSettings(path, settings); err != nil {
		t.Fatal(err)
	}
	if settings, err = ReadSettings(path, defaults); err != nil || settings.ApplyOverlayFont {
		t.Fatal("disabled overlay font preference did not persist", err)
	}
}
func TestMigrationPreservesWorldAndRebasesInstall(t *testing.T) {
	parent := t.TempDir()
	oldRoot := filepath.Join(parent, "portable Minecraft")
	newRoot := filepath.Join(parent, "Documents", "Sintet", "Minecraft")
	s := New(oldRoot)
	paths := []string{"runtime/java/bin/java.exe", "libraries/test.jar", "instances/1.21.1/saves/World/level.dat"}
	for _, p := range paths {
		target := filepath.Join(oldRoot, filepath.FromSlash(p))
		os.MkdirAll(filepath.Dir(target), 0755)
		os.WriteFile(target, []byte("a"), 0644)
	}
	r := Receipt{ID: "1.21.1", Java: filepath.Join(oldRoot, "runtime/java/bin/java.exe"), GameDir: filepath.Join(oldRoot, "instances/1.21.1"), Classpath: []string{filepath.Join(oldRoot, "libraries/test.jar")}, Files: []File{{Path: "libraries/test.jar", Size: 1}}}
	saveJSON(s.receiptPath(r.ID), r)
	if e := MigrateRoot(oldRoot, newRoot); e != nil {
		t.Fatal(e)
	}
	moved := New(newRoot)
	if !moved.Installed(r.ID) {
		t.Fatal("moved install lost")
	}
	receipt, e := moved.readReceipt(r.ID)
	if e != nil || !strings.HasPrefix(receipt.Java, newRoot) || !strings.HasPrefix(receipt.GameDir, newRoot) || !strings.HasPrefix(receipt.Classpath[0], newRoot) {
		t.Fatal(receipt, e)
	}
	if _, e = os.Stat(filepath.Join(newRoot, filepath.FromSlash(paths[2]))); e != nil {
		t.Fatal("world lost", e)
	}
}
func TestMigrationRefusesOverwriteAndNestedPaths(t *testing.T) {
	parent := t.TempDir()
	oldRoot := filepath.Join(parent, "old")
	newRoot := filepath.Join(parent, "new")
	os.MkdirAll(oldRoot, 0755)
	os.MkdirAll(newRoot, 0755)
	os.WriteFile(filepath.Join(oldRoot, "world"), []byte("original"), 0644)
	os.WriteFile(filepath.Join(newRoot, "world"), []byte("existing"), 0644)
	if MigrateRoot(oldRoot, newRoot) == nil {
		t.Fatal("overwrote nonempty directory")
	}
	b, _ := os.ReadFile(filepath.Join(newRoot, "world"))
	if string(b) != "existing" {
		t.Fatal("target modified")
	}
	if MigrateRoot(oldRoot, filepath.Join(oldRoot, "child")) == nil {
		t.Fatal("nested move accepted")
	}
}
func TestNeoForgeMinecraftVersionMapping(t *testing.T) {
	for input, expected := range map[string]string{"21.1.265": "1.21.1", "21.0.1": "1.21", "20.4.100": "1.20.4", "26.3.1-beta": "26.3", "invalid": ""} {
		if actual := neoMinecraftVersion(input); actual != expected {
			t.Fatalf("%s => %s, want %s", input, actual, expected)
		}
	}
}
func TestNicknameControlsLaunchIdentity(t *testing.T) {
	s := New(t.TempDir())
	r := Receipt{ID: "1.21.1", Base: "1.21.1", Loader: "vanilla", Java: "java"}
	r.Profile.MainClass = "minecraft.Main"
	r.Profile.Arguments.Game = rawStrings("--username", "${auth_player_name}", "--uuid", "${auth_uuid}")
	options := LaunchOptions{MemoryMB: 4096, Width: 1280, Height: 720, InstanceMode: "profile"}
	identity := func(name string) (string, string) {
		spec, e := s.buildLaunch(r, name, options)
		if e != nil {
			t.Fatal(e)
		}
		values := map[string]string{}
		for i, arg := range spec.Args {
			if (arg == "--username" || arg == "--uuid") && i+1 < len(spec.Args) {
				values[arg] = spec.Args[i+1]
			}
		}
		return values["--username"], values["--uuid"]
	}
	name, first := identity("Player")
	if name != "Player" || first != offlineUUID("Player") {
		t.Fatal(name, first)
	}
	name, second := identity("SintetUser")
	_, repeat := identity("SintetUser")
	if name != "SintetUser" || first == second || second != repeat || second != offlineUUID("SintetUser") {
		t.Fatal("nickname and UUID not propagated consistently", name, first, second, repeat)
	}
}
