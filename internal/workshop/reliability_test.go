package workshop

import (
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestDownloadRecoversFromTemporaryFailures(t *testing.T) {
	jar := testJar(t, "retries")
	sum := sha512.Sum512(jar)
	for _, failure := range []string{"503", "429", "truncated", "corrupt"} {
		t.Run(failure, func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if requests.Add(1) == 1 {
					switch failure {
					case "503":
						w.WriteHeader(503)
					case "429":
						w.Header().Set("Retry-After", "0")
						w.WriteHeader(429)
					case "truncated":
						w.Header().Set("Content-Length", strconv.Itoa(len(jar)))
						w.Write(jar[:len(jar)/2])
					case "corrupt":
						bad := append([]byte(nil), jar...)
						bad[len(bad)/2] ^= 1
						w.Write(bad)
					}
					return
				}
				w.Write(jar)
			}))
			defer server.Close()
			v := Version{Kind: "packfile", Filename: "mods/retry.jar", URL: server.URL, Size: int64(len(jar)), Algorithm: "sha512", Hash: hex.EncodeToString(sum[:])}
			file := filepath.Join(t.TempDir(), "retry.jar")
			if _, err := NewManager(New("")).download(context.Background(), v, file, func(int64, int64) {}); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(file)
			if err != nil || !bytes.Equal(got, jar) || requests.Load() != 2 {
				t.Fatal("retry did not produce the original bytes", requests.Load(), err)
			}
		})
	}
}

func TestDownloadSwitchesToManifestMirrorWithoutLeakingCredentials(t *testing.T) {
	jar := testJar(t, "mirror")
	sum := sha512.Sum512(jar)
	var first, second atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-api-key") != "" || r.Header.Get("Authorization") != "" {
			t.Error("credentials sent to a download server")
		}
		if r.URL.Path == "/gone" {
			first.Add(1)
			w.WriteHeader(404)
			return
		}
		second.Add(1)
		w.Write(jar)
	}))
	defer server.Close()
	v := Version{Kind: "packfile", Filename: "mods/mirror.jar", URL: server.URL + "/gone", URLs: []string{server.URL + "/gone", "https://untrusted.invalid/evil.jar", server.URL + "/mirror"}, Size: int64(len(jar)), Algorithm: "sha512", Hash: hex.EncodeToString(sum[:])}
	_, err := NewManager(New("private-key")).download(context.Background(), v, filepath.Join(t.TempDir(), "mirror.jar"), func(int64, int64) {})
	if err != nil || first.Load() != 1 || second.Load() != 1 {
		t.Fatal("mirror not used", first.Load(), second.Load(), err)
	}
}

func TestRetryCancellationAndPermanentErrors(t *testing.T) {
	for _, status := range []int{403, 429} {
		t.Run(strconv.Itoa(status), func(t *testing.T) {
			var requests atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				w.Header().Set("Retry-After", "15")
				w.WriteHeader(status)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
			defer cancel()
			v := Version{Kind: "packfile", Filename: "mods/cancel.jar", URL: server.URL, Size: 1, Algorithm: "sha512", Hash: strings.Repeat("0", 128)}
			_, err := NewManager(New("")).download(ctx, v, filepath.Join(t.TempDir(), "cancel.jar"), func(int64, int64) {})
			if err == nil || requests.Load() != 1 || (status == 429 && !errors.Is(err, context.DeadlineExceeded)) {
				t.Fatal("incorrect retry or cancellation", requests.Load(), err)
			}
		})
	}
}

func TestCatalogMetadataRetriesTemporaryResponses(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if requests.Add(1) < 3 {
			w.WriteHeader(502)
			return
		}
		w.Write([]byte(`{"id":"fixture","title":"Recovered"}`))
	}))
	defer server.Close()
	client := New("")
	client.Modrinth = server.URL
	project, err := client.Project(context.Background(), "modrinth", "fixture")
	if err != nil || project.Name != "Recovered" || requests.Load() != 3 {
		t.Fatal("catalog request not recovered", project, requests.Load(), err)
	}
}

type countingTransport struct {
	requests atomic.Int32
}

func (c *countingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Path == "/mod.jar" {
		c.requests.Add(1)
	}
	return http.DefaultTransport.RoundTrip(r)
}

func TestPackReusesOnlyVerifiedLocalFiles(t *testing.T) {
	for _, corrupt := range []bool{false, true} {
		t.Run(strconv.FormatBool(corrupt), func(t *testing.T) {
			manager, _, _ := packFixture(t, false)
			plan, err := manager.PreparePack(context.Background(), "modrinth", "pack", "pack-v", t.TempDir())
			if err != nil {
				t.Fatal(err)
			}
			target := t.TempDir()
			if err := os.MkdirAll(filepath.Join(target, "mods"), 0700); err != nil {
				t.Fatal(err)
			}
			jar := testJar(t, "CatalogMod")
			if corrupt {
				jar[len(jar)/2] ^= 1
			}
			if err := os.WriteFile(filepath.Join(target, "mods", "example.jar"), jar, 0600); err != nil {
				t.Fatal(err)
			}
			counter := &countingTransport{}
			manager.Client.HTTP.Transport = counter
			if err := manager.installPack(context.Background(), plan, target); err != nil {
				t.Fatal(err)
			}
			wantRequests := int32(0)
			if corrupt {
				wantRequests = 1
			}
			got, err := os.ReadFile(filepath.Join(target, "mods", "example.jar"))
			if err != nil || !bytes.Equal(got, testJar(t, "CatalogMod")) || counter.requests.Load() != wantRequests {
				t.Fatal("incorrect verified reuse", counter.requests.Load(), err)
			}
		})
	}
}

func TestCurseForgePackResourceAndShaderPaths(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		class := 12
		if strings.HasSuffix(r.URL.Path, "/2") {
			class = 6552
		}
		id, _ := strconv.Atoi(filepath.Base(r.URL.Path))
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "classId": class}})
	}))
	defer server.Close()
	client := New("private-key")
	client.CurseForge = server.URL
	for project, want := range map[string]string{"1": "resourcepacks/fixture.zip", "2": "shaderpacks/fixture.zip"} {
		got, err := client.cursePackPath(context.Background(), Version{Project: project, Filename: "fixture.zip"})
		if err != nil || got != want {
			t.Fatal("incorrect CurseForge destination", got, err)
		}
	}
}

func TestPackRejectsFileDirectoryConflictsBeforeDownload(t *testing.T) {
	for _, files := range []map[string][]byte{
		{"overrides/custom": []byte("file"), "overrides/custom/settings.json": []byte("{}")},
		{"overrides/Custom/a.json": []byte("{}"), "overrides/custom/b.json": []byte("{}")},
	} {
		files["modrinth.index.json"] = []byte(`{"formatVersion":1,"game":"minecraft","dependencies":{"minecraft":"1.21.1"},"files":[]}`)
		archive := filepath.Join(t.TempDir(), "conflict.zip")
		if err := os.WriteFile(archive, packZIP(t, files), 0600); err != nil {
			t.Fatal(err)
		}
		manager := NewManager(New(""))
		if err := manager.parsePack(context.Background(), archive, &PackPlan{Project: Project{Provider: "modrinth"}}); err == nil {
			t.Fatal("conflicting layout accepted")
		}
	}
}

func TestCurseForgeMetadataPoolIsBoundedAndPreservesOrder(t *testing.T) {
	var active, peak atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := active.Add(1)
		defer active.Add(-1)
		for old := peak.Load(); n > old && !peak.CompareAndSwap(old, n); old = peak.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		id, _ := strconv.Atoi(filepath.Base(r.URL.Path))
		json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": id, "modId": 42, "fileName": strconv.Itoa(id) + ".jar", "downloadUrl": "https://edge.forgecdn.net/files/1/2/fixture.jar", "fileLength": 1, "hashes": []any{map[string]any{"algo": 1, "value": strings.Repeat("0", 40)}}}})
	}))
	defer server.Close()
	client := New("private-key")
	client.CurseForge = server.URL
	refs := []cursePackReference{}
	for i := 1; i <= 12; i++ {
		refs = append(refs, cursePackReference{Project: 42, File: i, Required: true})
	}
	files, err := client.cursePackFiles(context.Background(), refs)
	if err != nil || len(files) != len(refs) || peak.Load() < 2 || peak.Load() > 4 {
		t.Fatal("metadata pool failed", len(files), peak.Load(), err)
	}
	for i, v := range files {
		if v.ID != strconv.Itoa(i+1) || v.Filename != "mods/"+strconv.Itoa(i+1)+".jar" {
			t.Fatal("metadata order changed", i, v)
		}
	}
}

func TestPackPreservesMirrorsAndNormalizesKnownDirectories(t *testing.T) {
	jar := testJar(t, "mirrors")
	sum := sha512.Sum512(jar)
	index, _ := json.Marshal(map[string]any{
		"formatVersion": 1, "game": "minecraft", "dependencies": map[string]string{"minecraft": "1.21.1"},
		"files": []any{map[string]any{"path": "Mods/fixture.jar", "fileSize": len(jar), "hashes": map[string]string{"sha512": hex.EncodeToString(sum[:])}, "downloads": []string{"https://cdn.modrinth.com/missing.jar", "https://github.com/example/project/releases/download/test/mod.jar"}}},
	})
	archive := filepath.Join(t.TempDir(), "pack.zip")
	if err := os.WriteFile(archive, packZIP(t, map[string][]byte{"modrinth.index.json": index, "overrides/MODS/README.txt": []byte("documentation")}), 0600); err != nil {
		t.Fatal(err)
	}
	plan := PackPlan{Project: Project{Provider: "modrinth"}}
	if err := NewManager(New("")).parsePack(context.Background(), archive, &plan); err != nil {
		t.Fatal(err)
	}
	var found bool
	for _, entry := range plan.entries {
		if entry.Path == "mods/fixture.jar" {
			found = len(entry.File.URLs) == 2 && entry.File.Filename == entry.Path
		}
	}
	if !found || plan.Files != 2 {
		t.Fatal("mirrors or normalized directory lost", plan.entries)
	}
}

func TestModrinthEquivalentRequiresExactOriginalHash(t *testing.T) {
	for _, match := range []bool{false, true} {
		t.Run(strconv.FormatBool(match), func(t *testing.T) {
			original := strings.Repeat("a", 40)
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("x-api-key") != "" {
					t.Error("CurseForge key leaked to Modrinth")
				}
				got := strings.Repeat("b", 40)
				if match {
					got = original
				}
				json.NewEncoder(w).Encode(map[string]any{"id": "mirror", "project_id": "different-project", "files": []any{map[string]any{"url": "https://cdn.modrinth.com/data/test/mod.jar", "hashes": map[string]string{"sha1": got}}}})
			}))
			defer server.Close()
			client := New("private-key")
			client.Modrinth = server.URL
			v := Version{Project: "42", ID: "99", Filename: "original.jar", Algorithm: "sha1", Hash: original}
			got, found, err := client.modrinthEquivalent(context.Background(), v)
			if err != nil || found != match || got.Project != v.Project || got.ID != v.ID || got.Hash != original || got.Filename != v.Filename {
				t.Fatal("mirror changed identity or ignored hash", got, found, err)
			}
			if match && !strings.HasPrefix(got.URL, "https://cdn.modrinth.com/") {
				t.Fatal("exact mirror not applied", got)
			}
		})
	}
}

func TestPackCanDownloadVerifiedEmptyConfig(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	sum := sha512.Sum512(nil)
	v := Version{Kind: "packfile", Filename: "config/empty.cfg", URL: server.URL, Size: 0, Algorithm: "sha512", Hash: hex.EncodeToString(sum[:])}
	file := filepath.Join(t.TempDir(), "empty.cfg")
	if _, err := NewManager(New("")).download(context.Background(), v, file, func(int64, int64) {}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(file)
	if err != nil || info.Size() != 0 {
		t.Fatal("verified empty config not installed", err)
	}
}
