package workshop

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestSintetCatalogVersionHashAndResourceInstallation(t *testing.T) {
	data := resourceZip(t, true)
	sum := sha256.Sum256(data)
	var server *httptest.Server
	project := Project{ID: "native-pack", Provider: "sintedmods", Kind: "resources", Name: "Native pack", Games: []string{"1.21.1"}, Loaders: []string{}}
	version := func() map[string]any {
		return map[string]any{"id": "native-version", "project": "native-pack", "kind": "resources", "name": "1.0", "filename": "native-pack.zip", "games": []string{"1.21.1"}, "loaders": []string{}, "size": len(data), "url": server.URL + "/files/native-version", "hash": hex.EncodeToString(sum[:]), "algorithm": "sha256", "dependencies": []any{}}
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/projects":
			json.NewEncoder(w).Encode(SearchResult{Items: []Project{project}, Total: 1, NextOffset: 1})
		case "/projects/native-pack":
			json.NewEncoder(w).Encode(project)
		case "/projects/native-pack/versions":
			json.NewEncoder(w).Encode([]any{version()})
		case "/versions/native-version":
			json.NewEncoder(w).Encode(version())
		case "/files/native-version":
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := New("")
	client.Sintet = server.URL
	found, err := client.SearchFiltered(context.Background(), "sintedmods", "", 0, SearchFilters{Kind: "resources"})
	if err != nil || len(found.Items) != 1 {
		t.Fatal("native catalog", err)
	}
	files, err := client.ResourceVersions(context.Background(), "sintedmods", project.ID, "1.21.1")
	if err != nil || len(files) != 1 || files[0].URL == "" || files[0].Hash != hex.EncodeToString(sum[:]) {
		t.Fatal("native download metadata", err)
	}
	manager := NewManager(client)
	target := Target{ID: "fixture", Game: "1.21.1", Loader: "vanilla", Directory: t.TempDir()}
	plan, err := manager.PrepareResource(context.Background(), "sintedmods", project.ID, "native-version", target)
	if err != nil {
		t.Fatal(err)
	}
	if err = manager.Start(context.Background(), plan.ID, true); err != nil {
		t.Fatal(err)
	}
	state := waitInstall(t, manager)
	if state.Error != "" {
		t.Fatal(state.Error)
	}
	actual, err := os.ReadFile(filepath.Join(target.Directory, "resourcepacks", "native-pack.zip"))
	if err != nil || !bytes.Equal(actual, data) {
		t.Fatal("native resource pack not installed", err)
	}
}
