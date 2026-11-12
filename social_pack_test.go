package main

import (
	"context"
	"os"
	"path/filepath"
	"sintet/internal/launcher"
	"sintet/internal/packsync"
	"strings"
	"testing"
)

func TestP2PPackMaterializesAsASeparateRegisteredBuild(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Minecraft")
	host := t.TempDir()
	os.MkdirAll(filepath.Join(host, "mods"), 0700)
	os.MkdirAll(filepath.Join(host, "config"), 0700)
	os.WriteFile(filepath.Join(host, "mods", "example.jar"), []byte("PK\x03\x04shared fixture"), 0600)
	os.WriteFile(filepath.Join(host, "config", "example.toml"), []byte("setting=true"), 0600)
	original := filepath.Join(game, "instances", "original", "mods", "personal.jar")
	os.MkdirAll(filepath.Dir(original), 0700)
	os.WriteFile(original, []byte("preserve me"), 0600)
	app := &App{profiles: &profileRoster{root: root}, service: launcher.New(game), settingsPath: filepath.Join(root, "settings.json"), settings: launcher.DefaultSettings(game)}
	manager := newSocialManager(app, context.Background())
	snapshot, err := packsync.Scan(context.Background(), host, packsync.Manifest{Name: "Friend's pack", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}, manager.packCache())
	if err != nil {
		t.Fatal(err)
	}
	plan := &guestPack{manifest: snapshot.Manifest, host: "fixture"}
	id, err := manager.materializePack(context.Background(), plan)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(id, "build-p2p-") {
		t.Fatal(id)
	}
	data, _ := os.ReadFile(original)
	if string(data) != "preserve me" {
		t.Fatal("overwrote player's original build")
	}
	if _, err = os.Stat(filepath.Join(game, "instances", id, "mods", "example.jar")); err != nil {
		t.Fatal(err)
	}
	store, err := app.readWorkshopBuilds()
	if err != nil || len(store.Builds) != 1 || store.Builds[0].ID != id || store.Builds[0].LoaderVersion != "0.16.14" {
		t.Fatal(store, err)
	}
	for _, file := range []string{"instance.json", ".sintet-p2p.json"} {
		if _, err = os.Stat(filepath.Join(game, "instances", id, file)); err != nil {
			t.Fatal(err)
		}
	}
	if manager.startPackSync("unapproved-digest") == nil {
		t.Fatal("accepted a sync without an approved plan")
	}
	if app.remotePackBusy.Load() {
		t.Fatal("invalid consent acquired global operation lock")
	}
}

func TestGuestPackJoiningDoesNotRequireConfigReceipt(t *testing.T) {
	dir := t.TempDir()
	for name, content := range map[string]string{"mods/example.jar": "mod", "config/example.toml": "value=true"} {
		file := filepath.Join(dir, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(file), 0700)
		os.WriteFile(file, []byte(content), 0600)
	}
	remote, err := packsync.Scan(context.Background(), dir, packsync.Manifest{Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}, "")
	if err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(dir, "config", "example.toml"), []byte("# generated\nvalue = true\n"), 0600)
	local, err := packsync.Scan(context.Background(), dir, remote.Manifest, "")
	if err != nil {
		t.Fatal(err)
	}
	if !packsync.Compatible(local.Manifest, remote.Manifest) {
		t.Fatal("configuration mismatch forced a download")
	}
	os.WriteFile(filepath.Join(dir, ".sintet-p2p.json"), []byte(`{"format":2}`), 0600)
	if !packsync.Compatible(local.Manifest, remote.Manifest) {
		t.Fatal("metadata alone prevented joining with identical mods")
	}
}
