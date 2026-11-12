package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/sldcfg"
)

const (
	windowWidth     = 1100
	windowHeight    = 700
	fontLimit       = 12 << 20
	backgroundLimit = 150 << 20
	backgroundRoute = "/custom-background/"
)

var backgroundFile = regexp.MustCompile(`^[0-9a-f]{32}\.(png|jpg|jpeg|webp|gif|mp4|webm)$`)
var appearanceFontFile = regexp.MustCompile(`^[0-9a-f]{32}\.(ttf|otf|woff|woff2)$`)

type Appearance = sldcfg.Appearance

func defaultAppearance() Appearance { return sldcfg.Default() }

func readAppearance(path string) Appearance {
	settings := defaultAppearance()
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &settings) != nil || !settings.Valid() {
			return defaultAppearance()
		}
	}
	return settings
}

func (a *App) GetAppearance() Appearance {
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	return a.appearance
}

func (a *App) DefaultAppearance() Appearance { return defaultAppearance() }

func (a *App) SaveAppearance(settings Appearance) error {
	if !settings.Valid() {
		return errors.New("Проверь параметры оформления и размер окна.")
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if err := a.checkAppearanceAssets(settings); err != nil {
		return err
	}
	slots := a.copyAppearanceSlots()
	if index := a.appearanceActive - 1; index >= 0 {
		if slots[index].Settings != settings {
			slots[index].CreatorVersion = launcherVersion
			slots[index].Package = ""
			slots[index].Draft = ""
		}
		slots[index].Settings = settings
	}
	if err := a.commitAppearance(settings, slots, a.appearanceActive); err != nil {
		return err
	}
	if a.draftBackground == settings.Background {
		a.draftBackground = ""
	}
	if a.draftFont == settings.FontFile {
		a.draftFont = ""
	}
	return nil
}

func (a *App) ChooseAppearanceBackground() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Выбери фон для Sintet",
		Filters: []runtime.FileFilter{{DisplayName: "Изображения и видео", Pattern: "*.png;*.jpg;*.jpeg;*.webp;*.gif;*.mp4;*.webm"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	return a.importAppearanceBackground(path)
}

func (a *App) importAppearanceBackground(path string) (string, error) {
	input, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer input.Close()
	info, err := input.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > backgroundLimit {
		return "", errors.New("Выбери непустой файл размером до 150 МБ.")
	}
	return a.stageAppearanceAsset(strings.ToLower(filepath.Ext(path)), info.Size(), input, false)
}

func (a *App) stageAppearanceAsset(ext string, size int64, input io.Reader, font bool) (string, error) {
	name, err := a.writeAppearanceAsset(ext, size, input, font)
	if err != nil {
		return "", err
	}
	a.appearanceMu.Lock()
	defer a.appearanceMu.Unlock()
	if font {
		previous := a.draftFont
		a.draftFont = name
		a.removeUnusedAppearanceAsset(previous)
	} else {
		a.removeDraftBackground()
		a.draftBackground = name
	}
	return name, nil
}

func (a *App) writeAppearanceAsset(ext string, size int64, input io.Reader, font bool) (string, error) {
	var id [16]byte
	if _, err := rand.Read(id[:]); err != nil {
		return "", err
	}
	name := hex.EncodeToString(id[:]) + ext
	limit := int64(backgroundLimit)
	if font {
		limit = fontLimit
		if !appearanceFontFile.MatchString(name) {
			return "", errors.New("Поддерживаются TTF, OTF, WOFF и WOFF2.")
		}
	} else if !backgroundFile.MatchString(name) {
		return "", errors.New("Поддерживаются PNG, JPEG, WebP, GIF, MP4 и WebM.")
	}
	if size <= 0 || size > limit {
		return "", errors.New("Файл пуст или превышает допустимый размер.")
	}
	if err := os.MkdirAll(a.backgroundDir, 0700); err != nil {
		return "", err
	}
	destination := filepath.Join(a.backgroundDir, name)
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return "", err
	}
	written, err := io.Copy(output, io.LimitReader(input, limit+1))
	closeErr := output.Close()
	if err != nil || closeErr != nil || written != size {
		_ = os.Remove(destination)
		return "", errors.New("Не удалось скопировать фон.")
	}
	return name, nil
}

// removeDraftBackground requires appearanceMu.
func (a *App) removeDraftBackground() {
	if a.draftBackground != "" {
		name := a.draftBackground
		a.draftBackground = ""
		a.removeUnusedAppearanceAsset(name)
	}
}

func (a *App) serveAppearanceBackground(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name, ok := strings.CutPrefix(r.URL.Path, backgroundRoute)
		if !ok {
			next.ServeHTTP(w, r)
			return
		}
		if (!backgroundFile.MatchString(name) && !appearanceFontFile.MatchString(name)) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(a.backgroundDir, name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil || !info.Mode().IsRegular() {
			http.NotFound(w, r)
			return
		}
		mediaType := map[string]string{".png": "image/png", ".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".webp": "image/webp",
			".gif": "image/gif", ".mp4": "video/mp4", ".webm": "video/webm", ".ttf": "font/ttf", ".otf": "font/otf", ".woff": "font/woff", ".woff2": "font/woff2"}[filepath.Ext(name)]
		w.Header().Set("Content-Type", mediaType)
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, info.ModTime(), f)
	})
}

func (a *App) ChooseAppearanceFont() (string, error) {
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Выбери шрифт для Sintet",
		Filters: []runtime.FileFilter{{DisplayName: "Файлы шрифтов", Pattern: "*.ttf;*.otf;*.woff;*.woff2"}},
	})
	if err != nil || path == "" {
		return "", err
	}
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("Выбери файл шрифта.")
	}
	return a.stageAppearanceAsset(strings.ToLower(filepath.Ext(path)), info.Size(), f, true)
}
