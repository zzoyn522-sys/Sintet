package main

import (
	"bytes"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func TestOverlaySkinHeadIncludesHat(t *testing.T) {
	skin := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	for y := 8; y < 16; y++ {
		for x := 8; x < 16; x++ {
			skin.SetNRGBA(x, y, color.NRGBA{R: 180, A: 255})
		}
	}
	skin.SetNRGBA(40, 8, color.NRGBA{G: 200, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, skin); err != nil {
		t.Fatal(err)
	}
	value, err := overlayAvatarPNG(data.Bytes(), true)
	if err != nil {
		t.Fatal(err)
	}
	rgba, err := hex.DecodeString(value)
	if err != nil || len(rgba) != 36*36*4 {
		t.Fatalf("invalid RGBA size: %d, %v", len(rgba), err)
	}
	if !bytes.Equal(rgba[:4], []byte{0, 200, 0, 255}) {
		t.Fatalf("hat not composited: %v", rgba[:4])
	}
	if !bytes.Equal(rgba[len(rgba)-4:], []byte{180, 0, 0, 255}) {
		t.Fatal("head crop incorrect")
	}
	if _, err := overlayAvatarPNG(data.Bytes()[:20], true); err == nil {
		t.Fatal("accepted truncated PNG")
	}
}
