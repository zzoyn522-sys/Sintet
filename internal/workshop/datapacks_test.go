package workshop

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestEditWorldDataPacks(t *testing.T) {
	instance := t.TempDir()
	world := filepath.Join(instance, "saves", "test")
	packs := filepath.Join(world, "datapacks")
	const archive = "Рецепты 🌙.zip"
	const directory = "More loot"
	if err := os.MkdirAll(filepath.Join(packs, directory), 0700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{archive, filepath.Join(directory, "pack.mcmeta")} {
		if err := os.WriteFile(filepath.Join(packs, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	file := filepath.Join(world, "level.dat")
	enabled := dataPackList{ids: []string{"vanilla", dataPackID(archive), "mod/example"}}
	extra := []byte("\x01\x00\x03mod\x01")
	if err := writeWorldMetadata(file, []byte("\x0a\x00\x00\x0a\x00\x04Data\x08\x00\x09LevelName"),
		encodeWorldName("World"), []byte("\x0a\x00\x09DataPacks\x09\x00\x08Disabled\x00\x00\x00\x00\x00"),
		extra, []byte("\x09\x00\x07Enabled"), enabled.encode(), []byte{0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	changes := []WorldDataPack{{Name: archive, Mode: "disabled"}, {Name: directory, Mode: "enabled"}}
	if err := EditWorldDataPacks(instance, "test", changes); err != nil {
		t.Fatal(err)
	}
	worlds, err := LocalWorldDataPackSettings(instance)
	if err != nil || len(worlds) != 1 || worlds[0].Name != "World" || !worlds[0].Editable ||
		!reflect.DeepEqual(worlds[0].Packs, []WorldDataPack{changes[1], changes[0]}) {
		t.Fatalf("data pack settings = %+v, %v", worlds, err)
	}
	data, tags, err := readWorldMetadata(file)
	if err != nil || !bytes.Contains(data, extra) {
		t.Fatalf("unrelated data pack metadata missing: %v", err)
	}
	on, off, err := readDataPackLists(data, tags["DataPacks"])
	if err != nil || !reflect.DeepEqual(on.ids, []string{"vanilla", "mod/example", dataPackID(directory)}) ||
		!reflect.DeepEqual(off.ids, []string{dataPackID(archive)}) {
		t.Fatalf("unexpected config: %+v, %+v, %v", on.ids, off.ids, err)
	}
	if err := EditWorldDataPacks(instance, "test", []WorldDataPack{{Name: archive, Mode: "auto"}}); err != nil {
		t.Fatal(err)
	}
	worlds, err = LocalWorldDataPackSettings(instance)
	if err != nil || len(worlds) != 1 || len(worlds[0].Packs) != 2 || worlds[0].Packs[1].Mode != "auto" {
		t.Fatalf("default pack status not restored: %+v, %v", worlds, err)
	}
	if err := EditWorldDataPacks(instance, "test", []WorldDataPack{{Name: "../other.zip", Mode: "enabled"}}); err == nil {
		t.Fatal("a pack outside the world's datapacks directory was accepted")
	}
}
