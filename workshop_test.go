package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sintet/internal/launcher"
	"sintet/internal/workshop"
)

func TestWorkshopTargetsUseTheSameDirectoriesAsMinecraft(t *testing.T) {
	root := t.TempDir()
	java := filepath.Join(root, "java.exe")
	os.WriteFile(java, []byte("fixture"), 0600)
	id := "fabric-0.16.0-1.21.1"
	receipt := launcher.Receipt{ID: id, Base: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.0", Java: java, Files: []launcher.File{}}
	dir := filepath.Join(root, ".sintet", "installed")
	os.MkdirAll(dir, 0755)
	data, _ := json.Marshal(receipt)
	os.WriteFile(filepath.Join(dir, id+".json"), data, 0600)
	for _, mode := range []string{"profile", "family", "shared"} {
		t.Run(mode, func(t *testing.T) {
			settings := launcher.DefaultSettings(root)
			settings.InstanceMode = mode
			app := &App{service: launcher.New(root), settings: settings, workshop: workshop.NewManager(workshop.New("")), profiles: &profileRoster{root: t.TempDir()}}
			targets, err := app.WorkshopTargets()
			if err != nil || len(targets) != 1 {
				t.Fatal("installed instance missing", err)
			}
			expected, err := app.service.ProfileDirectory(id, mode)
			if err != nil || targets[0].Directory != expected {
				t.Fatal("workshop disagrees with launch directory")
			}
			os.WriteFile(filepath.Join(expected, "mods", "manually-added.jar"), []byte("manual fixture"), 0600)
			items, err := app.LocalWorkshop(id, "mods")
			if err != nil || len(items) != 1 || items[0].Filename != "manually-added.jar" {
				t.Fatal("manual local mod missing", err)
			}
			if _, err = app.LocalWorkshop("../../outside", "mods"); err == nil {
				t.Fatal("accepted an uninstalled instance")
			}
		})
	}
}

func TestLiveWorkshopAppInstallFlow(t *testing.T) {
	if os.Getenv("SINTET_WORKSHOP_INTEGRATION") != "1" {
		t.Skip("opt-in app API integration using a temporary Minecraft instance")
	}
	root := t.TempDir()
	java := filepath.Join(root, "java.exe")
	os.WriteFile(java, []byte("fixture"), 0600)
	id := "fabric-test-1.21.1"
	receipt := launcher.Receipt{ID: id, Base: "1.21.1", Loader: "fabric", Java: java, Files: []launcher.File{}}
	receiptDir := filepath.Join(root, ".sintet", "installed")
	os.MkdirAll(receiptDir, 0755)
	data, _ := json.Marshal(receipt)
	os.WriteFile(filepath.Join(receiptDir, id+".json"), data, 0600)
	app := &App{ctx: context.Background(), service: launcher.New(root), settings: launcher.DefaultSettings(root), workshop: workshop.NewManager(workshop.New("")), profiles: &profileRoster{root: t.TempDir()}}
	project, err := app.ModDetails("modrinth", "iris")
	if err != nil {
		t.Fatal(err)
	}
	versions, err := app.ModVersions("modrinth", project.ID, "1.21.1", "fabric")
	if err != nil || len(versions) == 0 {
		t.Fatal(err)
	}
	version := versions[0]
	for _, v := range versions {
		if v.Release == "release" {
			version = v
			break
		}
	}
	plan, err := app.PrepareMod("modrinth", project.ID, version.ID, id)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.InstallMod(plan.ID, true); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(90 * time.Second)
	for app.ModInstallState().Busy && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if state := app.ModInstallState(); state.Busy || state.Error != "" {
		t.Fatal(state.Error)
	}
	items, err := app.LocalWorkshop(id, "mods")
	if err != nil || len(items) < 2 {
		t.Fatal("app did not expose the installed mod and dependency", err)
	}
	for _, item := range items {
		if !item.Enabled || item.Provider != "modrinth" {
			t.Fatal("incorrect installed mod metadata")
		}
		if strings.HasPrefix(item.Icon, "/workshop-icons/") {
			response := httptest.NewRecorder()
			app.serveLauncherAssets(http.NotFoundHandler()).ServeHTTP(response, httptest.NewRequest("GET", item.Icon, nil))
			if response.Code != 200 {
				t.Fatal("extracted JAR icon was not served")
			}
		}
	}
	if _, err = os.Stat(filepath.Join(root, "mods", version.Filename)); !os.IsNotExist(err) {
		t.Fatal("mod leaked into the common game folder")
	}
	t.Logf("Verified app bridge flow: compatible file, dependency plan, installation into the selected instance, %d local mods and JAR icons", len(items))
}
