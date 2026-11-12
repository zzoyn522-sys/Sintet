package workshop

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func TestLocalResourcePacksAndArchiveIcons(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "resourcepacks"), 0755)
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 180, G: 90, B: 140, A: 255})
	var icon bytes.Buffer
	png.Encode(&icon, img)
	var buffer bytes.Buffer
	writer := zip.NewWriter(&buffer)
	meta, _ := writer.Create("pack.mcmeta")
	json.NewEncoder(meta).Encode(map[string]any{"pack": map[string]any{"description": "Local resource pack", "pack_format": 34}})
	file, _ := writer.Create("pack.png")
	file.Write(icon.Bytes())
	writer.Close()
	os.WriteFile(filepath.Join(root, "resourcepacks", "custom.zip"), buffer.Bytes(), 0600)
	items, err := Scan(root, "resources")
	if err != nil || len(items) != 1 || items[0].Description != "Local resource pack" {
		t.Fatal("resource pack metadata missing", err)
	}
	data, err := Icon(root, "resources", "custom.zip")
	if err != nil || !bytes.Equal(data, icon.Bytes()) {
		t.Fatal("pack icon missing", err)
	}
	if _, err = Icon(root, "resources", "../outside.zip"); err == nil {
		t.Fatal("accepted path outside selected pack")
	}
}
func TestDuplicateLocalModIDBlocksInstallation(t *testing.T) {
	m, _, _ := fixture(t)
	target := Target{ID: "test", Game: "1.21.1", Loader: "fabric", Directory: t.TempDir()}
	os.MkdirAll(filepath.Join(target.Directory, "mods"), 0755)
	path := filepath.Join(target.Directory, "mods", "manually-renamed.jar")
	data := testJar(t, "MainMod")
	os.WriteFile(path, data, 0600)
	plan, err := m.Prepare(context.Background(), "modrinth", "root", "root-v", target)
	if err != nil {
		t.Fatal(err)
	}
	m.Start(context.Background(), plan.ID, true)
	if waitInstall(t, m).Error == "" {
		t.Fatal("installed a duplicate mod ID under a different filename")
	}
	actual, _ := os.ReadFile(path)
	if !bytes.Equal(actual, data) {
		t.Fatal("manual mod changed")
	}
}

func writeLocalArchive(t *testing.T, dir, name string, files map[string]string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	out, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(out)
	for entry, contents := range files {
		file, err := archive.Create(entry)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := file.Write([]byte(contents)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestLocalImportToggleAndRemove(t *testing.T) {
	dir, source := t.TempDir(), t.TempDir()
	jar := writeLocalArchive(t, source, "example.jar", map[string]string{
		"fabric.mod.json": `{"id":"example","name":"Example Mod","version":"1.2","description":"About the mod","authors":["Alix",{"name":"Sam"}]}`,
	})
	pack := writeLocalArchive(t, source, "textures.zip", map[string]string{
		"pack.mcmeta": `{"pack":{"min_format":[65,1],"max_format":69,"description":{"text":"Better","extra":[{"text":" textures"}]}}}`,
	})
	for _, tc := range []struct {
		kind, file, description, authors, format string
	}{
		{"mods", jar, "About the mod", "Alix, Sam", ""},
		{"resources", pack, "Better textures", "", "65.1 – 69"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			if count, err := AddLocalFiles(dir, tc.kind, []string{tc.file}); count != 1 || err != nil {
				t.Fatalf("imported %d files: %v", count, err)
			}
			items, err := Scan(dir, tc.kind)
			if err != nil || len(items) != 1 || !items[0].Enabled || items[0].Description != tc.description || items[0].Authors != tc.authors || items[0].PackFormat != tc.format {
				t.Fatalf("unexpected archive metadata: %+v, %v", items, err)
			}
			name := filepath.Base(tc.file)
			if err := ToggleLocal(dir, tc.kind, name, false); err != nil {
				t.Fatal(err)
			}
			items, err = Scan(dir, tc.kind)
			if err != nil || len(items) != 1 || items[0].Enabled || items[0].Filename != name+disabledSuffix {
				t.Fatalf("disabled file missing: %+v, %v", items, err)
			}
			if _, err := AddLocalFiles(dir, tc.kind, []string{tc.file}); err == nil {
				t.Fatal("imported duplicate of a disabled file")
			}
			if err := ToggleLocal(dir, tc.kind, items[0].Filename, true); err != nil {
				t.Fatal(err)
			}
			if err := RemoveLocal(dir, tc.kind, "../"+name); err == nil {
				t.Fatal("accepted path outside the selected folder")
			}
			if err := RemoveLocal(dir, tc.kind, name); err != nil {
				t.Fatal(err)
			}
			items, err = Scan(dir, tc.kind)
			if err != nil || len(items) != 0 {
				t.Fatalf("file was not removed: %+v, %v", items, err)
			}
			if _, err := os.Stat(tc.file); err != nil {
				t.Fatal("original file is missing", err)
			}
		})
	}
}

func TestLocalImportRejectsInvalidResourcePackBatch(t *testing.T) {
	dir, source := t.TempDir(), t.TempDir()
	pack := writeLocalArchive(t, source, "textures.zip", map[string]string{
		"pack.mcmeta": `{"pack":{"pack_format":34,"description":"Textures"}}`,
	})
	wrong := writeLocalArchive(t, source, "other.zip", map[string]string{"notes.txt": "not a pack"})
	if _, err := AddLocalFiles(dir, "resources", []string{pack, wrong}); err == nil {
		t.Fatal("accepted a zip without pack.mcmeta")
	}
	files, err := os.ReadDir(filepath.Join(dir, "resourcepacks"))
	if err != nil || len(files) != 0 {
		t.Fatalf("failed batch left files behind: %v, %v", files, err)
	}
}

func TestRemoveDisabledCatalogPackClearsRegistry(t *testing.T) {
	dir, source := t.TempDir(), t.TempDir()
	pack := writeLocalArchive(t, source, "textures.zip", map[string]string{
		"pack.mcmeta": `{"pack":{"pack_format":34,"description":"Textures"}}`,
	})
	if _, err := AddLocalFiles(dir, "resources", []string{pack}); err != nil {
		t.Fatal(err)
	}
	records := map[string]Installed{"modrinth:test": {Filename: "textures.zip", Provider: "modrinth"}}
	data, err := json.Marshal(records)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".sintet-resourcepacks.json"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ToggleLocal(dir, "resources", "textures.zip", false); err != nil {
		t.Fatal(err)
	}
	if err := RemoveLocal(dir, "resources", "textures.zip"+disabledSuffix); err != nil {
		t.Fatal(err)
	}
	if records, err := resourceRegistry(dir); err != nil || len(records) != 0 {
		t.Fatalf("catalog entry remained after deletion: %v, %v", records, err)
	}
	if files, err := os.ReadDir(filepath.Join(dir, "resourcepacks")); err != nil || len(files) != 0 {
		t.Fatalf("deleted pack left files behind: %v, %v", files, err)
	}
}
