package main

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"sintet/internal/launcher"
	"testing"
	"time"
)

func TestWorkshopSnapshotIncludesShaders(t *testing.T) {
	root := t.TempDir()
	game := filepath.Join(root, "Minecraft")
	settings := launcher.DefaultSettings(game)
	service := launcher.New(game)
	app := &App{profiles: &profileRoster{root: root}, service: service, settings: settings, settingsPath: filepath.Join(root, "settings.json")}
	java, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	const profile = "build-test"
	receipt, err := json.Marshal(launcher.Receipt{ID: profile, Base: "1.21.1", Loader: "fabric", Java: java})
	if err != nil {
		t.Fatal(err)
	}
	installed := filepath.Join(game, ".sintet", "installed")
	if err = os.MkdirAll(installed, 0755); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(installed, profile+".json"), receipt, 0600); err != nil {
		t.Fatal(err)
	}
	shaderDir := filepath.Join(game, "instances", profile, "shaderpacks")
	if err = os.MkdirAll(shaderDir, 0755); err != nil {
		t.Fatal(err)
	}
	var archive bytes.Buffer
	writer := zip.NewWriter(&archive)
	if _, err = writer.Create("shaders/composite.fsh"); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"Active.zip", "Disabled.zip.disabled"} {
		if err = os.WriteFile(filepath.Join(shaderDir, name), archive.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := app.workshopSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	shaders := map[string]bool{}
	for _, entry := range snapshot.Entries {
		if entry.Kind == "shaders" {
			if entry.Game != "1.21.1" || entry.Loader != "fabric" {
				t.Fatalf("wrong profile metadata: %+v", entry)
			}
			shaders[entry.Name] = entry.Enabled
		}
	}
	if disabled, exists := shaders["Disabled"]; len(shaders) != 2 || !shaders["Active"] || !exists || disabled {
		t.Fatalf("shader states missing from the snapshot: %v", shaders)
	}
}

func TestLiveWorkshopSync(t *testing.T) {
	if os.Getenv("SINTET_WORKSHOP_SYNC_QA") != "1" {
		t.Skip("opt-in real account inventory sync")
	}
	app := NewApp()
	snapshot, err := app.workshopSnapshot()
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	for _, private := range []string{app.profiles.root, app.settings.GameDirectory} {
		if bytes.Contains(data, []byte(private)) || bytes.Contains(data, []byte(bytes.ReplaceAll([]byte(private), []byte(`\`), []byte(`\\`)))) {
			t.Fatal("local directory leaked into snapshot")
		}
	}
	slot := app.profiles.find(app.profiles.active)
	if slot == nil || slot.sintet == nil || slot.sintet.SavedAccount() == nil {
		t.Fatal("active account is not signed into Sintet ID")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	if err = slot.sintet.PublishWorkshop(ctx, slot.sintet.SavedAccount().ID, snapshot); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	icons := 0
	for _, entry := range snapshot.Entries {
		counts[entry.Kind]++
		if entry.Icon != "" {
			icons++
		}
	}
	t.Logf("Real launcher inventory sent successfully: counts=%v, icons=%d, bytes=%d", counts, icons, len(data))
}
