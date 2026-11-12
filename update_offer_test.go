package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestScopedPrelaunchProbeDoesNotCreateAnOfferForAnUnsignedInProfile(t *testing.T) {
	previous := previewDataDirectory
	previewDataDirectory = t.TempDir()
	defer func() { previewDataDirectory = previous }()
	destination := filepath.Join(t.TempDir(), "offer.json")
	if err := writeScopedUpdateOffer(destination); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(destination); !os.IsNotExist(err) {
		t.Fatal("anonymous profile produced a personal offer", err)
	}
}
