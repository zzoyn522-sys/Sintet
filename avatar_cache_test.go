package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestAvatarCacheSurvivesPortalOutage(t *testing.T) {
	var encoded bytes.Buffer
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	img.Set(0, 0, color.RGBA{R: 100, G: 50, B: 200, A: 255})
	if err := png.Encode(&encoded, img); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(encoded.Bytes())
	avatarPath := "/api/textures/avatar/12345678-1234-1234-1234-123456789abc/" + hex.EncodeToString(sum[:]) + ".png"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != avatarPath {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(encoded.Bytes())
	}))
	defer server.Close()
	root := t.TempDir()
	app := &App{profiles: &profileRoster{root: root, base: server.URL}}
	address := server.URL + avatarPath
	local, err := app.ResolveAvatar(address)
	if err != nil {
		t.Fatal(err)
	}
	server.Close()
	restarted := &App{profiles: &profileRoster{root: root, base: server.URL}}
	restored, err := restarted.ResolveAvatar(address)
	if err != nil || restored != local {
		t.Fatalf("avatar unavailable offline: %v", err)
	}
	request := httptest.NewRequest(http.MethodGet, local, nil)
	response := httptest.NewRecorder()
	restarted.serveLauncherAssets(http.NotFoundHandler()).ServeHTTP(response, request)
	if response.Code != 200 || !bytes.Equal(response.Body.Bytes(), encoded.Bytes()) {
		t.Fatal("cached avatar was not served")
	}
	if _, err = restarted.ResolveAvatar("https://unrelated.test" + avatarPath); err == nil {
		t.Fatal("accepted a different origin")
	}
}
