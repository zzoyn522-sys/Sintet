package packsync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (Snapshot, string) {
	t.Helper()
	root := t.TempDir()
	cache := t.TempDir()
	for file, content := range map[string]string{"mods/example.jar": "PK\x03\x04example jar", "mods/disabled.jar.disabled": "disabled", "config/test/client.toml": "enabled=true", "config/accounts.json": "must not leave host", "config/plugin.js": "do not execute", "saves/private/level.dat": "world data", "settings.json": "private"} {
		path := filepath.Join(root, filepath.FromSlash(file))
		os.MkdirAll(filepath.Dir(path), 0700)
		if err := os.WriteFile(path, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}
	snapshot, err := Scan(context.Background(), root, Manifest{Name: "Test pack", Game: "1.21.1", Loader: "fabric", LoaderVersion: "0.16.14"}, cache)
	if err != nil {
		t.Fatal(err)
	}
	return snapshot, root
}
func TestSnapshotIsImmutableAndContainsOnlyExplicitlySharedFiles(t *testing.T) {
	snapshot, root := fixture(t)
	if len(snapshot.Manifest.Files) != 2 {
		t.Fatal(snapshot.Manifest.Files)
	}
	original := snapshot.Manifest.Files[1]
	os.WriteFile(filepath.Join(root, "mods", "example.jar"), []byte("changed after room opened"), 0600)
	server := httptest.NewServer(snapshot.Handler())
	defer server.Close()
	r, err := http.Get(server.URL + "/file/" + original.SHA256)
	if err != nil {
		t.Fatal(err)
	}
	data, _ := io.ReadAll(r.Body)
	r.Body.Close()
	if string(data) != "PK\x03\x04example jar" {
		t.Fatal("snapshot changed", string(data))
	}
	for _, path := range []string{"/file/../../settings.json", "/file/" + strings.Repeat("a", 64), "/settings.json"} {
		r, err = http.Get(server.URL + path)
		if err != nil {
			t.Fatal(err)
		}
		r.Body.Close()
		if r.StatusCode == 200 {
			t.Fatal("unshared path exposed", path)
		}
	}
	if !ValidCached(snapshot.Cache, original) {
		t.Fatal("snapshot hash invalid")
	}
}
func TestManifestRejectsTraversalDevicesScriptsDuplicateNamesAndOversize(t *testing.T) {
	snapshot, _ := fixture(t)
	for _, path := range []string{"../mods/a.jar", "mods/../../a.jar", "/mods/a.jar", `mods\a.jar`, "config/a.json:stream", "mods/CON.jar", "mods/COM¹.jar", "mods/NUL .jar", "config/../x.json", "config/a.ps1", "config/tokens.json", "mods/folder/a.jar", "config/. /a.json", "config/a.json.", "config/a.json ", "mods/a.exe"} {
		if AllowedPath(path) {
			t.Fatalf("accepted unsafe path %q", path)
		}
	}
	for _, mutate := range []func(*Manifest){func(m *Manifest) { m.Files[0].Size = MaxMod + 1 }, func(m *Manifest) { m.Files = append(m.Files, m.Files[0]) }, func(m *Manifest) { m.Game = ".." }, func(m *Manifest) { m.Files[0].Path = "config/secret.json" }, func(m *Manifest) { m.Files[0].SHA256 = strings.Repeat("z", 64) }} {
		m := snapshot.Manifest
		m.Files = append([]File{}, m.Files...)
		mutate(&m)
		m.Digest = fingerprint(m)
		if Validate(m) == nil {
			t.Fatal("unsafe manifest accepted", m)
		}
	}
	m := snapshot.Manifest
	m.Name = "Renamed only"
	if Validate(m) != nil {
		t.Fatal("renaming should not force re-download")
	}
}
func TestHashMismatchAndCancellationNeverCommitDownloadedFile(t *testing.T) {
	snapshot, _ := fixture(t)
	file := snapshot.Manifest.Files[0]
	cache := t.TempDir()
	if CopyVerified(context.Background(), strings.NewReader("forged"), cache, file, nil) == nil {
		t.Fatal("accepted forged bytes")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if CopyVerified(ctx, strings.NewReader("unused"), cache, file, nil) == nil {
		t.Fatal("ignored cancellation")
	}
	entries, _ := os.ReadDir(cache)
	if len(entries) != 0 {
		t.Fatal("partial download retained", entries)
	}
}

type httpPeer struct {
	base     string
	requests int
}

func (p *httpPeer) PackGet(ctx context.Context, path string) (*http.Response, error) {
	p.requests++
	r, _ := http.NewRequestWithContext(ctx, "GET", p.base+path, nil)
	return http.DefaultClient.Do(r)
}
func TestDownloadReusesVerifiedFilesAndPopulatesOnlyANewInstance(t *testing.T) {
	snapshot, root := fixture(t)
	m := snapshot.Manifest
	for i := range m.Files {
		m.Files[i].SHA1 = ""
	}
	m.Digest = fingerprint(m)
	snapshot.Manifest = m
	server := httptest.NewServer(snapshot.Handler())
	defer server.Close()
	peer := &httpPeer{base: server.URL}
	cache := t.TempDir()
	local := map[string]string{m.Files[0].SHA256: filepath.Join(root, filepath.FromSlash(m.Files[0].Path))}
	if err := Download(context.Background(), m, cache, peer, local, nil); err != nil {
		t.Fatal(err)
	}
	if peer.requests != 1 {
		t.Fatal("downloaded a locally available file", peer.requests)
	}
	dest := filepath.Join(t.TempDir(), "new-instance")
	if err := Populate(context.Background(), m, cache, dest); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "config", "accounts.json")); !os.IsNotExist(err) {
		t.Fatal("leaked private config")
	}
	file := filepath.Join(dest, "mods", "example.jar")
	os.WriteFile(file, []byte("user changes"), 0600)
	if Populate(context.Background(), m, cache, dest) == nil {
		t.Fatal("overwrote an existing instance")
	}
	data, _ := os.ReadFile(file)
	if string(data) != "user changes" {
		t.Fatal("user mod overwritten")
	}
	before := peer.requests
	if err := Download(context.Background(), m, cache, peer, local, nil); err != nil {
		t.Fatal(err)
	}
	if peer.requests != before {
		t.Fatal("verified cache not reused")
	}
	// Extra fields must not be able to smuggle arbitrary download URLs or JVM flags.
	raw, _ := json.Marshal(m)
	raw = bytes.Replace(raw, []byte(`"format":`), []byte(`"jvmArgs":"-javaagent:evil","format":`), 1)
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(raw) }))
	defer bad.Close()
	if _, err := ReadManifest(context.Background(), &httpPeer{base: bad.URL}); err == nil {
		t.Fatal("accepted executable launch parameters")
	}
}
