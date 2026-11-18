package workshop

import (
	"archive/zip"
	"bytes"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestSintedotRepairsCatalogModAndMissingDependency(t *testing.T) {
	manager, _, corruptDownload := fixture(t)
	target := Target{ID: "build-local", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
	mods := filepath.Join(target.Directory, "mods")
	if err := os.Mkdir(mods, 0755); err != nil {
		t.Fatal(err)
	}
	write := func(path string, data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0644); err != nil {
			t.Fatal(err)
		}
	}
	broken := []byte("broken jar")
	write(filepath.Join(mods, "root.jar"), broken)
	copied := testJar(t, "duplicate")
	write(filepath.Join(mods, "a.jar"), copied)
	write(filepath.Join(mods, "b.jar"), copied)
	write(filepath.Join(mods, "intentionally-disabled.jar.disabled"), copied)
	record := Installed{Provider: "modrinth", Project: "root", Version: "root-v", Filename: "root.jar", Hash: "old"}
	data, err := json.Marshal(map[string]Installed{"modrinth:root": record})
	if err != nil {
		t.Fatal(err)
	}
	write(filepath.Join(target.Directory, ".sintet-mods.json"), data)

	*corruptDownload = true
	if err := manager.RunSintedot(t.Context(), target, "repair", nil); err == nil {
		t.Fatal("corrupt dependency was accepted")
	}
	if got, err := os.ReadFile(filepath.Join(mods, "root.jar")); err != nil || string(got) != string(broken) {
		t.Fatalf("failed repair changed the live JAR: %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(mods, "b.jar")); err != nil {
		t.Fatal(err)
	}

	*corruptDownload = false
	if err := manager.RunSintedot(t.Context(), target, "repair", []string{"test journal"}); err != nil {
		t.Fatal(err)
	}
	state := manager.SintedotState()
	if state.Busy || state.Percent != 100 || state.Backup == "" || len(state.Changes) != 4 || len(state.Journal) != 1 {
		t.Fatalf("unexpected Sintedot result: %+v", state)
	}
	for _, name := range []string{"root.jar", "dep.jar", "a.jar", "b.jar.disabled", "intentionally-disabled.jar.disabled"} {
		if _, err := os.Stat(filepath.Join(mods, name)); err != nil {
			t.Fatal(err)
		}
	}
	if saved, err := os.ReadFile(filepath.Join(state.Backup, "mods", "root.jar")); err != nil || string(saved) != string(broken) {
		t.Fatalf("backed up JAR: %q, %v", saved, err)
	}
}

func TestSintedotRespectsExactDependencyDuringUpdate(t *testing.T) {
	manager, _, _ := fixture(t)
	root, err := manager.Client.Version(t.Context(), "modrinth", "root", "root-v")
	if err != nil {
		t.Fatal(err)
	}
	dep, err := manager.Client.Version(t.Context(), "modrinth", "dep", "dep-v")
	if err != nil {
		t.Fatal(err)
	}
	newDep := dep
	newDep.ID = "dep-v-new"
	newDep.Published = time.Now()
	var issues []string
	keys := []string{"modrinth:dep", "modrinth:root"}
	planner := maintenancePlanner{
		manager: manager,
		target:  Target{Game: "1.21.1", Loader: "fabric"},
		records: map[string]Installed{
			"modrinth:dep":  {Provider: "modrinth", Project: "dep", Version: "dep-v", Filename: "dep.jar"},
			"modrinth:root": {Provider: "modrinth", Project: "root"},
		},
		active:  map[string]bool{"dep.jar": true},
		chosen:  map[string]Version{"modrinth:dep": newDep, "modrinth:root": root},
		planned: map[string]PlanEntry{},
		issues:  &issues,
	}
	if err := planner.resolve(t.Context(), keys); err != nil {
		t.Fatal(err)
	}
	if planner.chosen["modrinth:dep"].ID != dep.ID || len(planner.planned) != 1 || len(issues) != 1 {
		t.Fatalf("incorrectly replaced an exact dependency: %+v, %v", planner.planned, issues)
	}
}

func TestSintedotReadsRequiredForgeDependencies(t *testing.T) {
	metadata := `[[mods]]
modId="example"
[[dependencies.example]]
modId="library"
mandatory = true
side="BOTH"
[[dependencies.example]]
modId="serverlib"
type="required"
side="SERVER"
[[dependencies.example]]
modId="optional"
type="optional"
[[dependencies.example]]
modId="neolib"
type="required" # loader-specific dependency
`
	if got := strings.Join(forgeDependencies([]byte(metadata)), ","); got != "library,neolib" {
		t.Fatalf("incorrect mandatory dependencies: %q", got)
	}
}

func TestSintedotUpdatesToNewestCompatibleDownload(t *testing.T) {
	mod := testJar(t, "example")
	hash := sha512.Sum512(mod)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/example/version":
			version := func(id, date, filename, loader string) map[string]any {
				return map[string]any{"id": id, "project_id": "example", "name": id, "date_published": date,
					"game_versions": []string{"1.21.1"}, "loaders": []string{loader},
					"files": []any{map[string]any{"filename": filename, "url": server.URL + "/download/" + filename,
						"size": len(mod), "primary": true, "hashes": map[string]string{"sha512": hex.EncodeToString(hash[:])}}}}
			}
			_ = json.NewEncoder(w).Encode([]any{
				version("older", "2025-01-01T00:00:00Z", "older.jar", "fabric"),
				version("different-loader", "2025-03-01T00:00:00Z", "forge.jar", "forge"),
				version("latest", "2025-02-01T00:00:00Z", "latest.jar", "fabric"),
			})
		case "/download/latest.jar":
			_, _ = w.Write(mod)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New("")
	client.Modrinth = server.URL

	root := t.TempDir()
	mods := filepath.Join(root, "mods")
	if err := os.Mkdir(mods, 0755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(mods, "older.jar")
	if err := os.WriteFile(old, mod, 0644); err != nil {
		t.Fatal(err)
	}
	sum, err := fileHash(old)
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(map[string]Installed{
		"modrinth:example": {Provider: "modrinth", Project: "example", Version: "older", Filename: "older.jar", Hash: sum},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(filepath.Join(root, ".sintet-mods.json"), data, 0644); err != nil {
		t.Fatal(err)
	}
	manager := NewManager(client)
	if err = manager.RunSintedot(t.Context(), Target{ID: "build-example", Game: "1.21.1", Loader: "fabric", Directory: root}, "update", nil); err != nil {
		t.Fatal(err)
	}
	records, err := registry(root)
	if err != nil || records["modrinth:example"].Version != "latest" {
		t.Fatalf("incorrect version after update: %v, %v", records, err)
	}
	if _, err = os.Stat(old); !os.IsNotExist(err) {
		t.Fatalf("old file remains active: %v", err)
	}
}

func TestSintedotChecksCRCInsideReadableArchive(t *testing.T) {
	var buffer bytes.Buffer
	archive := zip.NewWriter(&buffer)
	entry, err := archive.CreateHeader(&zip.FileHeader{Name: "data", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("valid archive payload")
	if _, err = entry.Write(payload); err != nil {
		t.Fatal(err)
	}
	if err = archive.Close(); err != nil {
		t.Fatal(err)
	}
	content := buffer.Bytes()
	content[bytes.Index(content, payload)] ^= 1
	path := filepath.Join(t.TempDir(), "corrupt.jar")
	if err = os.WriteFile(path, content, 0644); err != nil {
		t.Fatal(err)
	}
	if _, err = inspectMod(path); err != nil {
		t.Fatalf("fixture's headers must remain readable: %v", err)
	}
	if err = verifyModArchive(t.Context(), path); err == nil {
		t.Fatal("tampered entry was accepted")
	}
}
