//go:build windows

package main

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestOverlayComponent(t *testing.T) {
	root := t.TempDir()
	bridge, err := openOverlay(root)
	if err != nil {
		t.Fatal(err)
	}
	defer bridge.close()
	bridge.update(0, "Sintet", "Minecraft Java Edition", true)
	if os.Getenv("SINTET_TEST_OVERLAY_PREVIEW") == "1" {
		syscall.SyscallN(bridge.preview)
		time.Sleep(45 * time.Second)
	}
	bridge.close()
	bridge, err = openOverlay(root)
	if err != nil {
		t.Fatal(err)
	}
	bridge.close()
	matches, err := filepath.Glob(filepath.Join(root, "components", "overlay", "*", "sintet-overlay.dll"))
	if err != nil || len(matches) != 1 {
		t.Fatal(matches, err)
	}
	if err = os.WriteFile(matches[0], []byte("invalid DLL"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = openOverlay(root); err == nil {
		t.Fatal("modified component was accepted")
	}
}
