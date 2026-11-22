package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/png"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"golang.org/x/image/draw"
	"golang.org/x/sys/windows"
)

const screenshotRoute = "/game-screenshots/"
const screenshotTrash = ".sintet-screenshot-trash"

var screenshotThumbnailSlots = make(chan struct{}, 2)

type GameScreenshot struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Profile   string    `json:"profile"`
	Directory string    `json:"directory"`
	Source    string    `json:"source"`
	Modified  time.Time `json:"modified"`
	Size      int64     `json:"size"`
	Revision  string    `json:"revision"`
}

func (a *App) screenshotRoot() (string, map[string]string) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	names := a.buildNameMap()
	return a.service.Root, names
}

func validScreenshotPath(rel string, trash bool) bool {
	if !filepath.IsLocal(rel) || rel != filepath.Clean(rel) || strings.Contains(rel, ":") {
		return false
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if trash {
		if len(parts) < 3 || parts[len(parts)-2] != screenshotTrash {
			return false
		}
		parts = append(parts[:len(parts)-2], parts[len(parts)-1])
	}
	if !strings.EqualFold(filepath.Ext(parts[len(parts)-1]), ".png") {
		return false
	}
	switch len(parts) {
	case 2:
		return parts[0] == "screenshots"
	case 4:
		return parts[0] == "instances" && parts[1] != "family" && parts[2] == "screenshots"
	case 5:
		return parts[0] == "instances" && parts[1] == "family" && parts[3] == "screenshots"
	}
	return false
}

func screenshotID(path string) string { return base64.RawURLEncoding.EncodeToString([]byte(path)) }

func screenshotPath(id string, trash bool) (string, error) {
	if len(id) > 2048 {
		return "", errors.New("неверный скриншот")
	}
	raw, err := base64.RawURLEncoding.DecodeString(id)
	if err != nil || !validScreenshotPath(string(raw), trash) {
		return "", errors.New("неверный скриншот")
	}
	return string(raw), nil
}

func screenshotDirs(root *os.Root) []string {
	dirs := []string{"screenshots"}
	for _, parent := range []string{"instances", filepath.Join("instances", "family")} {
		entries, _ := fs.ReadDir(root.FS(), filepath.ToSlash(parent))
		for _, entry := range entries {
			if entry.IsDir() && entry.Name() != "family" {
				dirs = append(dirs, filepath.Join(parent, entry.Name(), "screenshots"))
			}
		}
	}
	return dirs
}

func screenshotSource(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	if len(stem) >= 25 {
		parts := strings.Split(stem, "_")
		if len(parts) == 4 {

			// The overlay appends milliseconds and its process ID to the timestamp.
			if _, err := time.Parse("2006-01-02_15.04.05", strings.Join(parts[:2], "_")); err == nil &&
				len(parts[2]) == 3 && strings.IndexFunc(parts[2]+parts[3], func(r rune) bool { return r < '0' || r > '9' }) < 0 && parts[3] != "" {
				return "overlay"
			}
		}
	}
	if len(stem) >= 19 {
		if _, err := time.Parse("2006-01-02_15.04.05", stem[:19]); err == nil {
			return "minecraft"
		}
	}
	return ""
}

func (a *App) GameScreenshots() ([]GameScreenshot, error) {
	gameDir, names := a.screenshotRoot()
	root, err := os.OpenRoot(gameDir)
	if os.IsNotExist(err) {
		return []GameScreenshot{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer root.Close()
	items := []GameScreenshot{}
	for _, dir := range screenshotDirs(root) {
		info, err := root.Lstat(dir)
		if err != nil || !info.IsDir() {
			continue
		}
		files, _ := fs.ReadDir(root.FS(), filepath.ToSlash(dir))
		id := filepath.Base(filepath.Dir(dir))
		label := names[id]
		if label == "" && dir != "screenshots" {
			label = id
		}
		for _, entry := range files {
			path := filepath.Join(dir, entry.Name())
			if !entry.Type().IsRegular() || !validScreenshotPath(path, false) {
				continue
			}
			info, err := entry.Info()
			if err != nil || info.Size() <= 0 {
				continue
			}
			items = append(items, GameScreenshot{
				ID: screenshotID(path), Name: entry.Name(), Profile: label, Directory: dir,
				Source: screenshotSource(entry.Name()), Modified: info.ModTime(), Size: info.Size(),
				Revision: fmt.Sprintf("%d-%d", info.ModTime().UnixNano(), info.Size()),
			})
		}
	}
	slices.SortFunc(items, func(a, b GameScreenshot) int {
		if date := b.Modified.Compare(a.Modified); date != 0 {
			return date
		}
		return strings.Compare(a.ID, b.ID)
	})
	return items, nil
}

func (a *App) openScreenshot(id string, trash bool) (*os.Root, string, error) {
	path, err := screenshotPath(id, trash)
	if err != nil {
		return nil, "", err
	}
	gameDir, _ := a.screenshotRoot()
	root, err := os.OpenRoot(gameDir)
	if err != nil {
		return nil, "", err
	}
	check := filepath.Dir(path)
	for check != "." {
		info, err := root.Lstat(check)
		if err != nil || !info.IsDir() {
			root.Close()
			return nil, "", errors.New("папка снимка недоступна")
		}
		check = filepath.Dir(check)
	}
	info, err := root.Lstat(path)
	if err != nil || !info.Mode().IsRegular() {
		root.Close()
		return nil, "", errors.New("снимок не найден")
	}
	return root, path, nil
}

func (a *App) serveScreenshot(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.NotFound(w, r)
		return
	}
	root, path, err := a.openScreenshot(strings.TrimPrefix(r.URL.Path, screenshotRoute), false)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer root.Close()
	file, err := root.Open(path)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	stat, err := file.Stat()
	if err != nil || stat.Size() > 64<<20 {
		http.NotFound(w, r)
		return
	}
	config, err := png.DecodeConfig(io.LimitReader(file, 64<<20))
	if err != nil || config.Width < 1 || config.Height < 1 || int64(config.Width)*int64(config.Height) > 40_000_000 {
		http.NotFound(w, r)
		return
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, no-cache")
	if r.URL.Query().Get("thumb") == "1" && max(config.Width, config.Height) > 480 {
		tag := fmt.Sprintf("\"thumb-%d-%d\"", stat.ModTime().UnixNano(), stat.Size())
		w.Header().Set("ETag", tag)
		if r.Header.Get("If-None-Match") == tag {
			w.WriteHeader(http.StatusNotModified)
			return
		}
		if r.Method == http.MethodHead {
			return
		}
		select {
		case screenshotThumbnailSlots <- struct{}{}:
			defer func() { <-screenshotThumbnailSlots }()
		case <-r.Context().Done():
			return
		}
		source, err := png.Decode(io.LimitReader(file, 64<<20))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		scale := float64(480) / float64(max(config.Width, config.Height))
		thumb := image.NewRGBA(image.Rect(0, 0, max(1, int(float64(config.Width)*scale)), max(1, int(float64(config.Height)*scale))))
		draw.ApproxBiLinear.Scale(thumb, thumb.Bounds(), source, source.Bounds(), draw.Src, nil)
		_ = png.Encode(w, thumb)
		return
	}
	http.ServeContent(w, r, filepath.Base(path), stat.ModTime(), file)
}

func screenshotFilename(name string) (string, error) {
	name = strings.TrimSpace(name)
	if strings.EqualFold(filepath.Ext(name), ".png") {
		name = strings.TrimSpace(strings.TrimSuffix(name, filepath.Ext(name)))
	}
	filename := name + ".png"
	if name == "" || strings.HasSuffix(name, ".") || utf8.RuneCountInString(name) > 120 ||
		!filepath.IsLocal(filename) || strings.ContainsAny(name, "<>:\"/\\|?*") || strings.IndexFunc(name, unicode.IsControl) >= 0 {
		return "", errors.New("введи допустимое имя без символов < > : \" / \\ | ? *")
	}
	return filename, nil
}

func moveScreenshot(root *os.Root, from, to string) error {
	source, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), from))
	if err != nil {
		return err
	}
	target, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), to))
	if err != nil {
		return err
	}
	return windows.MoveFileEx(source, target, windows.MOVEFILE_WRITE_THROUGH)
}

func (a *App) RenameGameScreenshot(id, name string) (string, error) {
	filename, err := screenshotFilename(name)
	if err != nil {
		return "", err
	}
	root, path, err := a.openScreenshot(id, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	to := filepath.Join(filepath.Dir(path), filename)
	if to != path {
		if err = moveScreenshot(root, path, to); err != nil {
			return "", err
		}
	}
	return screenshotID(to), nil
}

func (a *App) TrashGameScreenshot(id string) (string, error) {
	root, path, err := a.openScreenshot(id, false)
	if err != nil {
		return "", err
	}
	defer root.Close()
	dir := filepath.Join(filepath.Dir(path), screenshotTrash)
	if err = root.Mkdir(dir, 0700); err != nil && !os.IsExist(err) {
		return "", err
	}
	info, err := root.Lstat(dir)
	if err != nil || !info.IsDir() {
		return "", errors.New("папка удалённых снимков недоступна")
	}
	to := filepath.Join(dir, fmt.Sprintf("%d-%s", time.Now().UnixNano(), filepath.Base(path)))
	if err = moveScreenshot(root, path, to); err != nil {
		return "", err
	}
	return screenshotID(to), nil
}

func (a *App) RestoreGameScreenshot(id string) (string, error) {
	root, path, err := a.openScreenshot(id, true)
	if err != nil {
		return "", err
	}
	defer root.Close()
	_, name, ok := strings.Cut(filepath.Base(path), "-")
	if !ok {
		return "", errors.New("неверное имя удалённого снимка")
	}
	to := filepath.Join(filepath.Dir(filepath.Dir(path)), name)
	if !validScreenshotPath(to, false) {
		return "", errors.New("неверное имя снимка")
	}
	if err = moveScreenshot(root, path, to); err != nil {
		return "", err
	}
	return screenshotID(to), nil
}

func (a *App) OpenGameScreenshot(id string, folder bool) error {
	root, path, err := a.openScreenshot(id, false)
	if err != nil {
		return err
	}
	defer root.Close()
	if folder {
		path = filepath.Dir(path)
	}
	target, err := windows.UTF16PtrFromString(filepath.Join(root.Name(), path))
	if err != nil {
		return err
	}
	return windows.ShellExecute(0, nil, target, nil, nil, windows.SW_SHOWNORMAL)
}
