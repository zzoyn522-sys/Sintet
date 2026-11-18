package launcher

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestDeleteVersionPreservesSharedComponents(t *testing.T) {
	s := New(t.TempDir())
	const id = "1.21.1"
	const build = "build-fixture"
	paths := []string{
		"versions/1.21.1/1.21.1.jar",
		"libraries/shared.jar",
		"libraries/exclusive.jar",
		"assets/objects/aa/exclusive",
		"instances/1.21.1/saves/world/level.dat",
		"instances/family/vanilla-1.21/saves/family/level.dat",
		"instances/build-fixture/saves/other/level.dat",
		"natives/1.21.1/lwjgl.dll",
		".sintet/profiles/1.21.1.json",
		"runtime/java-21/bin/java.exe",
	}
	for _, path := range paths {
		if err := save(filepath.Join(s.Root, path), []byte("fixture")); err != nil {
			t.Fatal(err)
		}
	}
	receipt := Receipt{ID: id, Base: id, Loader: "vanilla"}
	for _, path := range paths[:4] {
		receipt.Files = append(receipt.Files, File{Path: path})
	}
	if err := saveJSON(s.receiptPath(id), receipt); err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(s.receiptPath(build), Receipt{ID: build, Base: id, Loader: "vanilla", Files: receipt.Files[:2]}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVersion(id); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{paths[2], paths[3], paths[4], paths[7], paths[8], ".sintet/installed/1.21.1.json"} {
		if _, err := os.Stat(filepath.Join(s.Root, path)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("%s still exists: %v", path, err)
		}
	}
	for _, path := range []string{paths[0], paths[1], paths[5], paths[6], paths[9]} {
		if _, err := os.Stat(filepath.Join(s.Root, path)); err != nil {
			t.Fatalf("%s should remain: %v", path, err)
		}
	}
}

func TestDeleteVersionRollsBackOnUnsafeArtifact(t *testing.T) {
	s := New(t.TempDir())
	const id = "rd-132211"
	const world = "instances/rd-132211/saves/world/level.dat"
	const invalid = "libraries/directory.jar"
	if err := save(filepath.Join(s.Root, world), []byte("world")); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(s.Root, invalid), 0700); err != nil {
		t.Fatal(err)
	}
	if err := saveJSON(s.receiptPath(id), Receipt{ID: id, Base: id, Loader: "vanilla", Files: []File{{Path: invalid}}}); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVersion(id); err == nil {
		t.Fatal("unsafe artifact must stop deletion")
	}
	if data, err := os.ReadFile(filepath.Join(s.Root, world)); err != nil || string(data) != "world" {
		t.Fatalf("rollback did not restore world: %q, %v", data, err)
	}
	if _, err := s.readReceipt(id); err != nil {
		t.Fatal("receipt was lost", err)
	}
	if err := os.Remove(filepath.Join(s.Root, invalid)); err != nil {
		t.Fatal(err)
	}
	if err := s.DeleteVersion(id); err != nil {
		t.Fatal("retry could not delete version", err)
	}
}
