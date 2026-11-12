package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
	"time"

	"sintet/internal/sldcfg"
)

func TestThemePreviewCapture(t *testing.T) {
	if os.Getenv("SINTET_TEST_THEME_PREVIEWS") != "1" {
		t.Skip("set SINTET_TEST_THEME_PREVIEWS=1 to exercise the browser renderer")
	}
	data, err := os.ReadFile("testdata/sintet-theme-example.sldcfg")
	if err != nil {
		t.Fatal(err)
	}
	pack, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	images, err := captureThemePreviews(ctx, pack)
	if err != nil {
		t.Fatal(err)
	}
	for _, screen := range themePreviewScreens {
		if len(images) < 4 {
			t.Fatalf("missing %s", screen)
		}
		length := int(binary.BigEndian.Uint32(images))
		images = images[4:]
		if length > len(images) {
			t.Fatalf("truncated %s", screen)
		}
		shot := images[:length]
		frame, err := jpeg.DecodeConfig(bytes.NewReader(shot))
		if err != nil || frame.Width != windowWidth || frame.Height != windowHeight {
			t.Fatalf("invalid %s: %v %v", screen, frame, err)
		}
		if output := os.Getenv("SINTET_TEST_THEME_PREVIEW_DIR"); output != "" {
			if err := os.WriteFile(filepath.Join(output, screen+".jpg"), shot, 0600); err != nil {
				t.Fatal(err)
			}
		}
		images = images[length:]
	}
	if len(images) != 0 {
		t.Fatal("trailing bytes")
	}
}
