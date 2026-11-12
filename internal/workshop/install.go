package workshop

import (
	"archive/zip"
	"context"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha1"
	"crypto/sha256"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"sintet/internal/accountfiles"
)

type Target struct {
	ID        string `json:"id"`
	Name      string `json:"name,omitempty"`
	Game      string `json:"game"`
	Loader    string `json:"loader"`
	Directory string `json:"directory"`
}
type PlanEntry struct {
	Project   Project `json:"project"`
	Version   Version `json:"version"`
	Installed bool    `json:"installed"`
}
type Plan struct {
	Kind     string      `json:"kind"`
	ID       string      `json:"id"`
	Target   Target      `json:"target"`
	Entries  []PlanEntry `json:"entries"`
	Warnings []string    `json:"warnings"`
	Created  time.Time   `json:"-"`
}
type InstallState struct {
	Busy    bool    `json:"busy"`
	Stage   string  `json:"stage"`
	Percent float64 `json:"percent"`
	Error   string  `json:"error"`
	Done    int     `json:"done"`
	Total   int     `json:"total"`
}
type Manager struct {
	Client    *Client
	mu        sync.Mutex
	plans     map[string]Plan
	packPlans map[string]PackPlan
	state     InstallState
	cancel    context.CancelFunc
}

func NewManager(c *Client) *Manager {
	return &Manager{Client: c, plans: map[string]Plan{}, packPlans: map[string]PackPlan{}}
}
func (m *Manager) State() InstallState { m.mu.Lock(); defer m.mu.Unlock(); return m.state }
func (m *Manager) PlanTarget(id string) (Target, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.plans[id]
	if !ok {
		return Target{}, errors.New("план установки не найден")
	}
	return p.Target, nil
}
func (m *Manager) Cancel() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.cancel != nil {
		m.cancel()
	}
}

type Installed struct {
	Provider string `json:"provider"`
	Project  string `json:"project"`
	Version  string `json:"version"`
	Filename string `json:"filename"`
	Name     string `json:"name"`
	Hash     string `json:"sha256"`
	Icon     string `json:"icon"`
}

func registry(dir string) (map[string]Installed, error) {
	data, err := os.ReadFile(filepath.Join(dir, ".sintet-mods.json"))
	if os.IsNotExist(err) {
		return map[string]Installed{}, nil
	}
	if err != nil {
		return nil, err
	}
	records := map[string]Installed{}
	if json.Unmarshal(data, &records) != nil {
		return nil, errors.New("повреждён список установленных модов; существующие файлы сохранены")
	}
	return records, nil
}
func key(provider, id string) string { return provider + ":" + id }
func fileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err = io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func installedFile(dir string, r Installed) bool {
	if !safeFilename(r.Filename) {
		return false
	}
	for _, name := range []string{r.Filename} {
		if h, err := fileHash(filepath.Join(dir, "mods", name)); err == nil && h == r.Hash {
			return true
		}
	}
	return false
}
func (m *Manager) Prepare(ctx context.Context, provider, project, version string, target Target) (Plan, error) {
	p := Plan{Target: target, Entries: []PlanEntry{}, Warnings: []string{}, Created: time.Now()}
	if !contains([]string{"fabric", "forge", "neoforge", "quilt"}, target.Loader) || target.Game == "" || !filepath.IsAbs(target.Directory) {
		return p, errors.New("выберите установленную сборку с модлоадером")
	}
	records, err := registry(target.Directory)
	if err != nil {
		return p, err
	}
	seen := map[string]string{}
	var visit func(string, string, bool) error
	visit = func(id, versionID string, root bool) error {
		if len(seen) >= 64 {
			return errors.New("слишком много зависимостей")
		}
		if id != "" {
			if chosen, ok := seen[id]; ok {
				if versionID != "" && chosen != versionID {
					return errors.New("зависимости требуют разных версий одного мода")
				}
				return nil
			}
		}
		var v Version
		var err error
		if versionID != "" {
			v, err = m.Client.Version(ctx, provider, id, versionID)
		} else {
			var versions []Version
			versions, err = m.Client.Versions(ctx, provider, id, target.Game, target.Loader)
			if err == nil {
				for _, candidate := range versions {
					if candidate.Compatible(target.Game, target.Loader) {
						v = candidate
						break
					}
				}
				if v.ID == "" {
					return fmt.Errorf("нет совместимой зависимости %s для %s · %s", id, target.Game, target.Loader)
				}
			}
		}
		if err != nil {
			return err
		}
		if v.Project == "" || id != "" && v.Project != id || !v.Compatible(target.Game, target.Loader) {
			return errors.New("файл несовместим с выбранной версией Minecraft или загрузчиком")
		}
		id = v.Project
		if chosen, ok := seen[id]; ok {
			if chosen != v.ID {
				return errors.New("конфликт версий зависимостей")
			}
			return nil
		}
		seen[id] = v.ID
		info, err := m.Client.Project(ctx, provider, id)
		if err != nil {
			return err
		}
		if v.URL == "" {
			return fmt.Errorf("автор %s не разрешил автоматическое скачивание этого файла", info.Name)
		}
		current := records[key(provider, id)]
		present := current.Version == v.ID && installedFile(target.Directory, current)
		p.Entries = append(p.Entries, PlanEntry{Project: info, Version: v, Installed: present})
		for _, d := range v.Dependencies {
			switch d.Kind {
			case "required":
				if d.Project == "" && d.Version == "" {
					return fmt.Errorf("у %s указана неизвестная обязательная зависимость", info.Name)
				}
				if err := visit(d.Project, d.Version, false); err != nil {
					return err
				}
			case "incompatible":
				if r, ok := records[key(provider, d.Project)]; ok && installedFile(target.Directory, r) {
					return fmt.Errorf("%s несовместим с установленным %s", info.Name, r.Name)
				}
			case "optional":
				p.Warnings = append(p.Warnings, "У "+info.Name+" есть необязательные дополнения")
			}
		}
		return nil
	}
	if err := visit(project, version, true); err != nil {
		return p, err
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return p, err
	}
	p.ID = hex.EncodeToString(token[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, old := range m.plans {
		if time.Since(old.Created) > 10*time.Minute {
			delete(m.plans, id)
		}
	}
	m.plans[p.ID] = p
	return p, nil
}
func safeFilename(name string) bool {
	return name != "" && len(name) < 200 && filepath.Base(name) == name && !strings.ContainsAny(name, `/\:`) && !strings.HasSuffix(name, ".") && !strings.HasSuffix(name, " ") && strings.HasSuffix(strings.ToLower(name), ".jar")
}
func allowedDownload(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return false
	}
	if u.Scheme == "http" && u.Hostname() == "127.0.0.1" {
		return true
	}
	if u.Scheme == "https" && u.Host == "sintet.xyz" && strings.HasPrefix(u.Path, "/api/catalog/files/") {
		return true
	}
	return u.Scheme == "https" && (u.Hostname() == "cdn.modrinth.com" || u.Hostname() == "edge.forgecdn.net" || u.Hostname() == "mediafilez.forgecdn.net" || u.Hostname() == "media.forgecdn.net")
}
func (m *Manager) Start(ctx context.Context, id string, withDependencies bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy {
		return errors.New("дождитесь завершения установки мода")
	}
	plan, ok := m.plans[id]
	if !ok || time.Since(plan.Created) > 10*time.Minute {
		return errors.New("план установки устарел; выберите мод заново")
	}
	delete(m.plans, id)
	if !withDependencies && len(plan.Entries) > 1 {
		plan.Entries = plan.Entries[:1]
	}
	run, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.state = InstallState{Busy: true, Stage: "Подготовка модов", Total: len(plan.Entries)}
	go func() {
		defer cancel()
		err := m.install(run, plan)
		if err != nil && run.Err() != nil {
			err = errors.New("установка отменена")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.state.Busy = false
		m.cancel = nil
		if err != nil {
			m.state.Error = err.Error()
			m.state.Stage = "Установка не завершена"
		} else {
			m.state.Stage = "Установлено"
			m.state.Percent = 100
		}
	}()
	return nil
}
func (m *Manager) install(ctx context.Context, plan Plan) error {
	if isZipPack(plan.Kind) {
		return m.installResources(ctx, plan)
	}
	dir := plan.Target.Directory
	if err := os.MkdirAll(filepath.Join(dir, "mods"), 0755); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".sintet-mod-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	records, err := registry(dir)
	if err != nil {
		return err
	}
	type pending struct {
		entry      PlanEntry
		path, hash string
	}
	files := []pending{}
	names := map[string]bool{}
	for i, entry := range plan.Entries {
		v := entry.Version
		if !safeFilename(v.Filename) || names[strings.ToLower(v.Filename)] {
			return errors.New("каталог вернул небезопасное или повторяющееся имя файла")
		}
		names[strings.ToLower(v.Filename)] = true
		if current, ok := records[key(entry.Project.Provider, v.Project)]; ok && current.Version == v.ID && installedFile(dir, current) {
			continue
		}
		m.mu.Lock()
		m.state.Stage = "Скачивание " + entry.Project.Name
		m.mu.Unlock()
		path := filepath.Join(stage, v.Filename)
		digest, err := m.download(ctx, v, path, func(done, total int64) {
			m.mu.Lock()
			defer m.mu.Unlock()
			fraction := 0.0
			if total > 0 {
				fraction = float64(done) / float64(total)
			}
			m.state.Percent = (float64(i) + fraction) * 95 / float64(len(plan.Entries))
			m.state.Done = i
		})
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Project.Name, err)
		}
		files = append(files, pending{entry, path, digest})
	}
	if err := ctx.Err(); err != nil {
		return errors.New("установка отменена")
	}
	// Prevent duplicate Fabric/Forge IDs, including mods manually copied by the player.
	replacing := map[string]bool{}
	for _, file := range files {
		if old, ok := records[key(file.entry.Project.Provider, file.entry.Version.Project)]; ok {
			replacing[old.Filename] = true
		}
	}
	ids := map[string]string{}
	installed, _ := os.ReadDir(filepath.Join(dir, "mods"))
	for _, item := range installed {
		if item.IsDir() || !strings.HasSuffix(item.Name(), ".jar") || replacing[item.Name()] {
			continue
		}
		for _, id := range modIDs(filepath.Join(dir, "mods", item.Name())) {
			ids[id] = item.Name()
		}
	}
	for _, file := range files {
		for _, id := range modIDs(file.path) {
			if previous, ok := ids[id]; ok {
				return fmt.Errorf("мод %s уже есть в %s; отключите его перед установкой другой версии", id, previous)
			}
			ids[id] = file.entry.Version.Filename
		}
	}
	// Stage every file before touching the live mods directory. Roll back the complete batch on error.
	type moved struct{ from, to string }
	backups := []moved{}
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = os.Remove(path)
			}
			for i := len(backups) - 1; i >= 0; i-- {
				_ = os.Rename(backups[i].to, backups[i].from)
			}
		}
	}()
	backup := func(path string) error {
		if _, err := os.Lstat(path); os.IsNotExist(err) {
			return nil
		} else if err != nil {
			return err
		}
		to := filepath.Join(stage, fmt.Sprintf("backup-%d", len(backups)))
		if err := os.Rename(path, to); err != nil {
			return err
		}
		backups = append(backups, moved{path, to})
		return nil
	}
	for _, file := range files {
		v, p := file.entry.Version, file.entry.Project
		k := key(p.Provider, v.Project)
		old, known := records[k]
		if known && safeFilename(old.Filename) {
			oldPath := filepath.Join(dir, "mods", old.Filename)
			if _, err := os.Stat(oldPath + ".disabled"); err == nil {
				oldPath += ".disabled"
			}
			if _, err := os.Lstat(oldPath); err == nil {
				sum, e := fileHash(oldPath)
				if e != nil || sum != old.Hash {
					return errors.New("установленный файл изменён вручную; обновление остановлено")
				}
				if err := backup(oldPath); err != nil {
					return err
				}
			}
		}
		dest := filepath.Join(dir, "mods", v.Filename)
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("файл %s уже существует; чужой мод не будет перезаписан", v.Filename)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := localNameAvailable(dest + disabledSuffix); err != nil {
			return err
		}
		if err := os.Rename(file.path, dest); err != nil {
			return err
		}
		created = append(created, dest)
		records[k] = Installed{Provider: p.Provider, Project: v.Project, Version: v.ID, Filename: v.Filename, Name: p.Name, Hash: file.hash, Icon: p.Icon}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err = accountfiles.Write(filepath.Join(dir, ".sintet-mods.json"), data); err != nil {
		return err
	}
	committed = true
	return nil
}

type progressWriter struct {
	w           io.Writer
	done, total int64
	progress    func(int64, int64)
}

func (w *progressWriter) Write(data []byte) (int, error) {
	n, err := w.w.Write(data)
	w.done += int64(n)
	w.progress(w.done, w.total)
	return n, err
}
func (m *Manager) download(ctx context.Context, v Version, path string, progress func(int64, int64)) (string, error) {
	allowed := allowedDownload
	if v.Kind == "packfile" {
		allowed = allowedPackDownload
	}
	urls, seen := []string{}, map[string]bool{}
	for _, u := range append([]string{v.URL}, v.URLs...) {
		if allowed(u) && !seen[u] && len(urls) < 8 {
			urls, seen[u] = append(urls, u), true
		}
	}
	if len(urls) == 0 {
		return "", errors.New("нет разрешённого источника скачивания")
	}
	v.URLs = urls
	var last error
	for attempt := 0; attempt < max(3, len(urls)); attempt++ {
		if ctx.Err() != nil {
			return "", ctx.Err()
		}
		v.URL = urls[attempt%len(urls)]
		digest, err := m.downloadOnce(ctx, v, path, progress)
		if err == nil {
			return digest, nil
		}
		last = err
		var temporary *transferError
		if !errors.As(err, &temporary) || attempt == max(3, len(urls))-1 {
			return "", err
		}
		progress(0, v.Size)
		if err = waitTransferRetry(ctx, attempt, err); err != nil {
			return "", err
		}
	}
	return "", last
}

var errDownloadRedirect = errors.New("неизвестный сервер скачивания")

func packFileDigest(algorithm string) (hash.Hash, error) {
	switch algorithm {
	case "sha256":
		return sha256.New(), nil
	case "sha512":
		return sha512.New(), nil
	case "sha1":
		return sha1.New(), nil
	case "md5":
		return md5.New(), nil
	default:
		return nil, errors.New("каталог не предоставил контрольную сумму")
	}
}

func (m *Manager) downloadOnce(ctx context.Context, v Version, path string, progress func(int64, int64)) (string, error) {
	limit := int64(256 << 20)
	if isZipPack(v.Kind) || v.Kind == "modpacks" {
		limit = 1 << 30
	}
	allowed := allowedDownload
	if v.Kind == "packfile" {
		allowed = allowedPackDownload
		limit = 1 << 30
	}
	if !allowed(v.URL) || !safeContentFilename(v.Filename, v.Kind) || v.Size < 0 || (v.Size == 0 && v.Kind != "packfile") || v.Size > limit {
		return "", errors.New("недопустимый файл для скачивания")
	}
	expected, err := packFileDigest(v.Algorithm)
	if err != nil {
		return "", err
	}
	raw, err := hex.DecodeString(v.Hash)
	if err != nil || len(raw) != expected.Size() {
		return "", errors.New("неверная контрольная сумма")
	}
	request, err := http.NewRequestWithContext(ctx, "GET", v.URL, nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("User-Agent", "Sintet/0.9 (sintet.xyz)")
	downloadClient := *m.Client.HTTP
	downloadClient.Timeout = 3 * time.Minute
	if isZipPack(v.Kind) || v.Kind == "modpacks" || v.Kind == "packfile" {
		downloadClient.Timeout = 10 * time.Minute
	}
	downloadClient.CheckRedirect = func(next *http.Request, via []*http.Request) error {
		if len(via) > 5 || !allowed(next.URL.String()) {
			return errDownloadRedirect
		}
		next.Header.Del("x-api-key")
		next.Header.Del("Authorization")
		next.Header.Del("Cookie")
		return nil
	}
	response, err := downloadClient.Do(request)
	if err != nil {
		if errors.Is(err, errDownloadRedirect) {
			return "", errDownloadRedirect
		}
		return "", &transferError{err: fmt.Errorf("не удалось скачать файл: %w", err)}
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		err := fmt.Errorf("сервер файла вернул %d", response.StatusCode)
		if transientStatus(response.StatusCode) || ((response.StatusCode == 403 || response.StatusCode == 404) && len(v.URLs) > 1) {
			return "", &transferError{err: err, delay: retryDelay(response.Header.Get("Retry-After"))}
		}
		return "", err
	}
	if response.ContentLength > limit {
		return "", errors.New("размер файла превышает лимит загрузки")
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return "", err
	}
	verified := false
	defer func() {
		if !verified {
			_ = os.Remove(path)
		}
	}()
	digest := sha256.New()
	total := v.Size
	if response.ContentLength > 0 {
		total = response.ContentLength
	}
	writer := &progressWriter{w: io.MultiWriter(f, expected, digest), total: total, progress: progress}
	// Pack manifests can contain stale fileSize values. Read the whole bounded
	// response and verify its digest; never truncate a valid JAR to that estimate.
	n, err := io.Copy(writer, io.LimitReader(response.Body, limit+1))
	var networkError net.Error
	if err != nil && (errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) || errors.As(err, &networkError)) {
		err = &transferError{err: fmt.Errorf("скачивание файла прервано: %w", err)}
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return "", err
	}
	if n > limit {
		return "", errors.New("размер файла превышает лимит загрузки")
	}
	if !strings.EqualFold(hex.EncodeToString(expected.Sum(nil)), v.Hash) {
		return "", &transferError{err: fmt.Errorf("контрольная сумма файла не совпала (получено %d байт, в каталоге %d); файл не установлен", n, v.Size)}
	}
	if v.Kind == "packfile" && !strings.HasSuffix(strings.ToLower(v.Filename), ".jar") && !strings.HasSuffix(strings.ToLower(v.Filename), ".zip") {
		verified = true
		return hex.EncodeToString(digest.Sum(nil)), nil
	}
	jar, err := zip.OpenReader(path)
	if err != nil {
		return "", errors.New("скачанный архив повреждён")
	}
	if v.Kind == "shaders" && !isShaderArchive(&jar.Reader) {
		jar.Close()
		return "", errors.New("в ZIP нет папки shaders с файлами шейдеров")
	}
	if v.Kind == "resources" {
		data, e := zipBytes(&jar.Reader, "pack.mcmeta", 1<<20)
		var meta struct{ Pack json.RawMessage }
		if e != nil || json.Unmarshal(data, &meta) != nil || len(meta.Pack) == 0 || string(meta.Pack) == "null" {
			jar.Close()
			return "", errors.New("в ZIP нет корректного pack.mcmeta")
		}
		assets := false
		for _, file := range jar.File {
			if strings.HasPrefix(file.Name, "assets/") || strings.Contains(file.Name, "/assets/") {
				assets = true
				break
			}
		}
		if !assets {
			jar.Close()
			return "", errors.New("этот архив не содержит ресурсов Minecraft")
		}
	}
	jar.Close()
	verified = true
	return hex.EncodeToString(digest.Sum(nil)), nil
}
