package workshop

import (
	"bytes"
	"compress/gzip"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkshopWorldEditTrashAndRestore(t *testing.T) {
	instance := t.TempDir()
	folder := filepath.Join(instance, "saves", "original")
	if err := os.MkdirAll(folder, 0700); err != nil {
		t.Fatal(err)
	}
	// Root/Data includes a mod-owned compound that should survive the edit.
	fixture, err := hex.DecodeString("0a00000a000444617461" +
		"0800094c6576656c4e616d6500034f6c64" +
		"01000a446966666963756c747902" +
		"01000868617264636f726500" +
		"01000d616c6c6f77436f6d6d616e647300" +
		"0a00036d6f64070004646174610000000301020300" +
		"0000")
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(folder, "level.dat")
	var compressed bytes.Buffer
	writer := gzip.NewWriter(&compressed)
	_, err = writer.Write(fixture)
	if err != nil {
		t.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(file, compressed.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}

	if err := EditWorld(instance, "original", "Мир 🌙", 3, 1); err != nil {
		t.Fatal(err)
	}
	worlds, err := LocalWorldDetails(instance)
	if err != nil || len(worlds) != 1 || worlds[0] != (LocalWorld{Folder: "original", Name: "Мир 🌙", Editable: true, Difficulty: 3, Commands: 1}) {
		t.Fatalf("edited metadata: %+v %v", worlds, err)
	}
	data, _, err := readWorldMetadata(file)
	fragment, _ := hex.DecodeString("0a00036d6f64070004646174610000000301020300")
	if err != nil || !bytes.Contains(data, fragment) {
		t.Fatal("edit lost an unrelated compound", err)
	}
	id, err := TrashWorld(instance, "original")
	if err != nil {
		t.Fatal(err)
	}
	if worlds, err := LocalWorlds(instance); err != nil || len(worlds) != 0 {
		t.Fatal("deleted world still visible", worlds, err)
	}
	if err := RestoreWorld(instance, id); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(file); err != nil {
		t.Fatal(err)
	}
	if _, err := TrashWorld(instance, "../original"); err == nil {
		t.Fatal("invalid folder accepted")
	}
}
