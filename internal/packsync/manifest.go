package packsync

import (
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

const MaxFiles = 6000
const MaxTotal = int64(2 << 30)
const MaxMod = int64(256 << 20)
const MaxConfig = int64(4 << 20)
const MaxManifest = 2 << 20

var hashPattern = regexp.MustCompile(`^[a-f0-9]{64}$`)
var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_.+\-]{1,80}$`)

type File struct {
	Path             string   `json:"path"`
	Size             int64    `json:"size"`
	SHA256           string   `json:"sha256"`
	SHA1             string   `json:"sha1,omitempty"`
	CurseFingerprint uint32   `json:"curseFingerprint,omitempty"`
	Sources          []Source `json:"sources,omitempty"`
}
type Manifest struct {
	Format        int    `json:"format"`
	Name          string `json:"name"`
	Game          string `json:"game"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	Files         []File `json:"files"`
	Digest        string `json:"digest"`
}
type Snapshot struct {
	Manifest Manifest
	Cache    string
	slots    chan struct{}
}

func transientConfigPath(value string) bool {
	parts := strings.Split(strings.ToLower(value), "/")
	if len(parts) < 2 || (parts[0] != "config" && parts[0] != "defaultconfigs") {
		return false
	}
	for _, part := range parts[1:] {
		switch part {
		case "log", "logs", "cache", "caches", ".cache", "crash-reports", "backup", "backups":
			return true
		}
	}
	return false
}

func AllowedPath(value string) bool {
	if len(value) > 240 || strings.ContainsAny(value, "\\:\x00\r\n") || strings.HasPrefix(value, "/") || path.Clean(value) != value {
		return false
	}
	parts := strings.Split(value, "/")
	if len(parts) < 2 {
		return false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		for _, r := range part {
			if r < 32 || strings.ContainsRune(`<>"|?*`, r) {
				return false
			}
		}
		stem := strings.ToUpper(strings.TrimSpace(strings.SplitN(part, ".", 2)[0]))
		if stem == "CON" || stem == "PRN" || stem == "AUX" || stem == "NUL" || stem == "CONIN$" || stem == "CONOUT$" || regexp.MustCompile(`^(COM|LPT)[1-9¹²³]$`).MatchString(stem) {
			return false
		}
	}
	if parts[0] == "mods" {
		return len(parts) == 2 && strings.EqualFold(path.Ext(value), ".jar")
	}
	if parts[0] != "config" && parts[0] != "defaultconfigs" {
		return false
	}
	lower := strings.ToLower(value)
	for _, secret := range []string{"account", "password", "credential", "secret", "session", "token", "auth"} {
		if strings.Contains(lower, secret) {
			return false
		}
	}
	switch strings.ToLower(path.Ext(value)) {
	case ".json", ".json5", ".toml", ".cfg", ".conf", ".properties", ".yaml", ".yml", ".txt", ".ini", ".xml", ".snbt", ".nbt", ".dat":
		return true
	}
	return false
}
func fingerprint(m Manifest) string {
	m.Digest = ""
	m.Name = ""
	m.Files = append([]File{}, m.Files...)
	for i := range m.Files {
		m.Files[i].Sources = nil
		m.Files[i].CurseFingerprint = 0
	}
	data, _ := json.Marshal(m)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func Validate(m Manifest) error {
	if (m.Format != 1 && m.Format != 2) || !tokenPattern.MatchString(m.Game) || len(m.Name) > 160 || strings.ContainsAny(m.Name, "\x00\r\n") || len(m.Files) > MaxFiles {
		return errors.New("некорректный манифест сборки")
	}
	if m.Game[0] < '0' || m.Game[0] > '9' || strings.Contains(m.Game, "..") {
		return errors.New("некорректная версия Minecraft")
	}
	if m.Loader != "vanilla" && m.Loader != "forge" && m.Loader != "fabric" && m.Loader != "neoforge" {
		return errors.New("неподдерживаемый загрузчик")
	}
	if (m.Loader != "vanilla" && !tokenPattern.MatchString(m.LoaderVersion)) || (m.Loader == "vanilla" && m.LoaderVersion != "") {
		return errors.New("некорректная версия загрузчика")
	}
	var total int64
	seen := map[string]bool{}
	prev := ""
	for _, f := range m.Files {
		if len(f.Sources) > 3 {
			return errors.New("слишком много источников файла")
		}
		for _, source := range f.Sources {
			if !strings.HasPrefix(f.Path, "mods/") || !validSource(source) {
				return errors.New("неизвестный источник файла сборки")
			}
		}
		key := strings.ToLower(f.Path)
		limit := MaxConfig
		if strings.HasPrefix(f.Path, "mods/") {
			limit = MaxMod
		}
		if !AllowedPath(f.Path) || !hashPattern.MatchString(f.SHA256) || f.Size < 0 || f.Size > limit || seen[key] || f.Path <= prev {
			return errors.New("некорректный файл в манифесте")
		}
		if f.SHA1 != "" && (len(f.SHA1) != 40 || strings.Trim(f.SHA1, "0123456789abcdef") != "") {
			return errors.New("некорректный хеш файла")
		}
		total += f.Size
		if total > MaxTotal {
			return errors.New("сборка превышает лимит 2 ГБ")
		}
		seen[key] = true
		prev = f.Path
	}
	if !hashPattern.MatchString(m.Digest) || m.Digest != fingerprint(m) {
		return errors.New("манифест повреждён")
	}
	if data, _ := json.Marshal(m); len(data) > MaxManifest {
		return errors.New("манифест сборки превышает лимит 2 МБ")
	}
	return nil
}
func safeSource(root, file string) bool {
	rel, err := filepath.Rel(root, file)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}
	cur := root
	for _, part := range append([]string{""}, strings.Split(rel, string(os.PathSeparator))...) {
		if part != "" {
			cur = filepath.Join(cur, part)
		}
		info, err := os.Lstat(cur)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func Scan(ctx context.Context, root string, m Manifest, cache string) (Snapshot, error) {
	return scanFolders(ctx, root, m, cache, []string{"mods", "config", "defaultconfigs"})
}

// Runtime config/cache files must not affect the inventory used for joining.
func ScanMods(ctx context.Context, root string, m Manifest) (Snapshot, error) {
	return scanFolders(ctx, root, m, "", []string{"mods"})
}

func scanFolders(ctx context.Context, root string, m Manifest, cache string, folders []string) (Snapshot, error) {
	m.Format = 2
	m.Files = []File{}
	var total int64
	for _, folder := range folders {
		base := filepath.Join(root, folder)
		if _, err := os.Lstat(base); os.IsNotExist(err) {
			continue
		}
		err := filepath.WalkDir(base, func(file string, entry fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if err = ctx.Err(); err != nil {
				return err
			}
			rel, _ := filepath.Rel(root, file)
			rel = filepath.ToSlash(rel)
			if transientConfigPath(rel) {
				if entry.IsDir() {
					return fs.SkipDir
				}
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				return errors.New("в папках сборки обнаружена ссылка; уберите её перед созданием лобби")
			}
			if entry.IsDir() {
				return nil
			}
			if !AllowedPath(rel) {
				return nil
			}
			if !entry.Type().IsRegular() || !safeSource(root, file) {
				return errors.New("небезопасный путь файла сборки")
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			limit := MaxConfig
			if folder == "mods" {
				limit = MaxMod
			}
			if info.Size() > limit || len(m.Files) >= MaxFiles || total+info.Size() > MaxTotal {
				return errors.New("сборка превышает лимиты передачи")
			}
			input, err := os.Open(file)
			if err != nil {
				return err
			}
			defer input.Close()
			a, b := sha256.New(), sha1.New()
			counter := &curseByteCounter{}
			writer := io.MultiWriter(a, b)
			var temp *os.File
			if cache != "" {
				if err = os.MkdirAll(cache, 0700); err != nil {
					return err
				}
				temp, err = os.CreateTemp(cache, "pack-*.tmp")
				if err != nil {
					return err
				}
				defer os.Remove(temp.Name())
				writer = io.MultiWriter(a, b, temp)
				if folder == "mods" {
					writer = io.MultiWriter(a, b, temp, counter)
				}
			}
			n, err := io.Copy(writer, contextInput{ctx, io.LimitReader(input, limit+1)})
			after, e := input.Stat()
			if err == nil && (e != nil || n != info.Size() || n > limit || !after.ModTime().Equal(info.ModTime())) {
				err = errors.New("файл изменился во время сверки; повторите создание лобби")
			}
			if temp != nil {
				if err == nil {
					err = temp.Sync()
				}
				closeErr := temp.Close()
				if err == nil {
					err = closeErr
				}
			}
			if err != nil {
				return err
			}
			digest := hex.EncodeToString(a.Sum(nil))
			if temp != nil {
				dest := filepath.Join(cache, digest)
				if err = os.Rename(temp.Name(), dest); err != nil {
					if !ValidCached(cache, File{SHA256: digest, Size: n}) {
						return err
					}
				}
			}
			item := File{Path: rel, Size: n, SHA256: digest, SHA1: hex.EncodeToString(b.Sum(nil))}
			if temp != nil && folder == "mods" {
				item.CurseFingerprint, err = curseFileFingerprint(ctx, filepath.Join(cache, digest), counter.n)
				if err != nil {
					return err
				}
			}
			m.Files = append(m.Files, item)
			total += n
			return nil
		})
		if err != nil {
			return Snapshot{}, err
		}
	}
	sort.Slice(m.Files, func(i, j int) bool { return m.Files[i].Path < m.Files[j].Path })
	m.Digest = fingerprint(m)
	if err := Validate(m); err != nil {
		return Snapshot{}, err
	}
	return Snapshot{Manifest: m, Cache: cache, slots: make(chan struct{}, 2)}, nil
}
func ValidCached(cache string, f File) bool {
	if !hashPattern.MatchString(f.SHA256) {
		return false
	}
	file := filepath.Join(cache, f.SHA256)
	info, e := os.Lstat(file)
	if e != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
		return false
	}
	r, e := os.Open(file)
	if e != nil {
		return false
	}
	defer r.Close()
	h := sha256.New()
	n, e := io.Copy(h, io.LimitReader(r, f.Size+1))
	return e == nil && n == f.Size && hex.EncodeToString(h.Sum(nil)) == f.SHA256
}
func (s Snapshot) Handler() http.Handler {
	if s.slots == nil {
		s.slots = make(chan struct{}, 2)
	}
	data, _ := json.Marshal(s.Manifest)
	files := map[string]File{}
	for _, f := range s.Manifest.Files {
		files[f.SHA256] = f
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != "GET" {
			http.Error(w, "Method not allowed", 405)
			return
		}
		if r.URL.Path == "/manifest" {
			w.Header().Set("Content-Type", "application/json")
			w.Write(data)
			return
		}
		digest := strings.TrimPrefix(r.URL.Path, "/file/")
		f, ok := files[digest]
		if !ok || !hashPattern.MatchString(digest) {
			http.NotFound(w, r)
			return
		}
		select {
		case s.slots <- struct{}{}:
			defer func() { <-s.slots }()
		default:
			w.Header().Set("Retry-After", "1")
			http.Error(w, "Host transfer limit reached", http.StatusTooManyRequests)
			return
		}
		file := filepath.Join(s.Cache, digest)
		if !safeSource(s.Cache, file) {
			http.NotFound(w, r)
			return
		}
		info, err := os.Lstat(file)
		if err != nil || !info.Mode().IsRegular() || info.Size() != f.Size {
			http.Error(w, "Snapshot unavailable", 409)
			return
		}
		input, err := os.Open(file)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer input.Close()
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("Content-Length", fmt.Sprint(f.Size))
		io.CopyN(w, input, f.Size)
	})
}
