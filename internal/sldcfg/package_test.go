package sldcfg

import (
	"bytes"
	"compress/gzip"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"image"
	"image/png"
	"testing"
)

func TestOpenRejectsUntrustedBeforeDecompression(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	file, err := Seal(Package{Name: "Тема", Settings: Default()}, key)
	if err != nil {
		t.Fatal(err)
	}
	other, _, _ := ed25519.GenerateKey(rand.Reader)
	if _, err = Open(file, other); !errors.Is(err, ErrSignature) {
		t.Fatalf("wrong key: %v", err)
	}
	file[headerSize] ^= 0xff
	if _, err = Open(file, pub); !errors.Is(err, ErrSignature) {
		t.Fatalf("altered compressed data: %v", err)
	}
	file[headerSize] ^= 0xff
	if _, err = Open(append(file, 0), pub); err == nil {
		t.Fatal("accepted trailing data")
	}
}

func TestNavigationSlidersRoundTrip(t *testing.T) {
	settings := Default()
	settings.NavOpacity = 65
	settings.NavBlur = 18
	settings.NavHeight = 70
	settings.NavTabWidth = 162
	settings.NavTabHeight = 44
	settings.NavTabRadius = 0
	settings.NavTabGap = 22
	settings.NavTextSize = 18
	pack := Package{Name: "Навигация", Settings: settings}
	draft, err := MarshalDraft(pack)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenDraft(draft); err != nil || got.Settings != settings {
		t.Fatalf("navigation settings lost in draft: %v", err)
	}
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signed, err := Seal(pack, key)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := Open(signed, pub); err != nil || got.Settings != settings {
		t.Fatalf("navigation settings lost in signed theme: %v", err)
	}
}

func TestCreatorVersionRoundTrip(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	theme := Package{Name: "Theme", CreatorVersion: "0.9.25.6", Settings: Default()}
	draft, err := MarshalDraft(theme)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := OpenDraft(draft)
	if err != nil || decoded.FormatVersion != 6 || decoded.CreatorVersion != theme.CreatorVersion {
		t.Fatalf("creator version lost in draft: %q, format %d, %v", decoded.CreatorVersion, decoded.FormatVersion, err)
	}
	signed, err := Seal(decoded, key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Open(signed, pub)
	if err != nil || verified.FormatVersion != 6 || verified.CreatorVersion != theme.CreatorVersion {
		t.Fatalf("creator version lost after signing: %q, format %d, %v", verified.CreatorVersion, verified.FormatVersion, err)
	}
	theme.CreatorVersion = ""
	legacy, err := Seal(theme, key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err = Open(legacy, pub)
	if err != nil || verified.FormatVersion >= 6 || verified.CreatorVersion != "" {
		t.Fatalf("unexpected legacy creator version: %q, format %d, %v", verified.CreatorVersion, verified.FormatVersion, err)
	}
}

func TestBackgroundFiltersRoundTrip(t *testing.T) {
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	settings := Default()
	settings.BackgroundBrightness = 135
	settings.BackgroundContrast = 82
	settings.BackgroundHue = 270
	settings.BackgroundGrayscale = 15
	settings.BackgroundSepia = 38
	settings.BackgroundOpacity = 0
	settings.BackgroundSaturation = 225
	pack := Package{Name: "Background", CreatorVersion: "0.9.25.8", Settings: settings}
	draft, err := MarshalDraft(pack)
	if err != nil {
		t.Fatal(err)
	}
	fromDraft, err := OpenDraft(draft)
	if err != nil || fromDraft.Settings != settings || fromDraft.FormatVersion != 7 {
		t.Fatalf("background filters lost in draft: %v", err)
	}
	signed, err := Seal(fromDraft, key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Open(signed, pub)
	if err != nil || verified.Settings != settings || verified.FormatVersion != 7 || verified.CreatorVersion != pack.CreatorVersion {
		t.Fatalf("background filters lost after signing: %v", err)
	}
	pack.Settings.BackgroundHue = 361
	if _, err := MarshalDraft(pack); err == nil {
		t.Fatal("invalid hue accepted")
	}
}

func TestWaveAppearanceRoundTrip(t *testing.T) {
	public, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	settings := Default()
	settings.BackgroundEffect = "wave"
	settings.WaveColor = "#73bfc2"
	settings.WaveIntensity = 82
	settings.WaveAmplitude = 150
	settings.WaveTrail = 0
	settings.MotionSpeed = 25
	settings.Animate = false
	pack := Package{Name: "Wave", Settings: settings}
	draft, err := MarshalDraft(pack)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err := OpenDraft(draft)
	if err != nil || decoded.FormatVersion != 8 || decoded.Settings != settings {
		t.Fatalf("wave settings lost in draft: %+v, %v", decoded.Settings, err)
	}
	file, err := Seal(decoded, key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err := Open(file, public)
	if err != nil || verified.FormatVersion != 8 || verified.Settings != settings {
		t.Fatalf("wave settings lost in signed theme: %+v, %v", verified.Settings, err)
	}
	pack.Settings.WaveIntensity = 131
	if _, err := MarshalDraft(pack); err == nil {
		t.Fatal("accepted out-of-range wave intensity")
	}
	pack.Settings = settings
	pack.Settings.WaveOffset = 125
	draft, err = MarshalDraft(pack)
	if err != nil {
		t.Fatal(err)
	}
	decoded, err = OpenDraft(draft)
	if err != nil || decoded.FormatVersion != 9 || decoded.Settings != pack.Settings {
		t.Fatalf("wave position lost in draft: %+v, %v", decoded.Settings, err)
	}
	file, err = Seal(decoded, key)
	if err != nil {
		t.Fatal(err)
	}
	verified, err = Open(file, public)
	if err != nil || verified.FormatVersion != 9 || verified.Settings != pack.Settings {
		t.Fatalf("wave position lost in signed theme: %+v, %v", verified.Settings, err)
	}
	pack.Settings.WaveOffset = 241
	if _, err := MarshalDraft(pack); err == nil {
		t.Fatal("accepted out-of-range wave position")
	}
}

func TestSignedMalformedPayloads(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	good, err := encode(Package{Name: "Theme", Settings: Default()}, 2)
	if err != nil {
		t.Fatal(err)
	}
	member := func(data []byte) []byte {
		var buf bytes.Buffer
		w := gzip.NewWriter(&buf)
		_, _ = w.Write(data)
		_ = w.Close()
		return buf.Bytes()
	}
	framed := func(payload []byte) []byte {
		file := append(make([]byte, headerSize), payload...)
		copy(file, Magic)
		binary.BigEndian.PutUint16(file[4:6], 2)
		binary.BigEndian.PutUint32(file[lengthOffset:headerSize], uint32(len(payload)))
		copy(file[signatureOffset:lengthOffset], ed25519.Sign(key, signedBytes(file)))
		return file
	}
	badLengths := append([]byte(nil), good...)
	binary.BigEndian.PutUint32(badLengths[len(badLengths)-12:], MaxBackground+1)
	for name, payload := range map[string][]byte{
		"decompression limit":        member(make([]byte, maxPlain+1)),
		"multiple gzip members":      append(member(good), member(nil)...),
		"extra asset":                member(append(good, 1)),
		"asset length out of bounds": member(badLengths),
		"truncated settings":         member(good[:10]),
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Open(framed(payload), pub); err == nil {
				t.Fatal("accepted malformed signed payload")
			}
		})
	}
}

func TestRoundTripAndValidateResources(t *testing.T) {
	pub, key, _ := ed25519.GenerateKey(rand.Reader)
	var pngBytes bytes.Buffer
	if err := png.Encode(&pngBytes, image.NewNRGBA(image.Rect(0, 0, 8, 8))); err != nil {
		t.Fatal(err)
	}
	imageData := pngBytes.Bytes()
	p := Package{Name: "Моя тема", Settings: Default(), Background: imageData, Icon: imageData}
	p.Settings.Accent = "#89ac45"
	p.Settings.GradientAngle = 360
	p.Settings.GradientShape = "radial"
	p.Settings.Borders = true
	p.Settings.TextScale = 115
	p.Settings.TextWeight = 750
	p.Settings.ButtonRadius = 22
	p.Settings.GradientX = 75
	p.Settings.GradientSize = 145
	p.Settings.BackgroundBlur = 7
	p.Settings.BackgroundSaturation = 75
	p.Settings.BackgroundZoom = 120
	p.Settings.MotionSpeed = 80
	p.Settings.Theme = "midnight"
	p.Settings.NavColor = "#e4e9f0"
	p.Settings.NavActive = "#263c55"
	p.Settings.ProfileColor = "#ffffff"
	p.Settings.ProfileText = "#172838"
	file, err := Seal(p, key)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Open(file, pub)
	if err != nil || got.Settings != p.Settings || got.Name != p.Name || !bytes.Equal(got.Icon, p.Icon) || !bytes.Equal(got.Background, p.Background) {
		t.Fatal("roundtrip", err)
	}
	draft, err := MarshalDraft(p)
	if err != nil {
		t.Fatal(err)
	}
	if draftPack, err := OpenDraft(draft); err != nil || draftPack.Settings != p.Settings {
		t.Fatal("section colors lost in draft", err)
	}
	if binary.BigEndian.Uint16(file[4:6]) != 3 {
		t.Fatal("new settings must use the extended format")
	}
	p.Settings.BackgroundEdgeBlur = 18
	draft, err = MarshalDraft(p)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := OpenDraft(draft); err != nil || got.Settings != p.Settings {
		t.Fatalf("edge blur lost in draft: %v", err)
	}
	file, err = Seal(p, key)
	if err != nil {
		t.Fatal(err)
	}
	if binary.BigEndian.Uint16(file[4:6]) != 5 {
		t.Fatal("edge blur must use format version 5")
	}
	if got, err := Open(file, pub); err != nil || got.Settings != p.Settings {
		t.Fatalf("edge blur lost in signed theme: %v", err)
	}
	p.Icon = append(p.Icon, []byte("extra content")...)
	if _, err = Seal(p, key); err == nil {
		t.Fatal("accepted non-PNG suffix")
	}
	p.Icon = nil
	p.Settings.Font = "custom"
	p.Font = []byte("not a font")
	if _, err = Seal(p, key); err == nil {
		t.Fatal("accepted a non-font")
	}
	p.Settings.Font = "default"
	p.Font = nil
	p.Settings.Background = "../../outside"
	if _, err = Seal(p, key); err == nil {
		t.Fatal("accepted a filename")
	}
}
