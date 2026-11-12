package main

import (
	"os"
	"path/filepath"
	"sintet/internal/launcher"
	"strings"
	"testing"
	"time"
)

func TestCacheCleanupPreservesAccountsWorldsRecentLogsAndActiveFiles(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Minecraft")
	now := time.Now()
	old := now.Add(-9 * 24 * time.Hour)
	write := func(rel string, stale bool) {
		path := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("fixture"), 0600); err != nil {
			t.Fatal(err)
		}
		if stale {
			os.Chtimes(path, old, old)
		}
	}
	removable := []string{"P2P/Temp/stale.tmp", "Minecraft/.sintet/logs/old.log", "p2p.log", "Avatars/" + strings.Repeat("a", 64) + ".png", "Minecraft/assets/skins/aa/texture"}
	preserved := []string{"accounts.json", "accounts.json.bak", "Minecraft/instances/build-test/saves/World/level.dat", "Minecraft/instances/build-test/mods/example.jar", "Minecraft/.sintet/logs/current.log", "Minecraft/.sintet/logs/recent.log", "Minecraft/.sintet/logs/old-but-not-log.json", "Avatars/do-not-touch.txt", "P2P/Temp/recent.tmp"}
	for _, p := range removable {
		write(p, true)
	}
	for _, p := range preserved {
		write(p, p != "P2P/Temp/recent.tmp" && p != "Minecraft/.sintet/logs/recent.log")
	}
	app := &App{profiles: &profileRoster{root: root}, settings: launcher.DefaultSettings(game), service: launcher.New(game)}
	// Preserve the last game journal even if it is older than the retention period.
	files, report, err := cacheInventory(root, game, filepath.Join(game, ".sintet/logs/current.log"), now)
	if err != nil || len(files) != 5 || report.Files != 5 {
		t.Fatal(len(files), report, err)
	}
	// Freshen it before invoking the public cleanup method (the service has no game).
	current := filepath.Join(game, ".sintet/logs/current.log")
	os.Chtimes(current, now, now)
	result, err := app.ClearCache()
	if err != nil || result.Removed != 5 || result.Freed != 35 {
		t.Fatal(result, err)
	}
	for _, p := range preserved {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); err != nil {
			t.Fatalf("user data removed %s: %v", p, err)
		}
	}
	for _, p := range removable {
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(p))); !os.IsNotExist(err) {
			t.Fatalf("cache retained %s: %v", p, err)
		}
	}
	if safeCacheFile(root, filepath.Join(root, "..", "elsewhere")) {
		t.Fatal("outside path accepted")
	}
}

func TestCacheDoesNotFollowLinks(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	os.MkdirAll(filepath.Join(root, "P2P"), 0700)
	if err := os.Symlink(outside, filepath.Join(root, "P2P", "Temp")); err != nil {
		t.Skip("symlink privilege unavailable")
	}
	victim := filepath.Join(outside, "important.tmp")
	os.WriteFile(victim, []byte("outside"), 0600)
	old := time.Now().Add(-10 * 24 * time.Hour)
	os.Chtimes(victim, old, old)
	files, _, err := cacheInventory(root, filepath.Join(root, "Minecraft"), "", time.Now())
	if err != nil || len(files) != 0 {
		t.Fatal(files, err)
	}
	if _, err := os.Stat(victim); err != nil {
		t.Fatal("followed link", err)
	}
}
