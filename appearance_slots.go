package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
	"sintet/internal/sldcfg"
)

type AppearanceSlot struct {
	Name           string     `json:"name"`
	CreatorVersion string     `json:"creatorVersion,omitempty"`
	Icon           string     `json:"icon,omitempty"`
	Settings       Appearance `json:"settings"`
	Package        string     `json:"package,omitempty"`
	Draft          string     `json:"draft,omitempty"`
}

const initialAppearanceSlots = 4
const maxAppearanceSlots = 24

type AppearanceLibrary struct {
	Current    Appearance       `json:"current"`
	Slots      []AppearanceSlot `json:"slots"`
	ActiveSlot int              `json:"activeSlot"` // Zero means no saved theme is active.
}

type appearanceStorage struct {
	Appearance
	Library *appearanceLibraryStorage `json:"library,omitempty"`
}

type appearanceLibraryStorage struct {
	Slots      json.RawMessage `json:"slots"`
	ActiveSlot int             `json:"activeSlot"`
}

func validSlotName(name string) bool {
	return name != "" && utf8.RuneCountInString(name) <= 24 && strings.IndexFunc(name, unicode.IsControl) == -1
}

func decodeAppearanceSlots(data []byte) []AppearanceSlot {
	var records []struct {
		Name           string          `json:"name"`
		CreatorVersion string          `json:"creatorVersion"`
		Icon           string          `json:"icon"`
		Settings       json.RawMessage `json:"settings"`
		Package        string          `json:"package"`
		Draft          string          `json:"draft"`
	}
	if json.Unmarshal(data, &records) != nil {
		return make([]AppearanceSlot, initialAppearanceSlots)
	}
	records = records[:min(len(records), maxAppearanceSlots)]
	slots := make([]AppearanceSlot, max(initialAppearanceSlots, len(records)))
	for i, record := range records {
		settings := defaultAppearance()
		if validSlotName(record.Name) && len(record.Settings) > 0 && record.Settings[0] == '{' &&
			json.Unmarshal(record.Settings, &settings) == nil && settings.Valid() {
			icon := record.Icon
			if !cachedAvatarName.MatchString(icon) {
				icon = ""
			}
			signed := record.Package
			if !themePackageID.MatchString(signed) {
				signed = ""
			}
			draft := record.Draft
			if !themePackageID.MatchString(draft) {
				draft = ""
			}
			creatorVersion := record.CreatorVersion
			if !sldcfg.ValidCreatorVersion(creatorVersion) {
				creatorVersion = ""
			}
			slots[i] = AppearanceSlot{Name: record.Name, Icon: icon, Settings: settings, Package: signed, Draft: draft, CreatorVersion: creatorVersion}
		}
	}
	return slots
}

func readAppearanceLibrary(path string) AppearanceLibrary {
	result := AppearanceLibrary{Current: defaultAppearance()}
	document := appearanceStorage{Appearance: result.Current}
	if data, err := readLimitedFile(path, 256<<10); err == nil {
		if json.Unmarshal(data, &document) == nil && document.Appearance.Valid() {
			result.Current = document.Appearance
		} else {
			document.Library = nil
		}
	}
	if document.Library != nil {
		result.Slots = decodeAppearanceSlots(document.Library.Slots)
		active := document.Library.ActiveSlot
		if active > 0 && active <= len(result.Slots) && result.Slots[active-1].Name != "" && result.Slots[active-1].Settings == result.Current {
			result.ActiveSlot = active
		}
		return result
	}
	data, _ := readLimitedFile(filepath.Join(filepath.Dir(path), "appearance-slots.json"), 256<<10)
	result.Slots = decodeAppearanceSlots(data)
	for index, slot := range result.Slots {
		if slot.Name != "" && slot.Settings == result.Current {
			result.ActiveSlot = index + 1
			break
		}
	}
	return result
}

// copyAppearanceSlots requires appearanceMu. Work on a copy until the write succeeds.
func (a *App) copyAppearanceSlots() []AppearanceSlot {
	slots := make([]AppearanceSlot, max(initialAppearanceSlots, len(a.appearanceSlots)))
	copy(slots, a.appearanceSlots)
	return slots
}

func (a *App) GetAppearanceLibrary() AppearanceLibrary {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	return AppearanceLibrary{Current: a.appearance, Slots: a.copyAppearanceSlots(), ActiveSlot: a.appearanceActive}
}

func (a *App) SaveAppearanceSlot(index int, name string, settings Appearance) error {
	name = strings.TrimSpace(name)
	if !validSlotName(name) || !settings.Valid() {
		return errors.New("Укажи название до 24 символов и проверь настройки.")
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if err := a.checkAppearanceAssets(settings); err != nil {
		return err
	}
	slots := a.copyAppearanceSlots()
	if index < 0 || index >= len(slots) {
		return errors.New("Слот оформления не найден.")
	}
	if slots[index].Name != name || slots[index].Settings != settings {
		if slots[index].Name == "" || slots[index].Settings != settings {
			slots[index].CreatorVersion = launcherVersion
		}
		slots[index].Package = ""
		slots[index].Draft = ""
	}
	slots[index].Name = name
	slots[index].Settings = settings
	current := a.appearance
	if a.appearanceActive == index+1 {
		current = settings
	}
	return a.commitAppearance(current, slots, a.appearanceActive)
}

func (a *App) AddCurrentAppearance(name string) error {
	name = strings.TrimSpace(name)
	if !validSlotName(name) {
		return errors.New("Укажи название до 24 символов.")
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	slots, index, err := a.emptyAppearanceSlot()
	if err != nil {
		return err
	}
	slots[index] = AppearanceSlot{Name: name, Settings: a.appearance, CreatorVersion: launcherVersion}
	return a.commitAppearance(a.appearance, slots, a.appearanceActive)
}

func (a *App) emptyAppearanceSlot() ([]AppearanceSlot, int, error) {
	slots := a.copyAppearanceSlots()
	for index, slot := range slots {
		if slot.Name == "" {
			return slots, index, nil
		}
	}
	if len(slots) >= maxAppearanceSlots {
		return nil, 0, errors.New("В библиотеке уже 24 оформления. Удали одно, чтобы добавить новое.")
	}
	return append(slots, AppearanceSlot{}), len(slots), nil
}

func (a *App) ClearAppearanceSlot(index int) error {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	slots, err := a.editAppearanceSlot(index)
	if err != nil {
		return err
	}
	slots[index] = AppearanceSlot{}
	for len(slots) > initialAppearanceSlots && slots[len(slots)-1].Name == "" {
		slots = slots[:len(slots)-1]
	}
	if a.appearanceActive == index+1 {
		return a.commitAppearance(defaultAppearance(), slots, 0)
	}
	return a.commitAppearance(a.appearance, slots, a.appearanceActive)
}

func (a *App) RenameAppearanceSlot(index int, name string) error {
	name = strings.TrimSpace(name)
	if !validSlotName(name) {
		return errors.New("Укажи название до 24 символов.")
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	slots, err := a.editAppearanceSlot(index)
	if err != nil {
		return err
	}
	if slots[index].Name != name {
		slots[index].Name = name
		slots[index].Package = ""
		slots[index].Draft = ""
	}
	return a.commitAppearance(a.appearance, slots, a.appearanceActive)
}

func (a *App) SetActiveAppearanceSlot(index int) error {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if index == -1 {
		return a.commitAppearance(defaultAppearance(), a.copyAppearanceSlots(), 0)
	}
	slots, err := a.editAppearanceSlot(index)
	if err != nil {
		return err
	}
	if err = a.checkAppearanceAssets(slots[index].Settings); err != nil {
		return err
	}
	return a.commitAppearance(slots[index].Settings, slots, index+1)
}

func (a *App) editAppearanceSlot(index int) ([]AppearanceSlot, error) {
	slots := a.copyAppearanceSlots()
	if index < 0 || index >= len(slots) || slots[index].Name == "" {
		return nil, errors.New("Оформление не найдено.")
	}
	return slots, nil
}

func (a *App) checkAppearanceAssets(settings Appearance) error {
	available := func(name string) bool {
		if name == "" {
			return true
		}
		if !a.appearanceAssetAvailable(name) {
			return false
		}
		info, err := os.Stat(filepath.Join(a.backgroundDir, name))
		return err == nil && info.Mode().IsRegular()
	}
	if !available(settings.Background) {
		return errors.New("Выбери файл фона заново.")
	}
	if !available(settings.FontFile) {
		return errors.New("Выбери файл шрифта заново.")
	}
	return nil
}

// commitAppearance requires appearanceMu. A single write keeps selection and settings in sync.
func (a *App) commitAppearance(current Appearance, slots []AppearanceSlot, active int) error {
	records, err := json.Marshal(slots)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(appearanceStorage{Appearance: current, Library: &appearanceLibraryStorage{Slots: records, ActiveSlot: active}}, "", "  ")
	if err != nil {
		return err
	}
	if err = accountfiles.Write(a.appearancePath, data); err != nil {
		return err
	}
	previous, previousSlots := a.appearance, a.appearanceSlots
	a.appearance, a.appearanceSlots, a.appearanceActive = current, slots, active
	a.removeUnusedAppearanceAsset(previous.Background)
	a.removeUnusedAppearanceAsset(previous.FontFile)
	for _, slot := range previousSlots {
		a.removeUnusedAppearanceAsset(slot.Settings.Background)
		a.removeUnusedAppearanceAsset(slot.Settings.FontFile)
		a.removeUnusedAppearanceIcon(slot.Icon)
		a.removeUnusedThemePackage(slot.Package)
	}
	return nil
}

// appearanceAssetAvailable and removeUnusedAppearanceAsset require appearanceMu.
func (a *App) appearanceAssetAvailable(name string) bool {
	if name == a.draftBackground || name == a.appearance.Background || name == a.draftFont || name == a.appearance.FontFile {
		return true
	}
	for _, slot := range a.appearanceSlots {
		if slot.Settings.Background == name || slot.Settings.FontFile == name {
			return true
		}
	}
	return false
}

func (a *App) removeUnusedAppearanceAsset(name string) {
	if (backgroundFile.MatchString(name) || appearanceFontFile.MatchString(name)) && !a.appearanceAssetAvailable(name) {
		_ = os.Remove(filepath.Join(a.backgroundDir, name))
	}
}

func (a *App) themeIconsDir() string {
	return filepath.Join(filepath.Dir(a.appearancePath), "ThemeIcons")
}

func (a *App) removeUnusedAppearanceIcon(name string) {
	if !cachedAvatarName.MatchString(name) {
		return
	}
	for _, slot := range a.appearanceSlots {
		if slot.Icon == name {
			return
		}
	}
	_ = os.Remove(filepath.Join(a.themeIconsDir(), name))
}

func (a *App) ChooseAppearanceIcon(index int) (bool, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Иконка оформления",
		Filters: []runtime.FileFilter{{DisplayName: "Изображения PNG и JPEG", Pattern: "*.png;*.jpg;*.jpeg"}},
	})
	if err != nil || path == "" {
		return false, err
	}
	data, err := readLimitedFile(path, iconFileLimit)
	if err != nil {
		return false, err
	}
	return true, a.setAppearanceIcon(index, data)
}

func (a *App) ClearAppearanceIcon(index int) error {
	return a.setAppearanceIcon(index, nil)
}

func (a *App) setAppearanceIcon(index int, data []byte) error {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	slots, err := a.editAppearanceSlot(index)
	if err != nil {
		return err
	}
	name := ""
	if len(data) > 0 {
		name, err = saveCustomIcon(a.themeIconsDir(), data)
		if err != nil {
			return err
		}
	}
	if slots[index].Icon != name {
		slots[index].Icon = name
		slots[index].Package = ""
		slots[index].Draft = ""
	}
	if err := a.commitAppearance(a.appearance, slots, a.appearanceActive); err != nil {
		a.removeUnusedAppearanceIcon(name)
		return err
	}
	return nil
}

func readLimitedFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > limit {
		return nil, errors.New("Файл пуст или превышает допустимый размер.")
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err == nil && int64(len(data)) != info.Size() {
		return nil, errors.New("Файл изменился во время чтения.")
	}
	return data, err
}
