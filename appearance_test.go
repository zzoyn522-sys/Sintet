package main

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"sintet/internal/sldcfg"
)

func TestAppearanceRestoresImportedBackgroundAndServesVideoRanges(t *testing.T) {
	root := t.TempDir()
	app := &App{
		appearance:     defaultAppearance(),
		appearancePath: filepath.Join(root, "appearance.json"),
		backgroundDir:  filepath.Join(root, "Backgrounds"),
	}
	source := filepath.Join(root, "wallpaper.webm")
	if err := os.WriteFile(source, []byte("test video bytes"), 0600); err != nil {
		t.Fatal(err)
	}
	name, err := app.importAppearanceBackground(source)
	if err != nil {
		t.Fatal(err)
	}
	settings := app.GetAppearance()
	settings.Background = name
	settings.Theme = "midnight"
	settings.Opacity = 60
	settings.Gradient = false
	settings.BackgroundEdgeBlur = 18
	if err = app.SaveAppearance(settings); err != nil {
		t.Fatal(err)
	}
	if err = os.Remove(source); err != nil {
		t.Fatal(err)
	}
	restarted := &App{
		appearance:    readAppearance(app.appearancePath),
		backgroundDir: app.backgroundDir,
	}
	if restarted.GetAppearance() != settings {
		t.Fatal("saved appearance was not restored")
	}
	handler := restarted.serveAppearanceBackground(http.NotFoundHandler())
	request := httptest.NewRequest(http.MethodGet, backgroundRoute+name, nil)
	request.Header.Set("Range", "bytes=0-3")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusPartialContent || response.Body.String() != "test" ||
		response.Header().Get("Content-Type") != "video/webm" {
		t.Fatalf("unexpected range response: %d %v", response.Code, response.Header())
	}
	request = httptest.NewRequest(http.MethodGet, backgroundRoute+"../appearance.json", nil)
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusNotFound {
		t.Fatal("the background route exposed a file outside its directory")
	}
}

func TestApprovedDraftReusesOriginalSlot(t *testing.T) {
	root := t.TempDir()
	data, err := os.ReadFile("testdata/sintet-theme-example.sldcfg")
	if err != nil {
		t.Fatal(err)
	}
	p, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		t.Fatal(err)
	}
	a := &App{appearance: p.Settings, appearanceActive: 1, appearanceSlots: []AppearanceSlot{{Name: p.Name, Settings: p.Settings}},
		appearancePath: filepath.Join(root, "appearance.json"), backgroundDir: filepath.Join(root, "Backgrounds")}
	_, _, draft, err := a.appearanceDraft(0, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = a.importTheme(draft); err == nil {
		t.Fatal("unsigned draft passed the importer")
	}
	a.appearanceSlots[0].Draft = fmt.Sprintf("%x", sha256.Sum256(draft))
	if err = a.importTheme(data); err != nil {
		t.Fatal(err)
	}
	got := a.GetAppearanceLibrary()
	if got.Slots[0].Draft != "" || got.Slots[0].Package == "" || got.Slots[1].Name != "" || got.ActiveSlot != 1 || got.Current != p.Settings {
		t.Fatal("approved theme was not linked to the existing slot")
	}
}

func TestAppearanceDraftUsesOnlySelectedMedia(t *testing.T) {
	root := t.TempDir()
	settings := defaultAppearance()
	settings.FontFile = "0123456789abcdef0123456789abcdef.ttf" // A remembered custom font is inactive.
	settings.WaveOffset = 34
	a := &App{appearance: settings, appearanceActive: 1, backgroundDir: filepath.Join(root, "Backgrounds")}
	video := []byte("local video")
	videoName, err := a.writeAppearanceAsset(".mp4", int64(len(video)), bytes.NewReader(video), false)
	if err != nil {
		t.Fatal(err)
	}
	a.appearanceSlots = []AppearanceSlot{{Name: "Тема", Settings: settings}}
	a.appearanceSlots[0].Settings.Background = videoName
	if _, _, _, err := a.appearanceDraft(0, nil); err == nil || !strings.Contains(err.Error(), "видеофон") {
		t.Fatalf("exporting the saved video must explain the restriction: %v", err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for _, fixture := range []struct{ ext, data string }{
		{"", ""},
		{".webp", "UklGRhwAAABXRUJQVlA4TA8AAAAvAUAAAAcQ/Y/+ByKi/wEA"},
		{".gif", "R0lGODdhAgACAIEAAP8AAAAAAAAAAAAAACwAAAAAAgACAAAIBgABCAQQEAA7"},
	} {
		t.Run(fixture.ext, func(t *testing.T) {
			current := settings
			if fixture.data != "" {
				imageData, err := base64.StdEncoding.DecodeString(fixture.data)
				if err != nil {
					t.Fatal(err)
				}
				current.Background, err = a.writeAppearanceAsset(fixture.ext, int64(len(imageData)), bytes.NewReader(imageData), false)
				if err != nil {
					t.Fatal(err)
				}
				a.draftBackground = current.Background
			}
			_, _, draft, err := a.appearanceDraft(-1, &current)
			if err != nil {
				t.Fatal(err)
			}
			pack, err := sldcfg.OpenDraft(draft)
			if err != nil {
				t.Fatal(err)
			}
			signed, err := sldcfg.Seal(pack, key)
			if err != nil {
				t.Fatal(err)
			}
			opened, err := sldcfg.Open(signed, pub)
			if err != nil || opened.Settings.Font != "default" || len(opened.Font) != 0 || opened.Settings.WaveOffset != 34 {
				t.Fatalf("signed settings changed or included the inactive font: %v", err)
			}
			if fixture.data == "" {
				if len(opened.Background) != 0 {
					t.Fatal("export used the background from the saved slot")
				}
				return
			}
			img, err := png.Decode(bytes.NewReader(opened.Background))
			if err != nil || img.Bounds() != image.Rect(0, 0, 2, 2) {
				t.Fatalf("static background did not survive as PNG: %v", err)
			}
			if got := color.NRGBAModel.Convert(img.At(0, 0)); got != (color.NRGBA{R: 255, A: 255}) {
				t.Fatalf("background changed color: %v", got)
			}
		})
	}
}

func TestAppearanceLibraryMigrationAndActiveSlot(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "appearance.json")
	theme := defaultAppearance()
	theme.Accent = "#7788aa"
	writeJSON := func(path string, value any) {
		t.Helper()
		data, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	writeJSON(path, theme)
	writeJSON(filepath.Join(root, "appearance-slots.json"), []AppearanceSlot{{Name: "Первая", Settings: theme}, {Name: "Вторая", Settings: theme}})
	old := readAppearanceLibrary(path)
	if old.ActiveSlot != 1 || old.Slots[1].Name != "Вторая" {
		t.Fatalf("legacy migration: %+v", old)
	}
	a := &App{appearance: old.Current, appearanceSlots: old.Slots, appearanceActive: old.ActiveSlot, appearancePath: path, backgroundDir: filepath.Join(root, "Backgrounds")}
	if err := a.SetActiveAppearanceSlot(1); err != nil {
		t.Fatal(err)
	}
	if got := readAppearanceLibrary(path); got.ActiveSlot != 2 {
		t.Fatal("identical presets were confused")
	}
	if err := a.RenameAppearanceSlot(1, "Ночь"); err != nil {
		t.Fatal(err)
	}
	theme.Accent = "#edbbaa"
	if err := a.SaveAppearance(theme); err != nil {
		t.Fatal(err)
	}
	got := readAppearanceLibrary(path)
	if got.Current != theme || got.Slots[1].Settings != theme || got.Slots[1].Name != "Ночь" || got.Slots[0].Settings == theme {
		t.Fatal("editing settings didn't update only the active saved theme")
	}
	if err := a.SetActiveAppearanceSlot(-1); err != nil {
		t.Fatal(err)
	}
	got = readAppearanceLibrary(path)
	if got.ActiveSlot != 0 || got.Current != defaultAppearance() || got.Slots[1].Settings != theme {
		t.Fatal("disabling lost the preset")
	}
	if err := a.SetActiveAppearanceSlot(1); err != nil {
		t.Fatal(err)
	}
	if err := a.ClearAppearanceSlot(1); err != nil {
		t.Fatal(err)
	}
	got = readAppearanceLibrary(path)
	if got.ActiveSlot != 0 || got.Current != defaultAppearance() || got.Slots[1].Name != "" || got.Slots[0].Name != "Первая" {
		t.Fatal("removing the active theme didn't restore defaults")
	}
}

func TestThemeIconCleanup(t *testing.T) {
	root := t.TempDir()
	a := &App{appearance: defaultAppearance(), appearancePath: filepath.Join(root, "appearance.json"), backgroundDir: filepath.Join(root, "Backgrounds")}
	if err := a.AddCurrentAppearance("Моя тема"); err != nil {
		t.Fatal(err)
	}
	var imageBytes bytes.Buffer
	icon := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	icon.Set(0, 0, color.NRGBA{R: 255, A: 255})
	if err := png.Encode(&imageBytes, icon); err != nil {
		t.Fatal(err)
	}
	if err := a.setAppearanceIcon(0, imageBytes.Bytes()); err != nil {
		t.Fatal(err)
	}
	name := readAppearanceLibrary(a.appearancePath).Slots[0].Icon
	if !cachedAvatarName.MatchString(name) {
		t.Fatal("icon metadata was not persisted")
	}
	if err := a.ClearAppearanceIcon(0); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(a.themeIconsDir(), name)); !os.IsNotExist(err) {
		t.Fatal("unused icon was not cleaned up", err)
	}
}

func TestSignedThemeImportKeepsOriginalUntilEdited(t *testing.T) {
	root := t.TempDir()
	a := &App{appearance: defaultAppearance(), appearancePath: filepath.Join(root, "appearance.json"), backgroundDir: filepath.Join(root, "Backgrounds")}
	data, err := os.ReadFile("testdata/sintet-theme-example.sldcfg")
	if err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Clone(data)
	tampered[len(tampered)-1] ^= 1
	if err = a.importTheme(tampered); err == nil || a.GetAppearanceLibrary().Slots[0].Name != "" {
		t.Fatal("accepted a tampered theme")
	}
	if err = a.importTheme(data); err != nil {
		t.Fatal(err)
	}
	if err = a.importTheme(data); err == nil {
		t.Fatal("accepted a duplicate theme")
	}
	slot := readAppearanceLibrary(a.appearancePath).Slots[0]
	if slot.Name == "" || slot.Package == "" {
		t.Fatal("missing saved theme")
	}
	original, err := a.signedTheme(0, nil)
	if err != nil || !bytes.Equal(data, original) {
		t.Fatal("signed original not available", err)
	}
	if err = a.SetActiveAppearanceSlot(0); err != nil {
		t.Fatal(err)
	}
	changed := a.GetAppearance()
	changed.Dim++
	if err = a.SaveAppearance(changed); err != nil {
		t.Fatal(err)
	}
	if _, err = a.signedTheme(0, nil); err != errUnsignedTheme {
		t.Fatal("modified theme retained trusted status", err)
	}
	if _, err = os.Stat(a.packagePath(slot.Package)); !os.IsNotExist(err) {
		t.Fatal("unreferenced archive was not cleaned up", err)
	}
}

func TestLegacyThemeUsesDefaultsForExtendedSettings(t *testing.T) {
	data, err := os.ReadFile("testdata/sintet-theme-v1.sldcfg")
	if err != nil {
		t.Fatal(err)
	}
	p, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		t.Fatal(err)
	}
	defaults := sldcfg.Default()
	if p.Settings.TextScale != defaults.TextScale || p.Settings.GradientSize != defaults.GradientSize || p.Settings.BackgroundZoom != defaults.BackgroundZoom {
		t.Fatal("version 1 theme lost defaults")
	}
	if p.Settings.BackgroundEffect != "gradient" {
		t.Fatal("version 1 theme changed its background effect")
	}
}

func TestWaveDefaultPreservesLegacySavedThemes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "appearance.json")
	settings := sldcfg.Default()
	settings.BackgroundEffect = ""
	settings.Accent = "#aabbcc"
	write := func(document any) {
		t.Helper()
		data, err := json.Marshal(document)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(settings)
	if got := readAppearanceLibrary(path); got.Current.BackgroundEffect != "wave" || got.Current.CanvasColor != "#0d0d0d" || got.Current.Accent != settings.Accent {
		t.Fatal("unmodified legacy default did not migrate to the wave")
	}
	slots, err := json.Marshal([]AppearanceSlot{{Name: "Saved", Settings: settings}})
	if err != nil {
		t.Fatal(err)
	}
	write(appearanceStorage{Appearance: settings, Library: &appearanceLibraryStorage{ActiveSlot: 1, Slots: slots}})
	if got := readAppearanceLibrary(path); got.ActiveSlot != 1 || got.Current.BackgroundEffect != "gradient" || got.Slots[0].Settings != got.Current {
		t.Fatal("active legacy slot did not preserve its settings")
	}
	settings.BackgroundEffect = "gradient"
	write(settings)
	if got := readAppearanceLibrary(path); got.Current.BackgroundEffect != "gradient" {
		t.Fatal("explicitly selected gradient was replaced")
	}
}
