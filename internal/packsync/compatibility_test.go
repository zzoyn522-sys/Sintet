package packsync

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRuntimeConfigRewritesDoNotGateJoiningButModChangesDo(t *testing.T) {
	snapshot, root := fixture(t)
	approved := snapshot.Manifest
	if err := os.WriteFile(filepath.Join(root, "config", "test", "client.toml"), []byte("# rewritten by the mod\nenabled = true\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"generated.json", "preferences.json"} {
		if err := os.WriteFile(filepath.Join(root, "config", name), []byte("{}"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	local, err := Scan(context.Background(), root, approved, "")
	if err != nil {
		t.Fatal(err)
	}
	if !Compatible(local.Manifest, approved) {
		t.Fatal("runtime rewrites forced a download")
	}
	if err := os.WriteFile(filepath.Join(root, "mods", "example.jar"), []byte("different mod version"), 0600); err != nil {
		t.Fatal(err)
	}
	changed, err := Scan(context.Background(), root, approved, "")
	if err != nil {
		t.Fatal(err)
	}
	if Compatible(changed.Manifest, approved) {
		t.Fatal("changed mod bypassed synchronization")
	}
}

func TestHostConfigurationDoesNotGateJoiningButExtraLocalModDoes(t *testing.T) {
	snapshot, root := fixture(t)
	approved := snapshot.Manifest
	updated := approved
	updated.Files = append([]File{}, approved.Files...)
	for i := range updated.Files {
		if strings.HasPrefix(updated.Files[i].Path, "config/") {
			updated.Files[i].SHA256 = strings.Repeat("a", 64)
		}
	}
	if !Compatible(approved, updated) {
		t.Fatal("host settings alone forced an unnecessary download")
	}
	if err := os.WriteFile(filepath.Join(root, "mods", "extra.jar"), []byte("extra mod"), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := Scan(context.Background(), root, approved, "")
	if err != nil {
		t.Fatal(err)
	}
	if Compatible(local.Manifest, approved) {
		t.Fatal("extra executable mod was ignored")
	}
}

func TestCompatibilityIgnoresAuxiliaryMetadataAndExtraGeneratedConfig(t *testing.T) {
	snapshot, root := fixture(t)
	changed := snapshot.Manifest
	changed.Files = append([]File{}, changed.Files...)
	changed.Format = 1
	changed.Name = "different display name"
	for i := range changed.Files {
		changed.Files[i].SHA1 = ""
		changed.Files[i].CurseFingerprint = 123
	}
	if !SameContent(changed, snapshot.Manifest) {
		t.Fatal("metadata alone changed pack compatibility")
	}
	if err := os.WriteFile(filepath.Join(root, "config", "generated.json"), []byte("{}"), 0600); err != nil {
		t.Fatal(err)
	}
	local, err := Scan(context.Background(), root, snapshot.Manifest, "")
	if err != nil || !Compatible(local.Manifest, snapshot.Manifest) {
		t.Fatal("generated config forced a restart", err)
	}
}

func TestJoinInventoryNeverReadsRuntimeConfigOrLogs(t *testing.T) {
	snapshot, root := fixture(t)
	for _, name := range []string{"config/logs/latest.json", "config/cache/runtime.dat", "config/player-settings.json", "logs/latest.log"} {
		file := filepath.Join(root, filepath.FromSlash(name))
		os.MkdirAll(filepath.Dir(file), 0700)
		f, err := os.Create(file)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Truncate(MaxConfig + 1); err != nil {
			f.Close()
			t.Fatal(err)
		}
		f.Close()
	}
	local, err := ScanMods(context.Background(), root, snapshot.Manifest)
	if err != nil || len(local.Manifest.Files) != 1 || !Compatible(local.Manifest, snapshot.Manifest) {
		t.Fatal("runtime config/log data prevented joining", err)
	}
	for _, name := range []string{"config/logs/latest.json", "config/cache/runtime.dat"} {
		if !transientConfigPath(name) {
			t.Fatal("host runtime file was not excluded", name)
		}
	}
}
