package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestMRPackExportAndLocalImport(t *testing.T) {
	ctx := context.Background()
	root := t.TempDir()
	catalogMod := packZIP(t, map[string][]byte{"fabric.mod.json": []byte(`{"id":"demo","version":"1.0"}`)})
	for name, data := range map[string]string{
		"mods/local.jar": "local mod", "mods/catalog.jar": string(catalogMod),
		"mods/off.jar.disabled": "disabled mod", "config/game.toml": "enabled = true",
		"shaderpacks/sample.zip": "shader", "saves/private/level.dat": "my world",
		"options.txt": "private local settings", "logs/latest.log": "private log",
	} {
		file := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(file, []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	digest := sha512.Sum512(catalogMod)
	digestString := hex.EncodeToString(digest[:])
	const modURL = "https://cdn.modrinth.com/data/fixture/versions/one/catalog.jar"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && r.URL.Path == "/version_files" {
			var request struct {
				Algorithm string
				Hashes    []string
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil || request.Algorithm != "sha512" || len(request.Hashes) != 3 {
				http.Error(w, "wrong payload", http.StatusBadRequest)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{digestString: map[string]any{
				"files": []any{map[string]any{"url": modURL, "hashes": map[string]string{"sha512": digestString}}},
			}})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	client := New("")
	client.Modrinth = srv.URL
	httpClient := *client.HTTP
	httpClient.Transport = mrpackTestTransport(func(r *http.Request) (*http.Response, error) {
		if r.URL.String() == modURL {
			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(catalogMod)), ContentLength: int64(len(catalogMod))}, nil
		}
		return http.DefaultTransport.RoundTrip(r)
	})
	client.HTTP = &httpClient
	manager := NewManager(client)
	destination := filepath.Join(t.TempDir(), "roundtrip.mrpack")
	if err := manager.ExportPack(ctx, root, destination, "Round trip", "1.21.1", "forge", "1.21.1-52.1.0"); err != nil {
		t.Fatal(err)
	}
	archive, err := zip.OpenReader(destination)
	if err != nil {
		t.Fatal(err)
	}
	data, err := zipBytes(&archive.Reader, "modrinth.index.json", 4<<20)
	archive.Close()
	if err != nil {
		t.Fatal(err)
	}
	var index mrpackIndex
	if err := json.Unmarshal(data, &index); err != nil {
		t.Fatal(err)
	}
	if index.Format != 1 || index.Name != "Round trip" || index.VersionID == "" || index.Dependencies["forge"] != "52.1.0" ||
		len(index.Files) != 1 || index.Files[0].Path != "mods/catalog.jar" || len(index.Files[0].Hashes["sha1"]) != 40 || index.Files[0].Hashes["sha512"] != digestString {
		t.Fatalf("unexpected index: %+v", index)
	}
	file, err := os.Open(destination)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := manager.PrepareLocalPack(ctx, file, "Fallback", t.TempDir())
	file.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !plan.LocalArchive || plan.Project.Name != "Round trip" || plan.Files != 4 || plan.LoaderVersion != "1.21.1-52.1.0" {
		t.Fatalf("unexpected plan: %+v", plan)
	}
	target := t.TempDir()
	if err := manager.StartPack(ctx, plan.ID, Target{Directory: target, Game: plan.Game, Loader: plan.Loader}, nil); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for manager.State().Busy && time.Now().Before(deadline) {
		time.Sleep(10 * time.Millisecond)
	}
	if state := manager.State(); state.Busy || state.Error != "" {
		t.Fatalf("pack import failed: %+v", state)
	}
	for name, expected := range map[string]string{
		"mods/catalog.jar": string(catalogMod), "mods/local.jar": "local mod",
		"config/game.toml": "enabled = true", "shaderpacks/sample.zip": "shader",
	} {
		if data, err := os.ReadFile(filepath.Join(target, name)); err != nil || string(data) != expected {
			t.Fatalf("wrong content at %s: %q, %v", name, data, err)
		}
	}
	for _, name := range []string{"saves", "options.txt", "logs", "mods/off.jar.disabled"} {
		if _, err := os.Stat(filepath.Join(target, name)); !os.IsNotExist(err) {
			t.Fatalf("unexpected export of %s", name)
		}
	}
}

type mrpackTestTransport func(*http.Request) (*http.Response, error)

func (f mrpackTestTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}
