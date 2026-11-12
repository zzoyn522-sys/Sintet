package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

func packZIP(t *testing.T, files map[string][]byte) []byte {
	t.Helper()
	var buffer bytes.Buffer
	z := zip.NewWriter(&buffer)
	names := []string{}
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		data := files[name]
		w, e := z.Create(name)
		if e != nil {
			t.Fatal(e)
		}
		w.Write(data)
	}
	if e := z.Close(); e != nil {
		t.Fatal(e)
	}
	return buffer.Bytes()
}
func packFixture(t *testing.T, cf bool) (*Manager, *httptest.Server, *bool) {
	t.Helper()
	bad := false
	jar := testJar(t, "CatalogMod")
	sum := sha512.Sum512(jar)
	sha := sha1.Sum(jar)
	var server *httptest.Server
	makePack := func() []byte {
		if cf {
			manifest, _ := json.Marshal(map[string]any{"manifestType": "minecraftModpack", "manifestVersion": 1, "minecraft": map[string]any{"version": "1.20.1", "modLoaders": []any{map[string]any{"id": "forge-47.4.23", "primary": true}}}, "files": []any{map[string]any{"projectID": 42, "fileID": 99, "required": true}}, "overrides": "overrides"})
			return packZIP(t, map[string][]byte{"manifest.json": manifest, "overrides/config/test.toml": []byte("from=curseforge")})
		}
		index, _ := json.Marshal(map[string]any{"formatVersion": 1, "game": "minecraft", "dependencies": map[string]string{"minecraft": "1.21.1", "fabric-loader": "0.16.14"}, "files": []any{map[string]any{"path": "mods/example.jar", "hashes": map[string]string{"sha512": hex.EncodeToString(sum[:]), "sha1": hex.EncodeToString(sha[:])}, "downloads": []string{server.URL + "/mod.jar"}, "fileSize": len(jar)}, map[string]any{"path": "mods/server.jar", "env": map[string]string{"client": "unsupported"}}}})
		return packZIP(t, map[string][]byte{"modrinth.index.json": index, "overrides/config/test.toml": []byte("from=common"), "client-overrides/config/test.toml": []byte("from=client"), "server-overrides/config/test.toml": []byte("from=server"), "overrides/options.txt": []byte("language:ru_ru")})
	}
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ".jar") || strings.HasSuffix(r.URL.Path, ".mrpack") || strings.HasSuffix(r.URL.Path, ".zip") {
			if r.Header.Get("x-api-key") != "" {
				t.Error("CurseForge key leaked to download host")
			}
		}
		switch r.URL.Path {
		case "/project/pack":
			json.NewEncoder(w).Encode(map[string]any{"id": "pack", "title": "Fixture pack", "project_type": "modpack", "slug": "pack", "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}})
		case "/version/pack-v":
			data := makePack()
			hash := sha512.Sum512(data)
			json.NewEncoder(w).Encode(map[string]any{"id": "pack-v", "project_id": "pack", "name": "Fixture 1", "game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"}, "files": []any{map[string]any{"filename": "fixture.mrpack", "url": server.URL + "/pack.mrpack", "size": len(data), "primary": true, "hashes": map[string]string{"sha512": hex.EncodeToString(hash[:])}}}})
		case "/mods/1":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 1, "classId": 4471, "name": "Curse fixture", "slug": "fixture"}})
		case "/mods/1/files/2":
			data := makePack()
			hash := sha1.Sum(data)
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 2, "modId": 1, "displayName": "Curse pack", "fileName": "fixture.zip", "downloadUrl": server.URL + "/pack.zip", "fileLength": len(data), "hashes": []any{map[string]any{"algo": 1, "value": hex.EncodeToString(hash[:])}}}})
		case "/mods/42/files/99":
			json.NewEncoder(w).Encode(map[string]any{"data": map[string]any{"id": 99, "modId": 42, "displayName": "Curse mod", "fileName": "example.jar", "downloadUrl": server.URL + "/mod.jar", "fileLength": len(jar), "hashes": []any{map[string]any{"algo": 1, "value": hex.EncodeToString(sha[:])}}}})
		case "/pack.zip", "/pack.mrpack":
			w.Write(makePack())
		case "/mod.jar":
			if bad {
				w.Write([]byte("corrupt"))
			} else {
				w.Write(jar)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(server.Close)
	client := New("private-fixture-key")
	client.Modrinth = server.URL
	client.CurseForge = server.URL
	return NewManager(client), server, &bad
}
func TestModrinthPackDownloadsClientFilesAndBacksUpExistingBuild(t *testing.T) {
	manager, _, _ := packFixture(t, false)
	p, err := manager.PreparePack(context.Background(), "modrinth", "pack", "pack-v", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Game != "1.21.1" || p.Loader != "fabric" || p.LoaderVersion != "0.16.14" || p.Files != 3 {
		t.Fatal(p)
	}
	target := t.TempDir()
	for file, data := range map[string]string{"mods/old.jar": "old mod", "config/old.toml": "old config", "saves/World/level.dat": "my world", "instance.json": "my identity"} {
		dest := filepath.Join(target, filepath.FromSlash(file))
		os.MkdirAll(filepath.Dir(dest), 0700)
		os.WriteFile(dest, []byte(data), 0600)
	}
	called := false
	err = manager.StartPack(context.Background(), p.ID, Target{Directory: target, Game: p.Game, Loader: p.Loader}, func(context.Context, PackPlan, func(string, float64)) error { called = true; return nil })
	if err != nil {
		t.Fatal(err)
	}
	if state := waitInstall(t, manager); state.Error != "" {
		t.Fatal(state.Error)
	}
	if !called {
		t.Fatal("Minecraft install callback missing")
	}
	for file, want := range map[string]string{"config/test.toml": "from=client", "saves/World/level.dat": "my world", "instance.json": "my identity"} {
		data, e := os.ReadFile(filepath.Join(target, filepath.FromSlash(file)))
		if e != nil || string(data) != want {
			t.Fatal(file, string(data), e)
		}
	}
	if _, e := os.Stat(filepath.Join(target, "mods", "server.jar")); !os.IsNotExist(e) {
		t.Fatal("server-only mod installed")
	}
	backups, _ := filepath.Glob(filepath.Join(target, ".sintet-pack-backups", "before-*", "mods", "old.jar"))
	if len(backups) != 1 {
		t.Fatal("previous mods were not retained", backups)
	}
	if _, e := os.Stat(filepath.Join(p.folder, "source.zip")); !os.IsNotExist(e) {
		t.Fatal("temporary archive retained")
	}
}
func TestCurseForgePackResolvesProjectFileReferences(t *testing.T) {
	manager, _, _ := packFixture(t, true)
	p, err := manager.PreparePack(context.Background(), "curseforge", "1", "2", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if p.Loader != "forge" || p.LoaderVersion != "1.20.1-47.4.23" || p.Files != 2 {
		t.Fatal(p)
	}
	target := t.TempDir()
	if err = manager.StartPack(context.Background(), p.ID, Target{Directory: target, Game: p.Game, Loader: p.Loader}, nil); err != nil {
		t.Fatal(err)
	}
	if state := waitInstall(t, manager); state.Error != "" {
		t.Fatal(state.Error)
	}
	if _, err = os.Stat(filepath.Join(target, "mods", "example.jar")); err != nil {
		t.Fatal(err)
	}
}
func TestCorruptPackDownloadPreservesExistingFiles(t *testing.T) {
	manager, _, bad := packFixture(t, false)
	p, err := manager.PreparePack(context.Background(), "modrinth", "pack", "pack-v", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	*bad = true
	target := t.TempDir()
	os.MkdirAll(filepath.Join(target, "mods"), 0700)
	os.WriteFile(filepath.Join(target, "mods", "mine.jar"), []byte("keep"), 0600)
	if err = manager.StartPack(context.Background(), p.ID, Target{Directory: target, Game: p.Game, Loader: p.Loader}, nil); err != nil {
		t.Fatal(err)
	}
	if state := waitInstall(t, manager); state.Error == "" {
		t.Fatal("corrupt mod accepted")
	}
	data, e := os.ReadFile(filepath.Join(target, "mods", "mine.jar"))
	if e != nil || string(data) != "keep" {
		t.Fatal("user mod damaged")
	}
}
func TestCatalogPackRejectsZipTraversalAndLauncherOverrides(t *testing.T) {
	manager, _, _ := packFixture(t, false)
	for _, name := range []string{"overrides/../../settings.json", "overrides/mods/evil.exe", "overrides/instance.json", "overrides/INSTANCE.JSON", "overrides/.sintet-pack.json", "overrides/.sintet-pack-backups/before/config.json", "overrides/versions/test/test.json", "overrides/libraries/test.jar", "overrides/custom/evil.ps1", "overrides/custom/evil.scr", "overrides/config/a.json:stream", "overrides/mods/CON.jar"} {
		index := []byte(`{"formatVersion":1,"game":"minecraft","dependencies":{"minecraft":"1.21.1","fabric-loader":"0.16.14"},"files":[]}`)
		data := packZIP(t, map[string][]byte{"modrinth.index.json": index, name: []byte("evil")})
		file := filepath.Join(t.TempDir(), "bad.zip")
		os.WriteFile(file, data, 0600)
		plan := PackPlan{Project: Project{Provider: "modrinth"}}
		if manager.parsePack(context.Background(), file, &plan) == nil {
			t.Fatal("accepted unsafe archive", name)
		}
	}
}

func TestCatalogPackInstallsRootConfigsDocumentsAndCustomFolders(t *testing.T) {
	for _, provider := range []string{"modrinth", "curseforge"} {
		t.Run(provider, func(t *testing.T) {
			files := map[string][]byte{}
			prefix := "overrides/"
			if provider == "modrinth" {
				files["modrinth.index.json"] = []byte(`{"formatVersion":1,"game":"minecraft","dependencies":{"minecraft":"1.21.1","fabric-loader":"0.16.14"},"files":[]}`)
				files["client-overrides/emi.json"] = []byte(`{"enabled":true}`)
			} else {
				prefix = "client-files/"
				files["manifest.json"] = []byte(`{"manifestType":"minecraftModpack","manifestVersion":1,"minecraft":{"version":"1.21.1","modLoaders":[{"id":"fabric-0.16.14","primary":true}]},"files":[],"overrides":"client-files"}`)
			}
			want := map[string]string{
				"emi.json":                               `{"enabled":true}`,
				"COBBLEVERSE - Third-Party Licenses.pdf": "%PDF-1.4\nfixture licence document",
				"LICENSE":                                "Fixture licence",
				"README.md":                              "# Pack documentation",
				"cobblemon/settings.json":                `{"fixture":true}`,
				".bobby/config.json":                     `{"fixture":true}`,
				"defaultconfigs/example.toml":            "enabled=true",
			}
			for name, data := range want {
				files[prefix+name] = []byte(data)
			}
			if provider == "modrinth" {
				files[prefix+"emi.json"] = []byte(`{"enabled":false}`)
			}
			files[prefix+"saves/World/level.dat"] = []byte("must not replace world")
			folder := t.TempDir()
			archive := filepath.Join(folder, "source.zip")
			if err := os.WriteFile(archive, packZIP(t, files), 0600); err != nil {
				t.Fatal(err)
			}
			manager := NewManager(New(""))
			plan := PackPlan{Project: Project{Provider: provider}, folder: folder}
			if err := manager.parsePack(context.Background(), archive, &plan); err != nil {
				t.Fatal(err)
			}
			if plan.Files != len(want) || len(plan.Warnings) == 0 {
				t.Fatalf("incorrect overrides plan: %d files, warnings %v", plan.Files, plan.Warnings)
			}
			target := t.TempDir()
			for name, data := range map[string]string{"emi.json": "old settings", "instance.json": "my instance", "saves/World/level.dat": "my world"} {
				file := filepath.Join(target, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(file), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(file, []byte(data), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.installPack(context.Background(), plan, target); err != nil {
				t.Fatal(err)
			}
			want["instance.json"] = "my instance"
			want["saves/World/level.dat"] = "my world"
			for name, expected := range want {
				actual, err := os.ReadFile(filepath.Join(target, filepath.FromSlash(name)))
				if err != nil || string(actual) != expected {
					t.Fatalf("%s: got %q, error %v", name, actual, err)
				}
			}
			backups, err := filepath.Glob(filepath.Join(target, ".sintet-pack-backups", "before-*", "emi.json"))
			if err != nil || len(backups) != 1 {
				t.Fatal("root config not backed up", backups, err)
			}
			previous, err := os.ReadFile(backups[0])
			if err != nil || string(previous) != "old settings" {
				t.Fatal("root config backup differs", string(previous), err)
			}
		})
	}
}

func TestLiveModrinthPackManifest(t *testing.T) {
	if os.Getenv("SINTET_PACK_LIVE") != "1" {
		t.Skip("live catalog check is opt-in")
	}
	manager := NewManager(New(""))
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	project := os.Getenv("SINTET_PACK_LIVE_PROJECT")
	if project == "" {
		project = "fabulously-optimized"
	}
	versions, err := manager.Client.Versions(ctx, "modrinth", project, "1.21.1", "")
	if err != nil || len(versions) == 0 {
		t.Fatal("live versions", err)
	}
	plan, err := manager.PreparePack(ctx, "modrinth", project, versions[0].ID, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	defer removePackStage(plan.folder)
	if plan.Files < 10 || plan.Loader != "fabric" || plan.Game != "1.21.1" {
		t.Fatal(plan)
	}
	t.Logf("Prepared actual %s: %d files, %d bytes, %s %s", plan.Project.Name, plan.Files, plan.Bytes, plan.Loader, plan.LoaderVersion)
	if os.Getenv("SINTET_PACK_LIVE_INSTALL") == "1" {
		target := t.TempDir()
		if err = manager.StartPack(ctx, plan.ID, Target{Directory: target, Game: plan.Game, Loader: plan.Loader}, nil); err != nil {
			t.Fatal(err)
		}
		for manager.State().Busy {
			select {
			case <-ctx.Done():
				manager.Cancel()
				t.Fatal(ctx.Err())
			case <-time.After(100 * time.Millisecond):
			}
		}
		if state := manager.State(); state.Error != "" {
			t.Fatal(state.Error)
		}
		items, err := Scan(target, "mods")
		if err != nil || len(items) < 20 {
			t.Fatal("real modpack did not install mods", len(items), err)
		}
		t.Logf("Installed actual modpack into disposable directory: %d mod JARs", len(items))
	}
}
