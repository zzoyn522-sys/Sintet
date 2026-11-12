package main

import (
	"bytes"
	"os"
	"testing"
)

func TestOverlayFontUsesBundledInterOnlyWhenEnabled(t *testing.T) {
	app := &App{appearance: defaultAppearance(), profiles: &profileRoster{root: t.TempDir()}}

	if path := app.overlayFontPath(false); path != "" {
		t.Fatalf("disabled font override returned %q", path)
	}

	want, err := assets.ReadFile("frontend/assets/fonts/Inter-SemiBold.ttf")
	if err != nil {
		t.Fatal(err)
	}
	path := app.overlayFontPath(true)
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("failed to extract the bundled Inter font: %v", err)
	}

	if err := os.WriteFile(path, []byte("corrupt font cache"), 0600); err != nil {
		t.Fatal(err)
	}
	if repaired := app.overlayFontPath(true); repaired != path {
		t.Fatalf("expected the same cached font path, got %q", repaired)
	}
	if data, err := os.ReadFile(path); err != nil || !bytes.Equal(data, want) {
		t.Fatalf("failed to repair the bundled font cache: %v", err)
	}

	app.appearance.Font = "segoe"
	if path := app.overlayFontPath(true); path != "" {
		t.Fatalf("Segoe UI should use the native font, got %q", path)
	}
}
