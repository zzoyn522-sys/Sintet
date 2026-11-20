package main

import (
	"context"
	"errors"
	"io"
	"os"
	"time"

	"sintet/internal/identity"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (a *App) wardrobeClient(id string) (*identity.Client, error) {
	client := a.activeUpdateClient()
	if client == nil {
		return nil, errors.New("Войдите через Sintet ID, чтобы менять скины и плащи.")
	}
	account := client.SavedAccount()
	if account == nil || account.ID != id {
		return nil, errors.New("Аккаунт изменился. Повторите действие")
	}
	return client, nil
}

func (a *App) GetSkinWardrobe(id string) (identity.SkinWardrobe, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return identity.SkinWardrobe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.SkinWardrobe(ctx, id)
}

func (a *App) ApplyWardrobeSkin(id, key string) (identity.SkinWardrobe, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return identity.SkinWardrobe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.ApplyWardrobeSkin(ctx, id, key)
}

func (a *App) PickWardrobeSkin(id, model string) (*identity.SkinWardrobe, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return nil, err
	}
	if a.ctx == nil {
		return nil, errors.New("Не удалось открыть выбор файла.")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Выбрать скин Minecraft",
		Filters: []runtime.FileFilter{{DisplayName: "PNG", Pattern: "*.png"}},
	})
	if err != nil || path == "" {
		return nil, err
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, identity.MaxSkinPNG+1))
	if err != nil {
		return nil, err
	}
	active, err := a.wardrobeClient(id)
	if err != nil || active != client {
		return nil, errors.New("Аккаунт изменился. Повторите действие")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	result, err := client.UploadWardrobeSkin(ctx, id, model, data)
	if err != nil {
		return nil, err
	}
	return &result, nil
}

func (a *App) GetCapeWardrobe(id string) (identity.CapeWardrobe, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return identity.CapeWardrobe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.CapeWardrobe(ctx, id, nil)
}

func (a *App) EquipWardrobeCape(id, cape string) (identity.CapeWardrobe, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return identity.CapeWardrobe{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	return client.CapeWardrobe(ctx, id, &cape)
}

func (a *App) GetWardrobeTexture(id, address string) (string, error) {
	client, err := a.wardrobeClient(id)
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return client.WardrobeTexture(ctx, id, address)
}
