package main

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"sintet/internal/accountfiles"
)

// Invoked by the visible prelaunch updater. Only a signed public manifest leaves
// this process; DPAPI credentials never enter arguments, files or the WPF UI.
func writeScopedUpdateOffer(destination string) error {
	root := filepath.Join(documentsFolder(), "Sintet")
	if previewDataDirectory != "" {
		root = previewDataDirectory
	}
	base := os.Getenv("SINTET_ID_URL")
	if base == "" {
		base = defaultIdentityURL
	}
	roster := newProfileRoster(root, base)
	slot := roster.find(roster.active)
	if slot == nil || slot.Provider != "sintet" || slot.sintet == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	offer, err := slot.sintet.UpdateOffer(ctx)
	if err != nil {
		return err
	}
	if len(offer) == 0 || string(offer) == "null" {
		return nil
	}
	if _, err = parseSignedUpdate(offer); err != nil {
		return err
	}
	return accountfiles.Write(destination, offer)
}
