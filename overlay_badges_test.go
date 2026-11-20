package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestOverlayBadgeCatalog(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.SetNRGBA(0, 0, color.NRGBA{R: 240, G: 120, A: 255})
	var pngData bytes.Buffer
	if err := png.Encode(&pngData, img); err != nil {
		t.Fatal(err)
	}
	validPNG := base64.StdEncoding.EncodeToString(pngData.Bytes())
	pack := func(label string) overlayBadgeResponse {
		t.Helper()
		raw, _ := json.Marshal(map[string]any{"items": []map[string]string{
			{"id": "developer", "label": label, "png": validPNG},
			{"id": "INVALID", "label": label, "png": validPNG},
			{"id": "tester", "label": label, "png": "not a PNG"},
		}})
		catalog, err := decodeOverlayBadges(bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		return catalog
	}
	catalog := pack("Developer")
	var icons map[string]overlayBadgeIcon
	if err := json.Unmarshal(catalog.Icons, &icons); err != nil {
		t.Fatal(err)
	}
	icon := icons["developer"]
	if len(icons) != 1 || icon.Label != "Developer" || len(icon.Pixels) != 24*24*8 || icon.Pixels[:8] != "f07800ff" {
		t.Fatal("the overlay did not receive the validated icon")
	}
	if pack("Developer").Revision != catalog.Revision || pack("Admin").Revision == catalog.Revision {
		t.Fatal("the catalog revision does not track content")
	}
}
