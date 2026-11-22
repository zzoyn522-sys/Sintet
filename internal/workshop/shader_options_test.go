package workshop

import (
	"archive/zip"
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestShaderSettingsReadAndSaveIrisSidecar(t *testing.T) {
	dir := t.TempDir()
	folder := filepath.Join(dir, "shaderpacks")
	if err := os.Mkdir(folder, 0700); err != nil {
		t.Fatal(err)
	}
	var contents bytes.Buffer
	archive := zip.NewWriter(&contents)
	for name, text := range map[string]string{
		"Example/shaders/shaders.properties": "screen=\\\n  <profile> [DETAIL] *\nscreen.DETAIL=SOFT LIGHT\nsliders=LIGHT\nprofile.Low=!SOFT LIGHT=1\nprofile.High=profile.Low SOFT LIGHT:3\n",
		"Example/shaders/lang/ru_RU.lang":    "screen.DETAIL=Детали\noption.SOFT=Мягкие тени\nvalue.LIGHT.2=Среднее\n",
		"Example/shaders/lib/options.glsl":   "#define SOFT // Shadows\n#ifdef SOFT\n#endif\n#define LIGHT 2 // [1 2 3] Brightness\nconst int shadowMapResolution = 1024; // [512 1024]\n#define UNSUPPORTED\n",
		"Example/shaders/final.fsh":          "#include \"/lib/options.glsl\"\n",
	} {
		entry, err := archive.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := entry.Write([]byte(text)); err != nil {
			t.Fatal(err)
		}
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	const filename = "Example.zip.disabled"
	pack := filepath.Join(folder, filename)
	if err := os.WriteFile(pack, contents.Bytes(), 0600); err != nil {
		t.Fatal(err)
	}
	config := filepath.Join(folder, "Example.zip.txt")
	if err := os.WriteFile(config, []byte("# existing\nSOFT=false\nunknown=keep\\u0020me\n"), 0600); err != nil {
		t.Fatal(err)
	}
	view, err := ReadShaderOptions(dir, filename, "ru")
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Groups) != 2 || view.Groups[0].Options[0].ID != "shadowMapResolution" || view.Groups[1].Name != "Детали" {
		t.Fatalf("unexpected shader menu: %+v", view.Groups)
	}
	soft, light := view.Groups[1].Options[0], view.Groups[1].Options[1]
	if !soft.Toggle || soft.Value != "false" || soft.Name != "Мягкие тени" || !light.Slider || light.Labels["2"] != "Среднее" {
		t.Fatalf("unexpected options: %+v / %+v", soft, light)
	}
	if len(view.Profiles) != 2 || view.Profiles[1].Values["SOFT"] != "true" || view.Profiles[1].Values["LIGHT"] != "3" {
		t.Fatalf("unexpected profiles: %+v", view.Profiles)
	}
	if err := SaveShaderOptions(dir, filename, view.Revision, map[string]string{"LIGHT": "9"}); err == nil {
		t.Fatal("saved unsupported value")
	}
	if err := SaveShaderOptions(dir, filename, view.Revision, map[string]string{"LIGHT": "3", "SOFT": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveShaderOptions(dir, filename, view.Revision, map[string]string{"LIGHT": "1"}); err == nil {
		t.Fatal("stale editor overwrote settings")
	}
	reopened, err := ReadShaderOptions(dir, filename, "ru")
	if err != nil || reopened.Groups[1].Options[1].Value != "3" || reopened.Groups[1].Options[0].Value != "true" {
		t.Fatalf("settings did not round trip: %+v, %v", reopened, err)
	}
	saved, err := os.ReadFile(config)
	if err != nil || !strings.Contains(string(saved), "unknown = keep me") || strings.Contains(string(saved), "SOFT =") {
		t.Fatalf("sidecar changes: %q, %v", saved, err)
	}
	unchanged, err := os.ReadFile(pack)
	if err != nil || !bytes.Equal(unchanged, contents.Bytes()) {
		t.Fatal("shader archive was changed")
	}
	if _, err := ReadShaderOptions(dir, "../Example.zip", "ru"); err == nil {
		t.Fatal("path outside shaderpacks accepted")
	}
}
