package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sintet/internal/accountfiles"
)

var cachedAvatarName = regexp.MustCompile(`^[a-f0-9]{64}\.png$`)

func (a *App) avatarBytes(address string) ([]byte, string, error) {
	if !allowedAvatarTexture(a.profiles.base, address) && !a.avatarDB.allowedTexture(address) {
		return nil, "", errors.New("Неизвестный адрес аватарки.")
	}
	digest := sha256.Sum256([]byte(address))
	name := hex.EncodeToString(digest[:]) + ".png"
	path := filepath.Join(a.profiles.root, "Avatars", name)
	if data, err := os.ReadFile(path); err == nil {
		if err = validateAvatarContent(address, data); err == nil {
			return data, name, nil
		}
	}
	client := http.Client{
		Timeout:       6 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	response, err := client.Get(address)
	if err != nil {
		return nil, "", errors.New("Не удалось загрузить аватарку.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, "", errors.New("Аватарка недоступна.")
	}
	const limit = 512 << 10
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(data) > limit {
		return nil, "", errors.New("Не удалось прочитать аватарку.")
	}
	if err = validateAvatarContent(address, data); err != nil {
		return nil, "", err
	}
	if err = accountfiles.Write(path, data); err != nil {
		return nil, "", errors.New("Не удалось сохранить аватарку.")
	}
	return data, name, nil
}

func validateAvatarContent(address string, data []byte) error {
	target, err := url.Parse(address)
	if err != nil {
		return err
	}
	expected := strings.TrimSuffix(filepath.Base(target.Path), ".png")
	digest := sha256.Sum256(data)
	if expected != hex.EncodeToString(digest[:]) {
		return errors.New("повреждённый файл аватарки")
	}
	_, err = readAvatarGlow(data)
	return err
}

func (a *App) ResolveAvatar(address string) (string, error) {
	_, name, err := a.avatarBytes(address)
	if err != nil {
		return "", err
	}
	return "/cached-avatars/" + name, nil
}

func (a *App) serveLauncherAssets(next http.Handler) http.Handler {
	appearance := a.serveAppearanceBackground(next)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if preview, ok := strings.CutPrefix(r.URL.Path, "/catalog-theme-preview/"); ok {
			serveCatalogThemePreview(w, r, preview)
			return
		}
		directory := "WorkshopIcons"
		name, ok := strings.CutPrefix(r.URL.Path, "/workshop-icons/")
		if !ok {
			directory = "BuildIcons"
			name, ok = strings.CutPrefix(r.URL.Path, "/build-icons/")
		}
		if !ok {
			directory = "ThemeIcons"
			name, ok = strings.CutPrefix(r.URL.Path, "/theme-icons/")
		}
		if ok {
			if !cachedAvatarName.MatchString(name) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "image/png")
			w.Header().Set("X-Content-Type-Options", "nosniff")
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
			http.ServeFile(w, r, filepath.Join(a.profiles.root, directory, name))
			return
		}
		name, ok = strings.CutPrefix(r.URL.Path, "/cached-avatars/")
		if !ok {
			appearance.ServeHTTP(w, r)
			return
		}
		if !cachedAvatarName.MatchString(name) || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
			http.NotFound(w, r)
			return
		}
		f, err := os.Open(filepath.Join(a.profiles.root, "Avatars", name))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		http.ServeContent(w, r, name, info.ModTime(), f)
	})
}
