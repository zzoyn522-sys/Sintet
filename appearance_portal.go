package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sintet/internal/sldcfg"
)

func (a *App) ThemePortalAvailable() bool { return true }

func (a *App) ThemePortalIsLocal() bool { return identity.ThemePortalIsLocal() }

func (a *App) themePortalAccount() (*identity.Client, string, error) {
	a.authMu.Lock()
	slot := a.profiles.find(a.profiles.active)
	a.authMu.Unlock()
	if slot == nil || slot.sintet == nil {
		return nil, "", errors.New("Войди в Sintet ID для синхронизации.")
	}
	client := slot.sintet
	account := client.SavedAccount()
	if account == nil {
		return nil, "", errors.New("Войди в Sintet ID для синхронизации.")
	}
	return client, account.ID, nil
}

func (a *App) themePortalRequest(method, id string, data []byte, draft bool) ([]byte, error) {
	client, account, err := a.themePortalAccount()
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 15*time.Second)
	defer cancel()
	return client.ThemeLibrary(ctx, account, method, id, data, draft)
}

func (a *App) PortalThemes() ([]identity.SavedTheme, error) {
	data, err := a.themePortalRequest(http.MethodGet, "", nil, false)
	if err != nil {
		return nil, err
	}
	var response struct {
		Themes []identity.SavedTheme `json:"themes"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return nil, errors.New("Некорректный ответ кабинета.")
	}
	for _, item := range response.Themes {
		if !themePackageID.MatchString(item.ID) {
			return nil, errors.New("Некорректный ответ кабинета.")
		}
	}
	return response.Themes, nil
}

func (a *App) ImportPortalTheme(id string) error {
	if !themePackageID.MatchString(id) {
		return errors.New("Оформление не найдено.")
	}
	data, err := a.themePortalRequest(http.MethodGet, id, nil, false)
	if err != nil {
		return err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != id {
		return errors.New("Файл оформления изменился.")
	}
	return a.importTheme(data)
}

func (a *App) PublishPortalTheme(index int) error {
	data, err := a.themeForSharing(index, nil)
	if err != nil {
		return err
	}
	client, account, err := a.themePortalAccount()
	if err != nil {
		return err
	}
	pack, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		return err
	}
	capture, cancel := context.WithTimeout(a.ctx, 25*time.Second)
	defer cancel()
	previews, err := captureThemePreviews(capture, pack)
	if err != nil {
		return fmt.Errorf("Не удалось создать снимки оформления: %w", err)
	}
	upload, stop := context.WithTimeout(a.ctx, 35*time.Second)
	defer stop()
	id := fmt.Sprintf("%x", sha256.Sum256(data))
	if err = client.UploadThemePackage(upload, account, id, data); err != nil {
		return err
	}
	return client.UploadThemePreviews(upload, account, id, launcherVersion, previews)
}

func (a *App) themeForSharing(index int, current *Appearance) ([]byte, error) {
	data, err := a.signedTheme(index, current)
	if !errors.Is(err, errUnsignedTheme) {
		return data, err
	}
	index, slot, draft, err := a.appearanceDraft(index, current)
	if err != nil {
		return nil, err
	}
	data, err = a.themePortalRequest(http.MethodPost, "", draft, true)
	if err != nil {
		return nil, err
	}
	pack, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		return nil, err
	}
	signedDraft, err := sldcfg.MarshalDraft(pack)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(signedDraft, draft) {
		return nil, errors.New("Сервис вернул другое оформление.")
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if index >= 0 && index < len(a.appearanceSlots) && a.appearanceSlots[index] == slot {
		id := fmt.Sprintf("%x", sha256.Sum256(data))
		if err = accountfiles.Write(a.packagePath(id), data); err != nil {
			return nil, err
		}
		defer a.removeUnusedThemePackage(id)
		slots := a.copyAppearanceSlots()
		slots[index].Package = id
		slots[index].Draft = ""
		if err = a.commitAppearance(a.appearance, slots, a.appearanceActive); err != nil {
			return nil, err
		}
	}
	return data, nil
}

func (a *App) appearanceDraft(index int, current *Appearance) (int, AppearanceSlot, []byte, error) {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	slot := AppearanceSlot{Name: "Оформление Sintet", CreatorVersion: launcherVersion}
	if current != nil {
		index = a.appearanceActive - 1
	}
	if index >= 0 && index < len(a.appearanceSlots) && a.appearanceSlots[index].Name != "" {
		slot = a.appearanceSlots[index]
	} else if current == nil {
		return index, AppearanceSlot{}, nil, errors.New("Оформление не найдено.")
	}
	if current != nil {
		if slot.Settings != *current {
			slot.CreatorVersion = launcherVersion
		}
		slot.Settings = *current
	}
	settings := slot.Settings
	if settings.Font != "custom" {
		settings.FontFile = ""
	}
	if !settings.Valid() {
		return index, slot, nil, errors.New("Некорректные настройки оформления.")
	}
	if err := a.checkAppearanceAssets(settings); err != nil {
		return index, slot, nil, err
	}
	p := sldcfg.Package{Name: slot.Name, Settings: settings, CreatorVersion: slot.CreatorVersion}
	p.Settings.Background, p.Settings.FontFile = "", ""
	if name := settings.Background; name != "" {
		ext := strings.ToLower(filepath.Ext(name))
		if ext == ".mp4" || ext == ".webm" {
			return index, slot, nil, errors.New("В выбранном оформлении сохранён видеофон. Для экспорта в .sldcfg выбери изображение.")
		}
		source, err := readLimitedFile(filepath.Join(a.backgroundDir, name), sldcfg.MaxBackground)
		if err != nil {
			return index, slot, nil, err
		}
		p.Background, err = sldcfg.PreparePNG(source, false)
		if err != nil {
			return index, slot, nil, err
		}
	}
	if name := settings.FontFile; name != "" {
		if filepath.Ext(name) != ".ttf" {
			return index, slot, nil, errors.New("Для подписи выбери шрифт TrueType (.ttf).")
		}
		var err error
		p.Font, err = readLimitedFile(filepath.Join(a.backgroundDir, name), sldcfg.MaxFont)
		if err != nil {
			return index, slot, nil, err
		}
	}
	if cachedAvatarName.MatchString(slot.Icon) {
		source, err := readLimitedFile(filepath.Join(a.themeIconsDir(), slot.Icon), sldcfg.MaxIcon)
		if err != nil {
			return index, slot, nil, err
		}
		p.Icon, err = sldcfg.PreparePNG(source, true)
		if err != nil {
			return index, slot, nil, err
		}
	}
	data, err := sldcfg.MarshalDraft(p)
	return index, slot, data, err
}
