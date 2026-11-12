package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sintet/internal/identity"
)

func testJar(t *testing.T, name string) []byte {
	t.Helper()
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	file, _ := writer.Create("fabric.mod.json")
	json.NewEncoder(file).Encode(map[string]string{"id": name, "name": name, "version": "1.0", "description": "Test mod"})
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return buffer.Bytes()
}
func fixture(t *testing.T) (*Manager, *httptest.Server, *bool) {
	t.Helper()
	root, dep := testJar(t, "MainMod"), testJar(t, "Dependency")
	corrupt := false
	var server *httptest.Server
	version := func(id, project, name string, data []byte, dependencies []map[string]string) map[string]any {
		sum := sha512.Sum512(data)
		return map[string]any{"id": id, "project_id": project, "name": id, "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}, "dependencies": dependencies, "files": []any{map[string]any{"filename": name, "url": server.URL + "/download/" + name, "size": len(data), "primary": true, "hashes": map[string]string{"sha512": hex.EncodeToString(sum[:])}}}}
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" {
			t.Error("secret attached to Modrinth or download request")
		}
		switch r.URL.Path {
		case "/project/root", "/project/dep":
			id := filepath.Base(r.URL.Path)
			json.NewEncoder(w).Encode(map[string]any{"id": id, "title": id, "slug": id, "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}})
		case "/version/root-v":
			json.NewEncoder(w).Encode(version("root-v", "root", "root.jar", root, []map[string]string{{"project_id": "dep", "version_id": "dep-v", "dependency_type": "required"}}))
		case "/version/dep-v":
			json.NewEncoder(w).Encode(version("dep-v", "dep", "dep.jar", dep, nil))
		case "/download/root.jar":
			w.Write(root)
		case "/download/dep.jar":
			if corrupt {
				w.Write([]byte("bad file"))
			} else {
				w.Write(dep)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := New("private-test-key")
	client.Modrinth = server.URL
	return NewManager(client), server, &corrupt
}
func waitInstall(t *testing.T, m *Manager) InstallState {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state := m.State()
		if !state.Busy {
			return state
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("installation did not finish")
	return InstallState{}
}
func TestRequiredDependenciesAndRealLocalFiles(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "test", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
	plan, err := manager.Prepare(context.Background(), "modrinth", "root", "root-v", target)
	if err != nil || len(plan.Entries) != 2 {
		t.Fatal("dependency plan failed", err)
	}
	if err = manager.Start(context.Background(), plan.ID, true); err != nil {
		t.Fatal(err)
	}
	if state := waitInstall(t, manager); state.Error != "" {
		t.Fatal(state.Error)
	}
	items, err := Scan(target.Directory, "mods")
	if err != nil || len(items) != 2 {
		t.Fatal("local mods missing", err)
	}
	for _, item := range items {
		if item.Version != "1.0" || item.Provider != "modrinth" || !item.Enabled {
			t.Fatal("JAR metadata or registry not applied")
		}
	}
	if err = Toggle(target.Directory, "root.jar", false); err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(target.Directory, "mods", "root.jar.disabled")); err != nil {
		t.Fatal("disable did not persist")
	}
	plan, err = manager.Prepare(context.Background(), "modrinth", "root", "root-v", target)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Entries[0].Installed || !plan.Entries[1].Installed {
		t.Fatal("disabled root or installed dependency detected incorrectly")
	}
	if err = manager.Start(context.Background(), plan.ID, true); err != nil {
		t.Fatal(err)
	}
	if state := waitInstall(t, manager); state.Error != "" {
		t.Fatal(state.Error)
	}
	if _, err = os.Stat(filepath.Join(target.Directory, "mods", "root.jar")); err != nil {
		t.Fatal("explicit install did not re-enable the mod")
	}
}
func TestDecliningDependenciesAndCorruptDownloadRollback(t *testing.T) {
	for _, withDependencies := range []bool{false, true} {
		t.Run(map[bool]string{false: "declined", true: "corrupt dependency"}[withDependencies], func(t *testing.T) {
			m, _, corrupt := fixture(t)
			target := Target{ID: "test", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
			plan, err := m.Prepare(context.Background(), "modrinth", "root", "root-v", target)
			if err != nil {
				t.Fatal(err)
			}
			*corrupt = true
			if err = m.Start(context.Background(), plan.ID, withDependencies); err != nil {
				t.Fatal(err)
			}
			state := waitInstall(t, m)
			_, rootErr := os.Stat(filepath.Join(target.Directory, "mods", "root.jar"))
			_, depErr := os.Stat(filepath.Join(target.Directory, "mods", "dep.jar"))
			if withDependencies {
				if state.Error == "" || !os.IsNotExist(rootErr) || !os.IsNotExist(depErr) {
					t.Fatal("corrupt batch changed live mods")
				}
			} else {
				if state.Error != "" || rootErr != nil || !os.IsNotExist(depErr) {
					t.Fatal("declining dependencies did not install exactly one mod", state.Error)
				}
			}
		})
	}
}
func TestUnmanagedCollisionAndUnsafeInputsLeaveFilesIntact(t *testing.T) {
	m, _, _ := fixture(t)
	target := Target{ID: "test", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
	os.MkdirAll(filepath.Join(target.Directory, "mods"), 0755)
	path := filepath.Join(target.Directory, "mods", "dep.jar")
	os.WriteFile(path, []byte("user file"), 0600)
	plan, err := m.Prepare(context.Background(), "modrinth", "root", "root-v", target)
	if err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background(), plan.ID, true)
	if waitInstall(t, m).Error == "" {
		t.Fatal("overwrote an unmanaged file")
	}
	data, _ := os.ReadFile(path)
	if string(data) != "user file" {
		t.Fatal("original file lost")
	}
	if _, err = os.Stat(filepath.Join(target.Directory, "mods", "root.jar")); !os.IsNotExist(err) {
		t.Fatal("failed batch left a partial installation")
	}
	for _, name := range []string{"../bad.jar", `C:\bad.jar`, "bad.jar:stream", "bad.exe"} {
		if safeFilename(name) {
			t.Fatal("unsafe filename accepted")
		}
	}
	if allowedDownload("https://unrelated.test/mod.jar") {
		t.Fatal("untrusted download host accepted")
	}
	target.Loader = "forge"
	if _, err = m.Prepare(context.Background(), "modrinth", "root", "root-v", target); err == nil {
		t.Fatal("incompatible loader accepted")
	}
}

func TestDownloadUsesVerifiedDigestWithStaleCatalogSize(t *testing.T) {
	jar := testJar(t, "size-fixture")
	sum := sha512.Sum512(jar)
	want := sha256.Sum256(jar)
	for _, chunked := range []bool{false, true} {
		for _, delta := range []int{-32, -1, 0, 1, 64} {
			t.Run(strconv.FormatBool(chunked)+"/"+strconv.Itoa(delta), func(t *testing.T) {
				server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if chunked {
						w.(http.Flusher).Flush()
					}
					w.Write(jar)
				}))
				defer server.Close()
				manager := NewManager(New(""))
				v := Version{Kind: "packfile", Filename: "mods/fixture.jar", URL: server.URL, Size: int64(len(jar) + delta), Algorithm: "sha512", Hash: hex.EncodeToString(sum[:])}
				file := filepath.Join(t.TempDir(), "fixture.jar")
				var received, total int64
				got, err := manager.download(context.Background(), v, file, func(n, size int64) { received, total = n, size })
				if err != nil || got != hex.EncodeToString(want[:]) {
					t.Fatal("valid digest rejected for stale size", delta, got, err)
				}
				actual, err := os.ReadFile(file)
				if err != nil || !bytes.Equal(actual, jar) || received != int64(len(jar)) {
					t.Fatal("download was truncated", received, err)
				}
				if !chunked && total != int64(len(jar)) {
					t.Fatal("progress should use actual HTTP content length", total)
				}
			})
		}
	}
}

func TestDownloadRejectsCorruptionEvenWithMatchingSizeAndRemovesPartialFile(t *testing.T) {
	jar := testJar(t, "corrupt-fixture")
	sum := sha512.Sum512(jar)
	corrupt := append([]byte(nil), jar...)
	corrupt[len(corrupt)/2] ^= 1
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write(corrupt) }))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "fixture.jar")
	v := Version{Kind: "packfile", Filename: "mods/fixture.jar", URL: server.URL, Size: int64(len(jar)), Algorithm: "sha512", Hash: hex.EncodeToString(sum[:])}
	_, err := NewManager(New("")).download(context.Background(), v, file, func(int64, int64) {})
	if err == nil || !strings.Contains(err.Error(), "контрольная сумма") {
		t.Fatal("corrupt JAR was not rejected", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("unverified file left behind", err)
	}
}

func TestDownloadRejectsOversizedHTTPBodyBeforeCreatingFile(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", strconv.FormatInt((1<<30)+1, 10))
	}))
	defer server.Close()
	file := filepath.Join(t.TempDir(), "fixture.jar")
	v := Version{Kind: "packfile", Filename: "mods/fixture.jar", URL: server.URL, Size: 1, Algorithm: "sha512", Hash: strings.Repeat("0", 128)}
	_, err := NewManager(New("")).download(context.Background(), v, file, func(int64, int64) {})
	if err == nil || !strings.Contains(err.Error(), "лимит") {
		t.Fatal("oversized response accepted", err)
	}
	if _, err := os.Stat(file); !os.IsNotExist(err) {
		t.Fatal("oversized download created a file", err)
	}
}

func TestLiveCatalogsAndDownloads(t *testing.T) {
	if os.Getenv("SINTET_WORKSHOP_INTEGRATION") != "1" {
		t.Skip("opt-in live catalog and CDN verification")
	}
	key, err := identity.ReadServiceSecret(os.Getenv("SINTET_CURSEFORGE_TEST_KEY_FILE"))
	if err != nil {
		t.Fatal("could not read encrypted CurseForge test key")
	}
	client := New(key)
	for _, provider := range []string{"modrinth", "curseforge"} {
		t.Run(provider, func(t *testing.T) {
			result, err := client.Search(context.Background(), provider, "Iris", 0)
			if err != nil || len(result.Items) == 0 {
				t.Fatal("search failed", err)
			}
			var project Project
			for _, candidate := range result.Items {
				if candidate.Name == "Iris Shaders" || candidate.Name == "Iris" {
					project = candidate
					break
				}
			}
			if project.ID == "" {
				project = result.Items[0]
			}
			details, err := client.Project(context.Background(), provider, project.ID)
			if err != nil {
				t.Fatal(err)
			}
			if len(details.Games) == 0 || len(details.Loaders) == 0 {
				t.Fatal("missing version or loader metadata")
			}
			versions, err := client.Versions(context.Background(), provider, project.ID, "1.21.1", "fabric")
			if err != nil || len(versions) == 0 {
				t.Fatal("version selection failed", err)
			}
			manager := NewManager(client)
			target := Target{ID: "live-test", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
			plan, err := manager.Prepare(context.Background(), provider, project.ID, versions[0].ID, target)
			if err != nil {
				t.Fatal("dependency resolution failed", err)
			}
			if err = manager.Start(context.Background(), plan.ID, true); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(2 * time.Minute)
			for manager.State().Busy && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if state := manager.State(); state.Busy || state.Error != "" {
				t.Fatal("CDN install failed", state.Error)
			}
			items, err := Scan(target.Directory, "mods")
			if err != nil || len(items) == 0 {
				t.Fatal("downloaded JARs not recognized", err)
			}
			t.Logf("Verified search, compatible versions, %d required files, checksums and local JAR metadata", len(items))
		})
	}
}
