package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"sintet/internal/accountfiles"
	"sintet/internal/identity"
)

func prefersOfficialUpdates(root, key string) bool {
	if !(storedProfileKey.MatchString(key) || key == "legacy-sintet") {
		return false
	}
	data, err := os.ReadFile(filepath.Join(root, "accounts", key+".update-channel"))
	return err == nil && string(data) == "official"
}

func updateRoster() *profileRoster {
	root := filepath.Join(documentsFolder(), "Sintet")
	if previewDataDirectory != "" {
		root = previewDataDirectory
	}
	base := os.Getenv("SINTET_ID_URL")
	if base == "" {
		base = defaultIdentityURL
	}
	return newProfileRoster(root, base)
}

func (a *App) updateAccount() (*identity.Client, string, string) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if a.profiles != nil {
		slot := a.profiles.find(a.profiles.active)
		if slot != nil && slot.Provider == "sintet" {
			return slot.sintet, a.profiles.root, slot.Key
		}
	}
	return nil, "", ""
}

func (a *App) SetOfficialUpdates(official bool) error {
	client, root, key := a.updateAccount()
	if client == nil {
		return errors.New("Канал обновлений доступен только тестировщикам")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	offer, err := client.UpdateOfferDetails(ctx)
	if err != nil {
		return err
	}
	if !offer.Tester {
		return errors.New("Канал обновлений доступен только тестировщикам")
	}
	active, _, _ := a.updateAccount()
	if active != client {
		return errors.New("Аккаунт изменился. Повторите действие")
	}
	channel := "test"
	if official {
		channel = "official"
	}
	return accountfiles.Write(filepath.Join(root, "accounts", key+".update-channel"), []byte(channel))
}

// Invoked by the visible prelaunch updater. Only a signed public manifest leaves
// this process; DPAPI credentials never enter arguments, files or the WPF UI.
func writeScopedUpdateOffer(destination string) error {
	roster := updateRoster()
	slot := roster.find(roster.active)
	if slot == nil || slot.Provider != "sintet" || slot.sintet == nil || prefersOfficialUpdates(roster.root, slot.Key) {
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
