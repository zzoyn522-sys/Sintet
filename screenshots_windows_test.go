package main

import (
	"image"
	"image/png"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"sintet/internal/launcher"
)

func TestScreenshotGalleryFileActions(t *testing.T) {
	dir := t.TempDir()
	a := &App{service: &launcher.Service{Root: dir}, settingsPath: filepath.Join(dir, "settings.json")}
	names := []string{
		filepath.Join("screenshots", "2026-11-21_10.30.15.png"),
		filepath.Join("instances", "build-demo", "screenshots", "2026-11-21_10.30.15_012_321.png"),
		filepath.Join("instances", "family", "fabric-1.21", "screenshots", "custom.png"),
	}
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		file, err := os.Create(path)
		if err != nil {
			t.Fatal(err)
		}
		err = png.Encode(file, image.NewRGBA(image.Rect(0, 0, 900, 450)))
		file.Close()
		if err != nil {
			t.Fatal(err)
		}
	}
	items, err := a.GameScreenshots()
	if err != nil || len(items) != 3 {
		t.Fatalf("screenshots: %+v, %v", items, err)
	}
	id := screenshotID(names[1])
	found := false
	for _, item := range items {
		if item.ID == id && item.Source == "overlay" {
			found = true
		}
	}
	if !found {
		t.Fatal("overlay screenshot missing")
	}
	req := httptest.NewRequest("GET", screenshotRoute+id+"?thumb=1", nil)
	out := httptest.NewRecorder()
	a.serveScreenshot(out, req)
	config, err := png.DecodeConfig(out.Body)
	if out.Code != 200 || err != nil || config.Width != 480 || config.Height != 240 {
		t.Fatalf("invalid thumbnail: %d, %+v, %v", out.Code, config, err)
	}
	if _, err := a.RenameGameScreenshot(id, "../outside"); err == nil {
		t.Fatal("unsafe rename accepted")
	}
	id, err = a.RenameGameScreenshot(id, "Новый снимок")
	if err != nil {
		t.Fatal(err)
	}
	trash, err := a.TrashGameScreenshot(id)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := a.GameScreenshots(); err != nil || len(got) != 2 {
		t.Fatalf("trash still listed: %+v %v", got, err)
	}
	if restored, err := a.RestoreGameScreenshot(trash); err != nil || restored != id {
		t.Fatalf("restore: %q %v", restored, err)
	}
	if _, err := a.TrashGameScreenshot(screenshotID(filepath.Join("..", "screenshots", "outside.png"))); err == nil {
		t.Fatal("unsafe delete accepted")
	}
}
