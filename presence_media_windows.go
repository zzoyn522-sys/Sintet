//go:build windows

package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"image"
	"image/png"
	"syscall"
	"unsafe"
)

type presenceMediaSample struct {
	generation    uint64
	width, height uint32
	title, artist [384]byte
	rgba          [96 * 96 * 4]byte
}

func (a *App) presenceMedia(enabled bool) (*presenceMusic, []byte) {
	a.overlayMu.Lock()
	bridge := a.overlay
	a.overlayMu.Unlock()
	if bridge == nil {
		return nil, nil
	}
	bridge.mu.Lock()
	defer bridge.mu.Unlock()
	if bridge.handle == 0 || bridge.mediaEnable == 0 || bridge.mediaRead == 0 {
		return nil, nil
	}
	if bridge.mediaEnabled != enabled {
		flag := uintptr(0)
		if enabled {
			flag = 1
		}
		syscall.SyscallN(bridge.mediaEnable, flag)
		bridge.mediaEnabled = enabled
	}
	if !enabled {
		bridge.mediaCover = nil
		return nil, nil
	}
	var sample presenceMediaSample
	read, _, _ := syscall.SyscallN(bridge.mediaRead, uintptr(unsafe.Pointer(&sample)), unsafe.Sizeof(sample))
	if read == 0 {
		return nil, nil
	}
	title, _, _ := bytes.Cut(sample.title[:], []byte{0})
	artist, _, _ := bytes.Cut(sample.artist[:], []byte{0})
	music := &presenceMusic{Title: cleanActivityLabel(string(title)), Artist: cleanActivityLabel(string(artist))}
	if music.Title == "" {
		return nil, nil
	}
	w, h := int(sample.width), int(sample.height)
	if w < 1 || h < 1 || w > 96 || h > 96 {
		return music, nil
	}
	pixels := sample.rgba[:w*h*4]
	hash := sha256.New()
	hash.Write([]byte{byte(w), byte(h)})
	hash.Write(pixels)
	var digest [32]byte
	copy(digest[:], hash.Sum(nil))
	if bridge.mediaCover == nil || bridge.mediaPixels != digest {
		var out bytes.Buffer
		cover := &image.NRGBA{Pix: pixels, Stride: w * 4, Rect: image.Rect(0, 0, w, h)}
		if err := png.Encode(&out, cover); err != nil || out.Len() > 40*1024 {
			return music, nil
		}
		bridge.mediaPixels = digest
		bridge.mediaCover = out.Bytes()
		bridge.mediaCoverDigest = sha256.Sum256(bridge.mediaCover)
	}
	music.Digest = hex.EncodeToString(bridge.mediaCoverDigest[:])
	return music, bridge.mediaCover
}
