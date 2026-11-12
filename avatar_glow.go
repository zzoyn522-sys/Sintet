package main

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/png"
	"net/url"
	"regexp"
	"strings"
)

var avatarTexturePath = regexp.MustCompile(`^/api/textures/avatar/[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}/[a-f0-9]{64}\.png$`)

func allowedAvatarTexture(base, raw string) bool {
	origin, err := url.Parse(base)
	if err != nil {
		return false
	}
	target, err := url.Parse(raw)
	if err != nil {
		return false
	}
	return target.Host != "" && strings.EqualFold(target.Host, origin.Host) && target.Scheme == origin.Scheme &&
		(target.Scheme == "https" || (target.Scheme == "http" && target.Hostname() == "127.0.0.1")) &&
		target.User == nil && target.RawQuery == "" && target.Fragment == "" && avatarTexturePath.MatchString(target.EscapedPath())
}

func (a *App) AvatarGlow(address string) ([3]string, error) {
	data, _, err := a.avatarBytes(address)
	if err != nil {
		return [3]string{}, err
	}
	return readAvatarGlow(data)
}

func readAvatarGlow(data []byte) ([3]string, error) {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width > 512 || config.Height > 512 {
		return [3]string{}, errors.New("неподдерживаемая аватарка")
	}
	avatar, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return [3]string{}, err
	}
	return avatarGlow(avatar)
}

// Sample the top, middle, and bottom of the avatar, favoring visible and saturated pixels.
func avatarGlow(avatar image.Image) ([3]string, error) {
	type sample struct{ red, green, blue, weight uint64 }
	var bands [3]sample
	var all sample
	bounds := avatar.Bounds()
	for y := 0; y < 48; y++ {
		for x := 0; x < 48; x++ {
			r, g, b, a := avatar.At(bounds.Min.X+(2*x+1)*bounds.Dx()/96, bounds.Min.Y+(2*y+1)*bounds.Dy()/96).RGBA()
			if a < 8192 {
				continue
			}
			r, g, b = r*255/a, g*255/a, b*255/a
			weight := uint64((1 + (max(r, g, b)-min(r, g, b))/32) * (a / 8192))
			band := &bands[y/16]
			band.red += uint64(r) * weight
			band.green += uint64(g) * weight
			band.blue += uint64(b) * weight
			band.weight += weight
			all.red += uint64(r) * weight
			all.green += uint64(g) * weight
			all.blue += uint64(b) * weight
			all.weight += weight
		}
	}
	if all.weight == 0 {
		return [3]string{}, errors.New("на аватарке нет видимых цветов")
	}
	var palette [3]string
	for i, band := range bands {
		if band.weight == 0 {
			band = all
		}
		r, g, b := band.red/band.weight, band.green/band.weight, band.blue/band.weight
		// Lift dark colors slightly so they remain visible on charcoal.
		palette[i] = fmt.Sprintf("#%02x%02x%02x", (r*4+48)/5, (g*4+48)/5, (b*4+48)/5)
	}
	return palette, nil
}
