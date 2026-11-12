package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sintet/internal/identity"
	"testing"
	"time"
)

func TestLiveResourceCategories(t *testing.T) {
	if os.Getenv("SINTET_WORKSHOP_INTEGRATION") != "1" {
		t.Skip("live resource pack taxonomy")
	}
	key, e := identity.ReadServiceSecret(os.Getenv("SINTET_CURSEFORGE_TEST_KEY_FILE"))
	if e != nil {
		t.Fatal(e)
	}
	c := New(key)
	var r struct {
		Data []struct {
			ID         int
			Slug, Name string
		}
	}
	if e = c.get(context.Background(), "curseforge", "/categories?gameId=432&classId=12", &r); e != nil {
		t.Fatal(e)
	}
	if len(r.Data) == 0 {
		t.Fatal("missing resource pack categories")
	}
	for _, topic := range ResourceTopics {
		if topic.CurseForge != "" {
			if _, e = c.curseCategoryFor(context.Background(), topic.CurseForge, "12"); e != nil {
				t.Fatal(topic.ID, e)
			}
		}
	}
	for _, provider := range []string{"modrinth", "curseforge"} {
		t.Run(provider, func(t *testing.T) {
			result, e := c.SearchFiltered(context.Background(), provider, "Faithful", 0, SearchFilters{Kind: "resources", Game: "1.21.1", Resolution: "32x", Sort: "relevance"})
			if e != nil || len(result.Items) == 0 {
				t.Fatal("resource search failed", e)
			}
			combined, e := c.SearchFiltered(context.Background(), provider, "", 0, SearchFilters{Kind: "resources", Game: "1.21.1", Resolution: "32x", Topic: "vanilla", Sort: "downloads"})
			if e != nil || len(combined.Items) == 0 {
				t.Fatal("combined resource filters failed", e)
			}
			for _, item := range combined.Items {
				if provider == "modrinth" && (!contains(item.Tags, "32x") || !contains(item.Tags, "vanilla-like")) {
					t.Fatal("Modrinth ignored resource filters")
				}
				if provider == "curseforge" && (!contains(item.Tags, "thirty-two-x") || !contains(item.Tags, "traditional")) {
					t.Fatal("CurseForge ignored resource filters")
				}
			}
			project := result.Items[0]
			if provider == "curseforge" {
				found := false
				for _, candidate := range result.Items {
					files, err := c.ResourceVersions(context.Background(), provider, candidate.ID, "1.21.1")
					if err != nil {
						t.Fatal(err)
					}
					for _, file := range files {
						if file.URL != "" {
							project = candidate
							found = true
							break
						}
					}
					if found {
						break
					}
				}
				if !found {
					t.Fatal("no downloadable CurseForge pack among search results")
				}
			}
			if project.Kind != "resources" || project.Preview == "" {
				t.Fatal("missing project type or preview")
			}
			files, e := c.ResourceVersions(context.Background(), provider, project.ID, "1.21.1")
			if e != nil || len(files) == 0 {
				t.Fatal("pack version lookup failed", e)
			}
			target := Target{ID: "vanilla-test", Game: "1.21.1", Loader: "vanilla", Directory: t.TempDir()}
			m := NewManager(c)
			plan, e := m.PrepareResource(context.Background(), provider, project.ID, files[0].ID, target)
			if e != nil {
				t.Fatal(e)
			}
			if e = m.Start(context.Background(), plan.ID, true); e != nil {
				t.Fatal(e)
			}
			deadline := time.Now().Add(2 * time.Minute)
			for m.State().Busy && time.Now().Before(deadline) {
				time.Sleep(100 * time.Millisecond)
			}
			if s := m.State(); s.Busy || s.Error != "" {
				t.Fatal("resource install failed", s.Error)
			}
			items, e := Scan(target.Directory, "resources")
			if e != nil || len(items) == 0 || items[0].Provider != provider {
				t.Fatal("installed pack missing from local workshop", e)
			}
			if _, e = os.Stat(filepath.Join(target.Directory, "mods", files[0].Filename)); !os.IsNotExist(e) {
				t.Fatal("pack installed into mods")
			}
			t.Logf("Verified filtered search, preview, ZIP checksums, Vanilla target and %d installed packs", len(items))
		})
	}
}

func resourceZip(t *testing.T, valid bool) []byte {
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	if valid {
		meta, _ := z.Create("pack.mcmeta")
		meta.Write([]byte(`{"pack":{"pack_format":34,"description":"Test pack"}}`))
		asset, _ := z.Create("assets/minecraft/textures/test.txt")
		asset.Write([]byte("test"))
	} else {
		asset, _ := z.Create("not-a-pack.txt")
		asset.Write([]byte("test"))
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return buffer.Bytes()
}
func resourceFixture(t *testing.T, valid bool) (*Manager, func()) {
	data := resourceZip(t, valid)
	sum := sha512.Sum512(data)
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/project/pack":
			w.Write([]byte(`{"id":"pack","title":"Pack","project_type":"resourcepack","game_versions":["1.21.1"]}`))
		case "/version/v1":
			json.NewEncoder(w).Encode(map[string]any{"id": "v1", "project_id": "pack", "name": "Pack 1", "game_versions": []string{"1.21.1"}, "files": []any{map[string]any{"filename": "pack.zip", "url": server.URL + "/download", "size": len(data), "primary": true, "hashes": map[string]string{"sha512": hex.EncodeToString(sum[:])}}}})
		case "/download":
			w.Write(data)
		default:
			http.NotFound(w, r)
		}
	}))
	c := New("")
	c.Modrinth = server.URL
	return NewManager(c), server.Close
}
func TestResourceInstallAndInvalidZipPreserveModsAndExistingFiles(t *testing.T) {
	for _, valid := range []bool{true, false} {
		t.Run(map[bool]string{true: "valid", false: "invalid"}[valid], func(t *testing.T) {
			m, close := resourceFixture(t, valid)
			defer close()
			target := Target{Game: "1.21.1", Loader: "vanilla", Directory: t.TempDir()}
			os.MkdirAll(filepath.Join(target.Directory, "mods"), 0755)
			original := filepath.Join(target.Directory, "mods", "existing.jar")
			os.WriteFile(original, []byte("unchanged"), 0600)
			plan, e := m.PrepareResource(context.Background(), "modrinth", "pack", "v1", target)
			if e != nil {
				t.Fatal(e)
			}
			if e = m.Start(context.Background(), plan.ID, true); e != nil {
				t.Fatal(e)
			}
			state := waitInstall(t, m)
			_, packErr := os.Stat(filepath.Join(target.Directory, "resourcepacks", "pack.zip"))
			if valid {
				if state.Error != "" || packErr != nil {
					t.Fatal("valid pack not installed", state.Error)
				}
				again, e := m.PrepareResource(context.Background(), "modrinth", "pack", "v1", target)
				if e != nil || !again.Entries[0].Installed {
					t.Fatal("installed pack not detected", e)
				}
			} else if state.Error == "" || !os.IsNotExist(packErr) {
				t.Fatal("invalid ZIP was installed")
			}
			data, _ := os.ReadFile(original)
			if string(data) != "unchanged" {
				t.Fatal("mod was changed")
			}
		})
	}
}
