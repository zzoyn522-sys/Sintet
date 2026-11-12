package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
)

func TestAvatarGlowUsesImageColorsAndRestrictsFetchOrigin(t *testing.T) {
	avatar := image.NewNRGBA(image.Rect(0, 0, 24, 24))
	colors := []color.NRGBA{{R: 255, A: 255}, {G: 255, A: 255}, {B: 255, A: 255}}
	for y := 0; y < 24; y++ {
		for x := 0; x < 24; x++ {
			avatar.SetNRGBA(x, y, colors[y/8])
		}
	}
	var data bytes.Buffer
	if err := png.Encode(&data, avatar); err != nil {
		t.Fatal(err)
	}
	got, err := readAvatarGlow(data.Bytes())
	if want := [3]string{"#d50909", "#09d509", "#0909d5"}; err != nil || got != want {
		t.Fatalf("palette=%v, err=%v", got, err)
	}
	path := "/api/textures/avatar/ba815a63-4176-45db-8e70-12e3801c1207/" + strings.Repeat("a", 64) + ".png"
	if !allowedAvatarTexture("http://127.0.0.1:3033", "http://127.0.0.1:3033"+path) {
		t.Fatal("local avatar rejected")
	}
	for _, address := range []string{"https://example.com" + path, "http://127.0.0.1:3040" + path, "http://127.0.0.1:3033" + path + "?url=other", "http://127.0.0.1:3033/api/auth/accounts"} {
		if allowedAvatarTexture("http://127.0.0.1:3033", address) {
			t.Fatalf("unsafe avatar accepted: %s", address)
		}
	}
}
