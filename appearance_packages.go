package main

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
	"sintet/internal/sldcfg"
)

var themePackageID = regexp.MustCompile(`^[a-f0-9]{64}$`)
var errUnsignedTheme = errors.New("У оформления ещё нет подписи.")
var errThemeAlreadyImported = errors.New("Это оформление уже есть в «Моё».")

func (a *App) packagePath(id string) string {
	return filepath.Join(filepath.Dir(a.appearancePath), "ThemePackages", id+sldcfg.Extension)
}

// Requires appearanceMu.
func (a *App) removeUnusedThemePackage(id string) {
	if !themePackageID.MatchString(id) {
		return
	}
	for _, slot := range a.appearanceSlots {
		if slot.Package == id {
			return
		}
	}
	_ = os.Remove(a.packagePath(id))
}

func (a *App) ImportAppearanceSlot() (bool, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Импорт оформления",
		Filters: []runtime.FileFilter{{DisplayName: "Подписанное оформление Sintet", Pattern: "*.sldcfg"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	if !strings.EqualFold(filepath.Ext(path), sldcfg.Extension) {
		return false, errors.New("Поддерживаются только подписанные файлы .sldcfg.")
	}
	data, err := readLimitedFile(path, sldcfg.MaxFile)
	if err != nil {
		return false, err
	}
	if err = a.importTheme(data); err != nil {
		return false, err
	}
	return true, nil
}

func (a *App) importTheme(data []byte) error {
	p, err := sldcfg.Open(data, sldcfg.TrustedKey())
	if err != nil {
		return err
	}
	id := fmt.Sprintf("%x", sha256.Sum256(data))
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	for _, slot := range a.appearanceSlots {
		if slot.Package == id {
			return errThemeAlreadyImported
		}
	}
	if draft, e := sldcfg.MarshalDraft(p); e == nil {
		draftID := fmt.Sprintf("%x", sha256.Sum256(draft))
		for i, slot := range a.appearanceSlots {
			if slot.Draft != draftID || slot.Package != "" {
				continue
			}
			if err = accountfiles.Write(a.packagePath(id), data); err != nil {
				return err
			}
			defer a.removeUnusedThemePackage(id)
			slots := a.copyAppearanceSlots()
			slots[i].Draft, slots[i].Package = "", id
			return a.commitAppearance(a.appearance, slots, a.appearanceActive)
		}
	}
	slots, index, err := a.emptyAppearanceSlot()
	if err != nil {
		return err
	}
	settings := p.Settings
	if len(p.Background) > 0 {
		settings.Background, err = a.writeAppearanceAsset(".png", int64(len(p.Background)), bytes.NewReader(p.Background), false)
		if err != nil {
			return err
		}
		defer a.removeUnusedAppearanceAsset(settings.Background)
	}
	if len(p.Font) > 0 {
		settings.FontFile, err = a.writeAppearanceAsset(".ttf", int64(len(p.Font)), bytes.NewReader(p.Font), true)
		if err != nil {
			return err
		}
		defer a.removeUnusedAppearanceAsset(settings.FontFile)
	}
	icon := ""
	if len(p.Icon) > 0 {
		icon, err = saveCustomIcon(a.themeIconsDir(), p.Icon)
		if err != nil {
			return err
		}
		defer a.removeUnusedAppearanceIcon(icon)
	}
	if err = accountfiles.Write(a.packagePath(id), data); err != nil {
		return err
	}
	defer a.removeUnusedThemePackage(id)
	slots[index] = AppearanceSlot{Name: p.Name, Settings: settings, Icon: icon, Package: id, CreatorVersion: p.CreatorVersion}
	return a.commitAppearance(a.appearance, slots, a.appearanceActive)
}

func (a *App) signedTheme(index int, current *Appearance) ([]byte, error) {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if current != nil {
		index = a.appearanceActive - 1
		if *current != a.appearance {
			return nil, errUnsignedTheme
		}
	}
	if index < 0 || index >= len(a.appearanceSlots) {
		return nil, errUnsignedTheme
	}
	slot := a.appearanceSlots[index]
	if !themePackageID.MatchString(slot.Package) {
		return nil, errUnsignedTheme
	}
	data, err := readLimitedFile(a.packagePath(slot.Package), sldcfg.MaxFile)
	if err != nil {
		return nil, errors.New("Подписанный файл не найден. Импортируй его заново.")
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != slot.Package {
		return nil, sldcfg.ErrSignature
	}
	if _, err = sldcfg.Open(data, sldcfg.TrustedKey()); err != nil {
		return nil, err
	}
	return data, nil
}

func (a *App) ExportAppearance(settings Appearance) (bool, error) {
	return a.exportTheme(-1, &settings)
}

func (a *App) ExportAppearanceSlot(index int) (bool, error) {
	return a.exportTheme(index, nil)
}

func (a *App) exportTheme(index int, current *Appearance) (bool, error) {
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Экспорт подписанного оформления", DefaultFilename: "Sintet-theme.sldcfg",
		Filters: []runtime.FileFilter{{DisplayName: "Подписанное оформление Sintet", Pattern: "*.sldcfg"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	if filepath.Ext(path) == "" {
		path += sldcfg.Extension
	}
	if !strings.EqualFold(filepath.Ext(path), sldcfg.Extension) {
		return false, errors.New("Используй расширение .sldcfg.")
	}
	data, err := a.themeForSharing(index, current)
	if err != nil {
		return false, err
	}
	if err = accountfiles.Write(path, data); err != nil {
		return false, err
	}
	return true, nil
}
