package workshop

import (
	"archive/zip"
	"bytes"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"sintet/internal/packsync"
)

func writeSintedotTestFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
}

func sintedotTestMod(t *testing.T, loader, id, version, dependency, constraint string) []byte {
	t.Helper()
	var out bytes.Buffer
	writer := zip.NewWriter(&out)
	var name, body string
	if loader == "fabric" {
		name = "fabric.mod.json"
		data := map[string]any{"id": id, "version": version}
		if dependency != "" {
			data["depends"] = map[string]json.RawMessage{dependency: json.RawMessage(constraint)}
		}
		raw, err := json.Marshal(data)
		if err != nil {
			t.Fatal(err)
		}
		body = string(raw)
	} else {
		name = "META-INF/neoforge.mods.toml"
		body = fmt.Sprintf("[[mods]]\nmodId=%q\nversion=%q\n", id, version)
		if dependency != "" {
			body += fmt.Sprintf("[[dependencies.%s]]\nmodId=%q\nversionRange=%q\nside='BOTH'\n", id, dependency, constraint)
		}
	}
	entry, err := writer.Create(name)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = entry.Write([]byte(body)); err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestSintedotReportsRangeIntersectionConflicts(t *testing.T) {
	for _, tc := range []struct{ loader, first, second string }{
		{"fabric", `">=0.92 <0.100"`, `[">=0.101 <0.110","^2.0"]`},
		{"neoforge", "[0.92,0.100)", "[0.101,)"},
	} {
		t.Run(tc.loader, func(t *testing.T) {
			target := Target{ID: "build-conflict", Directory: t.TempDir(), Game: "1.21.1", Loader: tc.loader}
			mods := filepath.Join(target.Directory, "mods")
			jar := sintedotTestMod(t, tc.loader, "lib", "0.98", "", "")
			writeSintedotTestFile(t, filepath.Join(mods, "lib.jar"), jar)
			writeSintedotTestFile(t, filepath.Join(mods, "a.jar"), sintedotTestMod(t, tc.loader, "first", "1", "lib", tc.first))
			writeSintedotTestFile(t, filepath.Join(mods, "b.jar"), sintedotTestMod(t, tc.loader, "second", "1", "lib", tc.second))
			manager, _, _ := fixture(t)
			if err := manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
				t.Fatal(err)
			}
			state := manager.SintedotState()
			if len(state.Conflicts) != 1 || state.Backup != "" || state.Error != "" {
				t.Fatalf("incorrect conflict report: %+v", state)
			}
			c := state.Conflicts[0]
			if c.ModID != "lib" || !reflect.DeepEqual(c.Installed, []string{"0.98"}) ||
				len(c.Requirements) != 2 || c.Requirements[0].Mod != "first" || c.Requirements[1].Mod != "second" {
				t.Fatalf("missing conflict details: %+v", c)
			}
			got, err := os.ReadFile(filepath.Join(mods, "lib.jar"))
			if err != nil || !bytes.Equal(got, jar) {
				t.Fatalf("live JAR changed: %v", err)
			}
		})
	}
}

func TestSintedotDeduplicatesByModIDAndAllIncomingRanges(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "build-dedup", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	mods := filepath.Join(target.Directory, "mods")
	writeSintedotTestFile(t, filepath.Join(mods, "numeric-highest.jar"), sintedotTestMod(t, "fabric", "lib", "2.0", "", ""))
	writeSintedotTestFile(t, filepath.Join(mods, "different-filename.jar"), sintedotTestMod(t, "fabric", "lib", "1.6", "", ""))
	writeSintedotTestFile(t, filepath.Join(mods, "obsolete.jar"), sintedotTestMod(t, "fabric", "lib", "1.1", "", ""))
	writeSintedotTestFile(t, filepath.Join(mods, "first.jar"), sintedotTestMod(t, "fabric", "first", "1", "lib", `"^1.2"`))
	writeSintedotTestFile(t, filepath.Join(mods, "second.jar"), sintedotTestMod(t, "fabric", "second", "1", "lib", `["~0.3",">=1.5 <=1.6"]`))
	if err := manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"numeric-highest.jar.disabled", "obsolete.jar.disabled", "different-filename.jar"} {
		if _, err := os.Stat(filepath.Join(mods, name)); err != nil {
			t.Fatal(err)
		}
	}
	if len(manager.SintedotState().Conflicts) != 0 {
		t.Fatal(manager.SintedotState())
	}
}

func TestSintedotRecoversPendingTransactionOnStartup(t *testing.T) {
	for steps := 0; steps <= 4; steps++ {
		t.Run(fmt.Sprint(steps), func(t *testing.T) {
			game := t.TempDir()
			root := filepath.Join(game, "instances", "build-crash")
			backup := filepath.Join(root, ".sintedot-backup-crash")
			stage := filepath.Join(root, ".sintedot-stage-crash")
			config := filepath.Join("config", "test.json")
			original := []byte("{")
			writeSintedotTestFile(t, filepath.Join(root, "mods", "before.jar"), []byte("before"))
			writeSintedotTestFile(t, filepath.Join(root, config), original)
			writeSintedotTestFile(t, filepath.Join(stage, "mods", "after.jar"), []byte("after"))
			writeSintedotTestFile(t, filepath.Join(stage, config+".broken"), original)
			if err := os.MkdirAll(filepath.Join(backup, "config"), 0700); err != nil {
				t.Fatal(err)
			}
			tx := &sintedotTx{Status: "pending", Backup: filepath.Base(backup), Files: []sintedotTxFile{
				{Path: "mods", Original: true, Replacement: true},
				{Path: config, Original: true},
				{Path: config + ".broken", Replacement: true},
			}}
			if err := writeSintedotTx(root, tx); err != nil {
				t.Fatal(err)
			}
			moves := [][2]string{
				{filepath.Join(root, "mods"), filepath.Join(backup, "mods")},
				{filepath.Join(stage, "mods"), filepath.Join(root, "mods")},
				{filepath.Join(root, config), filepath.Join(backup, config)},
				{filepath.Join(stage, config+".broken"), filepath.Join(root, config+".broken")},
			}
			for _, move := range moves[:steps] {
				if err := moveTxFile(move[0], move[1]); err != nil {
					t.Fatal(err)
				}
			}
			for i := 0; i < 2; i++ {
				if err := RecoverSintedotInstances(game); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := os.Stat(filepath.Join(root, "mods", "before.jar")); err != nil {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(root, config))
			if err != nil || !bytes.Equal(got, original) {
				t.Fatalf("config was not restored: %q, %v", got, err)
			}
			for _, rel := range []string{config + ".broken", filepath.Join("mods", "after.jar")} {
				if _, err := os.Stat(filepath.Join(root, rel)); !os.IsNotExist(err) {
					t.Fatalf("%s survives recovery: %v", rel, err)
				}
			}
			journal, err := loadSintedotTx(root)
			if err != nil || journal.Status != "rolled_back" {
				t.Fatalf("journal: %+v, %v", journal, err)
			}
		})
	}
}

func TestSintedotQuarantinesAndRollsBackInvalidConfigs(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "build-config", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	folder := filepath.Join(target.Directory, "config", "nested")
	files := map[string]string{"config.json": `{"x":]`, "editor.toml": "[broken\nx=1", "ok.json": `{"ok":[1]}`, "ok.toml": "[test]\nkey = true"}
	for name, data := range files {
		writeSintedotTestFile(t, filepath.Join(folder, name), []byte(data))
	}
	if err := manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"config.json", "editor.toml"} {
		if _, err := os.Stat(filepath.Join(folder, name)); !os.IsNotExist(err) {
			t.Fatalf("invalid config stayed active: %v", err)
		}
		got, err := os.ReadFile(filepath.Join(folder, name+".broken"))
		if err != nil || string(got) != files[name] {
			t.Fatalf("quarantine: %q %v", got, err)
		}
	}
	if !CanRollbackSintedot(target.Directory) {
		t.Fatal("rollback unavailable")
	}
	writeSintedotTestFile(t, filepath.Join(folder, "config.json"), []byte(`{"regenerated":true}`))
	if err := RollbackSintedot(target.Directory); err != nil {
		t.Fatal(err)
	}
	if CanRollbackSintedot(target.Directory) {
		t.Fatal("already rolled back")
	}
	for name, data := range files {
		got, err := os.ReadFile(filepath.Join(folder, name))
		if err != nil || string(got) != data {
			t.Fatalf("%s: %q %v", name, got, err)
		}
	}
}

func TestSintedotMatchesCurseForgeFingerprintAndSHA1(t *testing.T) {
	path := filepath.Join(t.TempDir(), "manual.jar")
	body := sintedotTestMod(t, "fabric", "library", "1.2", "", "")
	writeSintedotTestFile(t, path, body)
	fingerprint, err := packsync.CurseFingerprint(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	sha := sha1.Sum(body)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var request struct{ Fingerprints []uint32 }
		if r.Method != "POST" || r.URL.Path != "/fingerprints/432" || json.NewDecoder(r.Body).Decode(&request) != nil ||
			len(request.Fingerprints) != 1 || request.Fingerprints[0] != fingerprint {
			t.Error("bad fingerprint request")
		}
		fmt.Fprintf(w, `{"data":{"exactMatches":[{"file":{"id":78,"modId":17,"fileFingerprint":%d,"fileLength":%d,"hashes":[{"algo":1,"value":"wrong"}]}},{"file":{"id":79,"modId":17,"fileFingerprint":%d,"fileLength":%d,"hashes":[{"algo":1,"value":"%x"}]}}]}}`, fingerprint, len(body), fingerprint, len(body), sha)
	}))
	defer server.Close()
	client := New("key-for-fixture")
	client.CurseForge = server.URL
	record, ok := NewManager(client).identifyCurseForge(t.Context(), path, "manual.jar", "sha256")
	if !ok || record.Project != "17" || record.Version != "79" {
		t.Fatalf("%+v, %v", record, ok)
	}
}

func TestSintedotCatalogueFindsNewestVersionWithinIntersection(t *testing.T) {
	versions := []string{"2.3", "1.7", "1.5", "1.3"}
	jars := map[string][]byte{}
	for _, v := range versions {
		jars[v] = sintedotTestMod(t, "fabric", "lib", v, "", "")
	}
	var server *httptest.Server
	server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/project/lib/version" {
			var rows []map[string]any
			for i, v := range versions {
				sum := sha512.Sum512(jars[v])
				rows = append(rows, map[string]any{"id": "v" + strings.ReplaceAll(v, ".", ""), "project_id": "lib", "name": v, "date_published": fmt.Sprintf("2025-0%d-01T00:00:00Z", len(versions)-i),
					"game_versions": []string{"1.21.1"}, "loaders": []string{"fabric"},
					"files": []any{map[string]any{"filename": v + ".jar", "url": server.URL + "/download/" + v, "primary": true, "size": len(jars[v]), "hashes": map[string]string{"sha512": fmt.Sprintf("%x", sum)}}}})
			}
			_ = json.NewEncoder(w).Encode(rows)
			return
		}
		if data := jars[filepath.Base(r.URL.Path)]; data != nil {
			_, _ = w.Write(data)
			return
		}
		http.NotFound(w, r)
	}))
	defer server.Close()
	client := New("")
	client.Modrinth = server.URL
	target := Target{ID: "build-selection", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	mods := filepath.Join(target.Directory, "mods")
	oldPath := filepath.Join(mods, "1.3.jar")
	writeSintedotTestFile(t, oldPath, jars["1.3"])
	sum, err := fileHash(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	records, _ := json.Marshal(map[string]Installed{"modrinth:lib": {Provider: "modrinth", Project: "lib", Version: "v13", Filename: "1.3.jar", Hash: sum}})
	writeSintedotTestFile(t, filepath.Join(target.Directory, ".sintet-mods.json"), records)
	writeSintedotTestFile(t, filepath.Join(mods, "first.jar"), sintedotTestMod(t, "fabric", "first", "1", "lib", `">=1.4"`))
	writeSintedotTestFile(t, filepath.Join(mods, "second.jar"), sintedotTestMod(t, "fabric", "second", "1", "lib", `"<1.7"`))
	manager := NewManager(client)
	if err := manager.RunSintedot(t.Context(), target, "update", nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(mods, "1.5.jar")); err != nil {
		t.Fatalf("wrong selected version: %v %+v", err, manager.SintedotState())
	}
	if _, err := os.Stat(filepath.Join(mods, "2.3.jar")); !os.IsNotExist(err) {
		t.Fatalf("incompatible interim version reached live mods: %v", err)
	}
}

func TestSintedotVersionPredicateBoundaries(t *testing.T) {
	for _, test := range []struct {
		spec, accept, reject string
	}{
		{`">=1.4 <=1.7"`, "1.7+build", "1.7.1"},
		{`">1.2 <2"`, "1.2.1", "1.2"},
		{`"=1.4.0"`, "1.4", "1.4.0-rc.1"},
		{`"~1.4"`, "1.4.30", "1.5"},
		{`"^0.4.0"`, "0.9.0", "1.0"},
		{`"1.4.*"`, "1.4.30", "1.5"},
		{`["^2.0","^3.0"]`, "3.1", "4.0"},
	} {
		match, err := parseFabricRange(json.RawMessage(test.spec))
		if err != nil || !match(test.accept) || match(test.reject) {
			t.Fatalf("Fabric %s: %v", test.spec, err)
		}
	}
	for _, test := range []struct {
		spec, accept, reject string
	}{
		{"[1.2,1.8]", "1.8", "1.8.1"},
		{"(1.2,1.8)", "1.6", "1.2"},
		{"[1.2,)", "1.2", "1.2-rc1"},
		{"(,1.8]", "1.8", "1.8.1"},
		{"(,1.0],[2.0,)", "2.5", "1.5"},
	} {
		match, err := parseMavenRange(test.spec)
		if err != nil || !match(test.accept) || match(test.reject) {
			t.Fatalf("Maven %s: %v", test.spec, err)
		}
	}
	match, err := parseFabricRange(json.RawMessage(`"*"`))
	if err != nil || !match("non-semver-version") {
		t.Fatalf("unrestricted Fabric range: %v", err)
	}
}

func TestSintedotRestoresOnlyRequiredDisabledMod(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "build-disabled", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	dir := filepath.Join(target.Directory, "mods")
	writeSintedotTestFile(t, filepath.Join(dir, "consumer.jar"), sintedotTestMod(t, "fabric", "consumer", "1", "library", `">=1.1 <2"`))
	writeSintedotTestFile(t, filepath.Join(dir, "library.jar.disabled"), sintedotTestMod(t, "fabric", "library", "1.2", "", ""))
	writeSintedotTestFile(t, filepath.Join(dir, "other.jar.disabled"), sintedotTestMod(t, "fabric", "other", "1", "", ""))
	if err := manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
		t.Fatal(err)
	}
	state := manager.SintedotState()
	if state.Stage != "Исправления применены" || state.Preview || len(state.Changes) != 1 {
		t.Fatalf("unexpected report: %+v", state)
	}
	for _, name := range []string{"library.jar", "other.jar.disabled"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSintedotStillRepairsConfigWhenModRangesConflict(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "build-config-conflict", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	dir := filepath.Join(target.Directory, "mods")
	library := sintedotTestMod(t, "fabric", "library", "1.2", "", "")
	writeSintedotTestFile(t, filepath.Join(dir, "one.jar"), sintedotTestMod(t, "fabric", "one", "1", "library", `"<2"`))
	writeSintedotTestFile(t, filepath.Join(dir, "two.jar"), sintedotTestMod(t, "fabric", "two", "1", "library", `">2"`))
	writeSintedotTestFile(t, filepath.Join(dir, "library.jar"), library)
	writeSintedotTestFile(t, filepath.Join(target.Directory, "config", "bad.json"), []byte("{"))
	if err := manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
		t.Fatal(err)
	}
	state := manager.SintedotState()
	if state.Stage != "Часть проблем исправлена" || len(state.Conflicts) != 1 || len(state.Changes) != 1 {
		t.Fatalf("unexpected report: %+v", state)
	}
	if _, err := os.Stat(filepath.Join(target.Directory, "config", "bad.json.broken")); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dir, "library.jar"))
	if err != nil || !bytes.Equal(got, library) {
		t.Fatalf("conflicting library was modified: %v", err)
	}
}

func TestSintedotReplacesExistingDependencyAndQuarantinesWrongLoader(t *testing.T) {
	manager, _, _ := fixture(t)
	target := Target{ID: "build-mixed", Directory: t.TempDir(), Game: "1.21.1", Loader: "fabric"}
	mods := filepath.Join(target.Directory, "mods")
	root := testJar(t, "MainMod")
	rootPath := filepath.Join(mods, "root.jar")
	writeSintedotTestFile(t, rootPath, root)
	digest, err := fileHash(rootPath)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := json.Marshal(map[string]Installed{
		"modrinth:root": {Provider: "modrinth", Project: "root", Version: "root-v", Filename: "root.jar", Hash: digest},
	})
	if err != nil {
		t.Fatal(err)
	}
	writeSintedotTestFile(t, filepath.Join(target.Directory, ".sintet-mods.json"), raw)
	old := sintedotTestMod(t, "fabric", "Dependency", "0.9", "", "")
	writeSintedotTestFile(t, filepath.Join(mods, "manual-dependency.jar"), old)
	writeSintedotTestFile(t, filepath.Join(mods, "diversity-forge.jar"), sintedotTestMod(t, "neoforge", "diversity", "2.5.3", "", ""))
	if err = manager.RunSintedot(t.Context(), target, "repair", nil); err != nil {
		t.Fatal(err)
	}
	report := manager.SintedotState()
	for _, name := range []string{"dep.jar", "root.jar", "diversity-forge.jar.disabled"} {
		if _, err = os.Stat(filepath.Join(mods, name)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = os.Stat(filepath.Join(mods, "manual-dependency.jar")); !os.IsNotExist(err) {
		t.Fatalf("duplicate still active: %v", err)
	}
	if got, e := os.ReadFile(filepath.Join(report.Backup, "mods", "manual-dependency.jar")); e != nil || !bytes.Equal(got, old) {
		t.Fatalf("original JAR not backed up: %v", e)
	}
	if report.Stage != "Исправления применены" {
		t.Fatalf("incorrect terminal result: %+v", report)
	}
}
