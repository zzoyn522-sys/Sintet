package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"golang.org/x/sys/windows"
)

func importFixtureFile(t *testing.T, root, relative, content string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func TestLauncherImportDetectsAndCopiesPrismInstance(t *testing.T) {
	root := t.TempDir()
	roaming := filepath.Join(root, "Roaming")
	instance := filepath.Join(roaming, "PrismLauncher", "instances", "Cozy")
	game := filepath.Join(instance, "minecraft")
	importFixtureFile(t, instance, "instance.cfg", "[General]\nname=Cozy Survival\n")
	importFixtureFile(t, instance, "mmc-pack.json", "{\"components\":[{\"uid\":\"net.minecraft\",\"version\":\"1.21.1\"},{\"uid\":\"net.fabricmc.fabric-loader\",\"version\":\"0.16.14\"}]}")
	importFixtureFile(t, game, "mods/fabric-api.jar.disabled", "test mod content")
	importFixtureFile(t, game, "saves/Cozy/level.dat", "test world")
	importFixtureFile(t, game, "saves/Cozy/session.lock", "test lock")
	importFixtureFile(t, game, "shaderpacks/example.zip", "test shader")
	importFixtureFile(t, game, "options.txt", "test options")
	importFixtureFile(t, game, "launcher_accounts.json", "credentials excluded")
	importFixtureFile(t, game, "logs/latest.log", "logs excluded")
	target := filepath.Join(root, "Sintet", "Minecraft")
	sources := detectLauncherImports(roaming, root, root, "", target)
	if len(sources) != 1 || sources[0].Launcher != "prism" || sources[0].Name != "Cozy Survival" ||
		sources[0].Game != "1.21.1" || sources[0].Loader != "fabric" || sources[0].LoaderVersion != "0.16.14" {
		t.Fatalf("incorrect source: %+v", sources)
	}
	source := sources[0]
	files, total, err := planLauncherImport(source.Folder)
	if err != nil {
		t.Fatal(err)
	}
	a := &App{settingsPath: filepath.Join(root, "Sintet", "settings.json")}
	var store workshopBuildStore
	var copied int64
	build, err := a.commitLauncherImport(context.Background(), target, source, files, &store, func(n int64) { copied += n })
	if err != nil || copied != total {
		t.Fatalf("copy failed %v (%d / %d)", err, copied, total)
	}
	newRoot := filepath.Join(target, "instances", build.ID)
	for _, name := range []string{"mods/fabric-api.jar.disabled", "saves/Cozy/level.dat", "shaderpacks/example.zip", "options.txt"} {
		before, err := os.ReadFile(filepath.Join(game, name))
		if err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(filepath.Join(newRoot, name))
		if err != nil || string(after) != string(before) {
			t.Fatalf("%s was not copied: %v", name, err)
		}
	}
	for _, name := range []string{"launcher_accounts.json", "logs/latest.log", "saves/Cozy/session.lock"} {
		if _, err := os.Stat(filepath.Join(newRoot, name)); !os.IsNotExist(err) {
			t.Fatalf("%s must not be imported", name)
		}
	}
	saved, err := a.readWorkshopBuilds()
	if err != nil || len(saved.Builds) != 1 || saved.Builds[0].ImportedFrom != source.ID {
		t.Fatalf("build not registered: %+v %v", saved, err)
	}
}

func TestLauncherImportDetectsTLauncherModpackWithCustomVersionID(t *testing.T) {
	root := t.TempDir()
	roaming := filepath.Join(root, "Roaming")
	game := filepath.Join(roaming, ".minecraft")
	version := filepath.Join(game, "versions", "Fog Pack")
	importFixtureFile(t, roaming, ".tlauncher/tlauncher-2.0.properties", "minecraft.gamedir="+game)
	importFixtureFile(t, version, "Fog Pack.json", "{\"inheritsFrom\":\"1.20.1\",\"libraries\":[{\"name\":\"net.minecraftforge:forge:1.20.1-47.2.17\"}]}")
	importFixtureFile(t, version, "mods/fog.jar", "test mod")
	found := detectLauncherImports(roaming, root, root, "", filepath.Join(root, "Sintet"))
	if len(found) != 1 || found[0].Launcher != "tlauncher" || found[0].Name != "Fog Pack" ||
		found[0].Game != "1.20.1" || found[0].Loader != "forge" || found[0].LoaderVersion != "47.2.17" {
		t.Fatalf("incorrect TLauncher source: %+v", found)
	}
}

func TestLauncherImportOnboardingPersistsBeforeSupport(t *testing.T) {
	root := t.TempDir()
	a := &App{importer: newLauncherImporter(root, true)}
	if !a.LauncherImportOffer() {
		t.Fatal("fresh launch did not offer import")
	}
	a.importer = newLauncherImporter(root, false)
	if !a.LauncherImportOffer() {
		t.Fatal("restart lost pending onboarding")
	}
	if err := a.FinishLauncherImportOffer(); err != nil {
		t.Fatal(err)
	}
	a.importer = newLauncherImporter(root, false)
	if a.LauncherImportOffer() {
		t.Fatal("completed onboarding repeated")
	}
	_ = newSupportManager(root, true)
	if notice := newSupportManager(root, false).notice("0.9.23.2"); notice.Kind != "welcome" {
		t.Fatal("restarting after the import replaced welcome with an update notice")
	}
}

func TestLauncherImportReadsModrinthDatabaseWithoutTouchingAccounts(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "profiles", "Fabric Pack")
	importFixtureFile(t, dir, "mods/example.jar", "test mod")
	dll := windows.NewLazySystemDLL("winsqlite3.dll")
	if err := dll.Load(); err != nil {
		t.Skip("Windows SQLite is unavailable", err)
	}
	path, _ := windows.BytePtrFromString(filepath.Join(root, "app.db"))
	var db uintptr
	open, _, _ := dll.NewProc("sqlite3_open_v2").Call(uintptr(unsafe.Pointer(path)), uintptr(unsafe.Pointer(&db)), 6, 0)
	if open != 0 {
		t.Fatalf("could not create test database: %d", open)
	}
	sql, _ := windows.BytePtrFromString("CREATE TABLE instances(id TEXT, path TEXT, name TEXT, applied_content_set_id TEXT);" +
		"CREATE TABLE instance_content_sets(id TEXT, instance_id TEXT, game_version TEXT, loader TEXT, loader_version TEXT);" +
		"INSERT INTO instances VALUES ('one','Fabric Pack','Modrinth Fabric','content');" +
		"INSERT INTO instance_content_sets VALUES ('content','one','1.21.1','fabric','0.16.14');")
	result, _, _ := dll.NewProc("sqlite3_exec").Call(db, uintptr(unsafe.Pointer(sql)), 0, 0, 0)
	dll.NewProc("sqlite3_close").Call(db)
	if result != 0 {
		t.Fatalf("could not prepare test database: %d", result)
	}
	sources := readModrinthImports(root)
	if len(sources) != 1 || sources[0].Name != "Modrinth Fabric" || sources[0].Folder != dir ||
		sources[0].Loader != "fabric" || sources[0].LoaderVersion != "0.16.14" {
		t.Fatalf("unexpected Modrinth profiles: %+v", sources)
	}
}

func TestLauncherImportSkipsUnsupportedModLoader(t *testing.T) {
	root := t.TempDir()
	importFixtureFile(t, root, "profiles/Quilt/profile.json", "{\"name\":\"Quilt\",\"game_version\":\"1.20.1\",\"loader\":\"quilt\",\"loader_version\":\"0.29.1\"}")
	found := detectLauncherImports(root, root, root, root, filepath.Join(root, "Sintet"))
	for _, entry := range found {
		if strings.EqualFold(entry.Name, "Quilt") {
			t.Fatalf("incompatible mod loader was offered: %+v", entry)
		}
	}
}
