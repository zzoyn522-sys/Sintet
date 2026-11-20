package main

import (
	"context"
	"errors"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"sintet/internal/identity"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

const publicIdentityService = "https://sintet-identity.zzoyn522.workers.dev"

func identityService(base string) string {
	if previewDataDirectory != "" {
		if local, err := url.Parse(base); err == nil && local.Scheme == "http" && local.Hostname() == "127.0.0.1" {
			return base
		}
	}
	return publicIdentityService
}

func (m *socialManager) lobbyClient(ctx context.Context) (*identity.Client, error) {
	m.identityMu.Lock()
	defer m.identityMu.Unlock()
	m.refreshLobbyIdentity()

	m.mu.Lock()
	client, provider := m.client, m.state.LobbyProvider
	m.mu.Unlock()
	if provider == "sintet" && client != nil {
		return client, nil
	}
	if provider == "" || provider == "sintet" {
		return nil, errors.New("выберите профиль для лобби")
	}

	app := m.app
	app.authMu.Lock()
	roster := app.profiles
	active, root, base := roster.active, roster.root, roster.base
	var microsoft *identity.MicrosoftClient
	var ely *identity.ElyClient
	name := localAccount(roster.lobbyDefaultName).Username
	selectedProvider := "local"
	if slot := roster.find(active); slot != nil {
		selectedProvider = slot.Provider
		microsoft, ely = slot.microsoft, slot.ely
		if account := slot.state().Account; account != nil {
			name = account.Username
		}
	}
	app.authMu.Unlock()
	if selectedProvider != provider || active == "" {
		return nil, errors.New("профиль изменился во время входа в лобби")
	}
	if client != nil && time.Until(client.LobbySessionExpires()) > 5*time.Minute {
		if account := client.SavedAccount(); account != nil && account.Username == name {
			return client, nil
		}
	}
	next, err := identity.OpenLobbySession(ctx, base, identityService(base), filepath.Join(root, "lobby-key.dpapi"), provider, name, microsoft, ely)
	if err != nil {
		return nil, err
	}
	app.authMu.Lock()
	unchanged := app.profiles.active == active
	app.authMu.Unlock()
	if !unchanged {
		return nil, errors.New("профиль изменился во время входа в лобби")
	}
	account := next.SavedAccount()
	if app.avatarDB != nil && (provider == "ely" || provider == "microsoft") {
		if avatar, err := app.avatarDB.avatar(ctx, account.ID); err == nil {
			next.SetLobbyAvatar(avatar)
		}
	}
	m.mu.Lock()
	previousID := m.account
	m.mu.Unlock()
	if previousID != "" && previousID != account.ID {
		m.disconnect()
	}
	m.mu.Lock()
	m.client, m.account, m.externalSlot = next, account.ID, active
	m.mu.Unlock()
	return next, nil
}

func (a *App) ChangeLobbyAvatar() (string, error) {
	if a.social == nil || a.ctx == nil {
		return "", errors.New("сервис профилей запускается")
	}
	provider := a.social.snapshot().LobbyProvider
	if provider != "microsoft" && provider != "ely" {
		return "", errors.New("выберите профиль Microsoft или Ely.by")
	}
	client, err := a.social.lobbyClient(a.ctx)
	if err != nil {
		return "", err
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Выбрать аватарку", Filters: []runtime.FileFilter{{DisplayName: "Изображения", Pattern: "*.png;*.jpg;*.jpeg;*.webp"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	const maxBytes = 2 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil || len(data) == 0 || len(data) > maxBytes {
		return "", errors.New("выберите изображение размером до 2 МБ")
	}
	a.social.identity()
	a.social.mu.Lock()
	active := a.social.client == client
	a.social.mu.Unlock()
	if !active {
		return "", errors.New("профиль изменился")
	}
	ctx, cancel := context.WithTimeout(a.ctx, 35*time.Second)
	defer cancel()
	avatar, err := client.UploadLobbyAvatar(ctx, data)
	if err != nil {
		return "", err
	}
	if !a.avatarDB.allowedTexture(avatar) {
		return "", errors.New("неверный адрес аватарки")
	}
	client.SetLobbyAvatar(avatar)
	return avatar, nil
}

func (m *socialManager) lobbyPlayerName() string {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client != nil {
		if account := client.SavedAccount(); account != nil {
			return account.Username
		}
	}
	return "Player"
}
