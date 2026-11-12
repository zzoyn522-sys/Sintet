package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"sintet/internal/identity"
)

func (a *App) overlayBuildIcon(icon string) (preset, pixels string) {
	switch icon {
	case "grass", "sword", "crystal", "copper", "chest", "end":
		return icon, ""
	}
	if name := customBuildIconName(icon); name != "" {
		file, err := os.Open(filepath.Join(a.profiles.root, "BuildIcons", name))
		if err == nil {
			defer file.Close()
			const maxIconBytes = 64 << 10
			data, err := io.ReadAll(io.LimitReader(file, maxIconBytes+1))
			if err == nil && len(data) <= maxIconBytes {
				if rgba, err := overlayAvatarPNG(data, false); err == nil {
					return "grass", rgba
				}
			}
		}
	}
	return "grass", ""
}

// Only a small RGBA image crosses into the game process, never a session token.
func (a *App) overlayAvatar(account *identity.Account) string {
	target, parseErr := url.Parse(account.Skin.URL)
	if parseErr == nil && target.Hostname() == "textures.minecraft.net" && target.Scheme == "http" {
		target.Scheme = "https"
	}
	if parseErr == nil && target.Scheme == "https" && target.User == nil {
		client := http.Client{Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
		if response, err := client.Get(target.String()); err == nil {
			data, readErr := io.ReadAll(io.LimitReader(response.Body, (512<<10)+1))
			response.Body.Close()
			if response.StatusCode == http.StatusOK && readErr == nil && len(data) <= 512<<10 {
				if rgba, err := overlayAvatarPNG(data, true); err == nil {
					return rgba
				}
			}
		}
	}
	if data, _, err := a.avatarBytes(account.AvatarURL); err == nil {
		if rgba, err := overlayAvatarPNG(data, false); err == nil {
			return rgba
		}
	}
	return ""
}

func overlayAvatarPNG(data []byte, skin bool) (string, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 512 || config.Height > 512 {
		return "", fmt.Errorf("invalid overlay image")
	}
	if skin && (config.Width != 64 || config.Height != 32 && config.Height != 64) {
		return "", fmt.Errorf("invalid skin dimensions")
	}
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return "", err
	}
	if skin {
		head := image.NewNRGBA(image.Rect(0, 0, 8, 8))
		draw.Draw(head, head.Bounds(), source, image.Pt(8, 8), draw.Src)
		draw.Draw(head, head.Bounds(), source, image.Pt(40, 8), draw.Over)
		source = head
	}
	output := image.NewNRGBA(image.Rect(0, 0, 36, 36))
	for y := 0; y < 36; y++ {
		for x := 0; x < 36; x++ {
			output.Set(x, y, source.At(x*source.Bounds().Dx()/36, y*source.Bounds().Dy()/36))
		}
	}
	return hex.EncodeToString(output.Pix), nil
}
