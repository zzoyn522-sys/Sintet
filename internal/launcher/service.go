package launcher

import (
	"context"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type State struct {
	GameVersion  string  `json:"-"`
	GameLoader   string  `json:"-"`
	GameBuild    string  `json:"-"`
	GameStarted  int64   `json:"-"`
	GameAccount  string  `json:"-"`
	PID          int     `json:"pid,omitempty"`
	Directory    string  `json:"directory,omitempty"`
	Busy         bool    `json:"busy"`
	Running      bool    `json:"running"`
	Profile      string  `json:"profile"`
	Stage        string  `json:"stage"`
	Percent      float64 `json:"percent"`
	Done         int64   `json:"done"`
	Total        int64   `json:"total"`
	Files        int64   `json:"files"`
	FileCount    int64   `json:"fileCount"`
	Error        string  `json:"error"`
	ErrorDetails string  `json:"errorDetails,omitempty"`
	Log          string  `json:"log"`
}
type Service struct {
	Root          string
	HTTP          *http.Client
	mu            sync.Mutex
	state         State
	cancel        context.CancelFunc
	done          chan struct{}
	gameCancel    context.CancelFunc
	manifest      Manifest
	manifestMu    sync.Mutex
	diskAvailable func(string) (uint64, error)
}

func New(root string) *Service  { return &Service{Root: root, HTTP: newHTTP()} }
func (s *Service) State() State { s.mu.Lock(); defer s.mu.Unlock(); return s.state }

func (s *Service) ProfileIdentity(id string) (InstalledProfile, error) {
	if err := validateID(id); err != nil {
		return InstalledProfile{}, err
	}
	r, err := s.readReceipt(id)
	if err != nil {
		return InstalledProfile{}, err
	}
	return InstalledProfile{ID: r.ID, Base: r.Base, Loader: r.Loader, LoaderVersion: r.LoaderVersion}, nil
}
func (s *Service) stage(text string) {
	s.mu.Lock()
	s.state.Stage = text
	s.state.Percent = 0
	s.state.Done = 0
	s.state.Total = 0
	s.state.Files = 0
	s.state.FileCount = 0
	s.mu.Unlock()
}
func (s *Service) setProgress(done, total, files, count int64) {
	s.mu.Lock()
	s.state.Done = done
	s.state.Total = total
	s.state.Files = files
	s.state.FileCount = count
	if total > 0 {
		s.state.Percent = float64(done) * 100 / float64(total)
		if s.state.Percent > 99.9 {
			s.state.Percent = 99.9
		}
	}
	s.mu.Unlock()
}
func (s *Service) receiptPath(id string) string {
	return filepath.Join(s.Root, ".sintet", "installed", id+".json")
}
func (s *Service) Installed(id string) bool {
	r, err := s.readReceipt(id)
	if err != nil {
		return false
	}
	for _, f := range r.Files {
		p, e := join(s.Root, f.Path)
		if e != nil || !validFile(p, f, false) {
			return false
		}
	}
	st, e := os.Stat(r.Java)
	return e == nil && !st.IsDir()
}
func (s *Service) ListVersions() (Catalog, error) {
	s.manifestMu.Lock()
	defer s.manifestMu.Unlock()
	cache := filepath.Join(s.Root, ".sintet", "cache", "manifest.json")
	cached := false
	var m Manifest
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err := jsonURL(ctx, s.HTTP, manifestURL, &m)
	if err != nil {
		cached = true
		if e := readJSON(cache, &m); e != nil {
			return Catalog{}, fmt.Errorf("не удалось получить список версий: %w", err)
		}
	} else {
		if len(m.Versions) == 0 {
			return Catalog{}, fmt.Errorf("пустой список версий")
		}
		_ = saveJSON(cache, m)
	}
	s.manifest = m
	for i := range m.Versions {
		m.Versions[i].Installed = s.Installed(m.Versions[i].ID)
	}
	return Catalog{m.Versions, m.Latest.Release, cached}, nil
}
func (s *Service) baseProfile(ctx context.Context, id string) (Profile, error) {
	s.manifestMu.Lock()
	m := s.manifest
	s.manifestMu.Unlock()
	if len(m.Versions) == 0 {
		if _, e := s.ListVersions(); e != nil {
			return Profile{}, e
		}
		s.manifestMu.Lock()
		m = s.manifest
		s.manifestMu.Unlock()
	}
	var entry *Version
	for i := range m.Versions {
		if m.Versions[i].ID == id {
			entry = &m.Versions[i]
			break
		}
	}
	if entry == nil {
		return Profile{}, fmt.Errorf("версия %s отсутствует в манифесте", id)
	}
	f := File{Path: "versions/" + id + "/" + id + ".json", URL: entry.URL, SHA1: entry.SHA1}
	if e := s.download(ctx, f, nil); e != nil {
		return Profile{}, e
	}
	var p Profile
	path, _ := join(s.Root, f.Path)
	e := readJSON(path, &p)
	return p, e
}
func (s *Service) LoaderVersions(base, kind string) ([]string, error) {
	if e := validateID(base); e != nil {
		return nil, e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 40*time.Second)
	defer cancel()
	cache := filepath.Join(s.Root, ".sintet", "cache", kind+"-"+base+".json")
	result := []string{}
	var err error
	switch kind {
	case "vanilla":
		return []string{}, nil
	case "fabric":
		var data []struct {
			Loader struct {
				Version string `json:"version"`
			} `json:"loader"`
		}
		err = jsonURL(ctx, s.HTTP, "https://meta.fabricmc.net/v2/versions/loader/"+base, &data)
		for _, v := range data {
			result = append(result, v.Loader.Version)
		}
	case "forge", "neoforge":
		url := "https://maven.minecraftforge.net/net/minecraftforge/forge/maven-metadata.xml"
		if kind == "neoforge" {
			url = "https://maven.neoforged.net/releases/net/neoforged/neoforge/maven-metadata.xml"
		}
		var data []byte
		data, err = fetch(ctx, s.HTTP, url)
		if err == nil {
			var metadata struct {
				Versions []string `xml:"versioning>versions>version"`
			}
			err = xml.Unmarshal(data, &metadata)
			prefix := base + "-"
			if kind == "neoforge" {
				parts := strings.Split(strings.TrimPrefix(base, "1."), ".")
				if len(parts) < 2 {
					parts = append(parts, "0")
				}
				prefix = strings.Join(parts, ".") + "."
			}
			for _, v := range metadata.Versions {
				if strings.HasPrefix(v, prefix) {
					result = append(result, v)
				}
			}
			sort.SliceStable(result, func(i, j int) bool { return versionLess(result[j], result[i]) })
		}
	default:
		return nil, fmt.Errorf("неизвестный загрузчик")
	}
	if err != nil {
		if readJSON(cache, &result) == nil {
			return result, nil
		}
		return nil, err
	}
	_ = saveJSON(cache, result)
	return result, nil
}
func versionLess(a, b string) bool {
	split := func(v string) []int {
		var nums []int
		for _, p := range strings.FieldsFunc(v, func(r rune) bool { return r < '0' || r > '9' }) {
			var n int
			fmt.Sscanf(p, "%d", &n)
			nums = append(nums, n)
		}
		return nums
	}
	x, y := split(a), split(b)
	for i := 0; i < len(x) && i < len(y); i++ {
		if x[i] != y[i] {
			return x[i] < y[i]
		}
	}
	return len(x) < len(y)
}
func ProfileID(base, kind, version string) string {
	if kind == "vanilla" {
		return base
	}
	return kind + "-" + version + "-" + base
}
func (s *Service) StartInstall(base, kind, loaderVersion string) error {
	return s.StartInstallWithJava(base, kind, loaderVersion, "")
}
func (s *Service) StartInstallWithJava(base, kind, loaderVersion, javaPath string) error {
	return s.startInstallAs(ProfileID(base, kind, loaderVersion), base, kind, loaderVersion, javaPath)
}

func (s *Service) StartInstallAs(id, base, kind, loaderVersion, javaPath string) error {
	return s.startInstallAs(id, base, kind, loaderVersion, javaPath)
}

func (s *Service) startInstallAs(id, base, kind, loaderVersion, javaPath string) error {
	if e := validateID(id); e != nil {
		return e
	}
	for _, id := range []string{base, kind} {
		if e := validateID(id); e != nil {
			return e
		}
	}
	if kind != "vanilla" {
		if e := validateID(loaderVersion); e != nil {
			return e
		}
	}
	if kind != "vanilla" && kind != "fabric" && kind != "forge" && kind != "neoforge" {
		return fmt.Errorf("неизвестный загрузчик")
	}
	s.mu.Lock()
	if s.state.Busy || s.state.Running {
		s.mu.Unlock()
		return fmt.Errorf("дождитесь завершения текущей операции")
	}
	ctx, cancel := context.WithCancel(context.Background())
	s.cancel = cancel
	s.done = make(chan struct{})
	done := s.done
	s.state = State{Busy: true, Profile: id, Stage: "Подготовка установки"}
	s.mu.Unlock()
	go func() {
		err := s.install(ctx, id, base, kind, loaderVersion, javaPath)
		s.mu.Lock()
		s.state.Busy = false
		s.cancel = nil
		if err != nil {
			if ctx.Err() != nil {
				s.state.Stage = "Установка отменена"
			} else {
				s.state.Stage = "Не удалось установить"
				s.state.Error = friendlyStorageError(err, s.Root).Error()
				s.state.ErrorDetails = storageDetails(err)
			}
		} else {
			s.state.Stage = "Установлено"
			s.state.Percent = 100
		}
		close(done)
		s.mu.Unlock()
	}()
	return nil
}
func (s *Service) WaitInstall() {
	s.mu.Lock()
	done := s.done
	s.mu.Unlock()
	if done != nil {
		<-done
	}
}
func (s *Service) Cancel() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cancel != nil {
		s.cancel()
	}
}
func (s *Service) Shutdown() { s.Cancel(); s.WaitInstall() }
func (s *Service) download(ctx context.Context, f File, progress func(int64)) error {
	path, e := join(s.Root, f.Path)
	if e != nil {
		return e
	}
	if validFile(path, f, true) {
		if progress != nil {
			progress(f.Size)
		}
		return nil
	}
	if f.URL == "" {
		return fmt.Errorf("отсутствует файл %s; установите профиль заново", f.Path)
	}
	if e = os.MkdirAll(filepath.Dir(path), 0755); e != nil {
		return e
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * time.Second):
			}
		}
		last = func() error {
			resp, err := request(ctx, s.HTTP, f.URL)
			if err != nil {
				return err
			}
			defer resp.Body.Close()
			required := f.Size
			if resp.ContentLength > required {
				required = resp.ContentLength
			}
			if err := s.checkDiskSpace(filepath.Dir(path), required); err != nil {
				return err
			}
			out, err := os.CreateTemp(filepath.Dir(path), ".download-*")
			if err != nil {
				return err
			}
			tmp := out.Name()
			defer os.Remove(tmp)
			n, err := io.Copy(out, resp.Body)
			closeErr := out.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
			if !validFile(tmp, f, true) {
				return fmt.Errorf("проверка размера/хеша не пройдена: %s", f.Path)
			}
			if err = os.Rename(tmp, path); err != nil {
				return err
			}
			if progress != nil {
				if f.Size > 0 {
					progress(f.Size)
				} else {
					progress(n)
				}
			}
			return nil
		}()
		if last == nil {
			return nil
		}
		if IsDiskFull(last) {
			return friendlyStorageError(last, path)
		}
	}
	return fmt.Errorf("%s: %w", f.Path, last)
}
func (s *Service) downloadAll(ctx context.Context, files []File) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan File)
	var done, completed atomic.Int64
	var total int64
	var required int64
	missing := make([]File, 0, len(files))
	for _, f := range files {
		if err := ctx.Err(); err != nil {
			return err
		}
		total += f.Size
		path, err := join(s.Root, f.Path)
		if err != nil {
			return err
		}
		if validFile(path, f, true) {
			done.Add(f.Size)
			completed.Add(1)
		} else {
			missing = append(missing, f)
			required += max(0, f.Size)
		}
	}
	if err := s.checkDiskSpace(s.Root, required); err != nil {
		return err
	}
	s.setProgress(done.Load(), total, completed.Load(), int64(len(files)))
	var wg sync.WaitGroup
	var once sync.Once
	var first error
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for f := range jobs {
				if ctx.Err() != nil {
					return
				}
				e := s.download(ctx, f, func(n int64) { done.Add(n) })
				if e != nil {
					once.Do(func() { first = e; cancel() })
					return
				}
				completed.Add(1)
				s.setProgress(done.Load(), total, completed.Load(), int64(len(files)))
			}
		}()
	}
loop:
	for _, f := range missing {
		select {
		case jobs <- f:
		case <-ctx.Done():
			break loop
		}
	}
	close(jobs)
	wg.Wait()
	if first != nil {
		return first
	}
	return ctx.Err()
}
func (s *Service) Logs() string {
	path := s.currentLogPath()
	if path == "" {
		return ""
	}
	f, e := os.Open(path)
	if e != nil {
		return e.Error()
	}
	defer f.Close()
	if st, err := f.Stat(); err == nil && st.Size() > 100000 {
		_, _ = f.Seek(-100000, io.SeekEnd)
	}
	data, _ := io.ReadAll(io.LimitReader(f, 100000))
	return string(data)
}

func (s *Service) currentLogPath() string {
	s.mu.Lock()
	path := s.state.Log
	s.mu.Unlock()
	if path == "" {
		return s.latestGameLog()
	}
	return path
}

var gameLogName = regexp.MustCompile(`^[A-Za-z0-9_.+\-]+-\d{8}-\d{6}(\.\d{9})?\.log$`)

func (s *Service) latestGameLog() string {
	dir := filepath.Join(s.Root, ".sintet", "logs")
	entries, _ := os.ReadDir(dir)
	var latest string
	var modified time.Time
	for _, entry := range entries {
		if !entry.Type().IsRegular() || !gameLogName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err == nil && (latest == "" || info.ModTime().After(modified)) {
			latest, modified = entry.Name(), info.ModTime()
		}
	}
	if latest == "" {
		return ""
	}
	return filepath.Join(dir, latest)
}
func (s *Service) readReceipt(id string) (Receipt, error) {
	var r Receipt
	if e := validateID(id); e != nil {
		return r, e
	}
	e := readJSON(s.receiptPath(id), &r)
	if e == nil && r.ID != id {
		e = fmt.Errorf("идентификатор в записи установки не совпадает с выбранным профилем")
	}
	if e == nil {
		for _, value := range []string{r.Base, r.Loader, r.AssetIndexID} {
			if value != "" && validateID(value) != nil {
				return r, fmt.Errorf("запись установки содержит некорректный идентификатор")
			}
		}
	}
	return r, e
}

// Keep raw JSON utility local for installer-generated metadata.
func rawStrings(values ...string) []json.RawMessage {
	r := make([]json.RawMessage, 0, len(values))
	for _, s := range values {
		v, _ := json.Marshal(s)
		r = append(r, v)
	}
	return r
}
func (s *Service) Installations() []InstalledProfile {
	result := []InstalledProfile{}
	paths, _ := filepath.Glob(filepath.Join(s.Root, ".sintet", "installed", "*.json"))
	for _, path := range paths {
		var r Receipt
		if readJSON(path, &r) == nil && filepath.Base(path) == r.ID+".json" && s.Installed(r.ID) {
			result = append(result, InstalledProfile{ID: r.ID, Base: r.Base, Loader: r.Loader, LoaderVersion: r.LoaderVersion})
		}
	}
	return result
}

type InstalledProfile struct {
	ID            string `json:"id"`
	Name          string `json:"name,omitempty"`
	Icon          string `json:"icon,omitempty"`
	Base          string `json:"base"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
}
