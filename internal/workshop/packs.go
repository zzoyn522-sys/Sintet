package workshop

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type PackPlan struct {
	ID            string    `json:"id"`
	Project       Project   `json:"project"`
	Version       string    `json:"version"`
	Game          string    `json:"game"`
	Loader        string    `json:"loader"`
	LoaderVersion string    `json:"loaderVersion"`
	Files         int       `json:"files"`
	Bytes         int64     `json:"bytes"`
	Warnings      []string  `json:"warnings"`
	Created       time.Time `json:"-"`
	folder        string
	entries       []packEntry
}
type packEntry struct {
	Path    string
	File    Version
	Archive string
}

type cursePackReference struct {
	Project  int  `json:"projectID"`
	File     int  `json:"fileID"`
	Required bool `json:"required"`
}

func safePackRelative(value string) bool {
	if value == "" || len(value) > 220 || strings.HasPrefix(value, "/") || strings.ContainsAny(value, "\\:\x00\r\n") || path.Clean(value) != value {
		return false
	}
	for _, part := range strings.Split(value, "/") {
		if part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
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
	return true
}
func safePackPath(value string) bool {
	if !safePackRelative(value) {
		return false
	}
	ext := strings.ToLower(path.Ext(value))
	switch ext {
	case ".exe", ".dll", ".so", ".dylib", ".bat", ".cmd", ".ps1", ".psm1", ".vbs", ".vbe", ".sh", ".lnk", ".url", ".com", ".scr", ".msi", ".msp", ".hta", ".reg", ".cpl", ".sys", ".wsf", ".wsh", ".jse", ".pif", ".scf":
		return false
	}
	parts := strings.Split(strings.ToLower(value), "/")
	for _, part := range parts {
		if strings.HasPrefix(part, ".sintet-") {
			return false
		}
	}
	// Overrides are relative to the game directory, not just config/ and mods/.
	// Packs also ship root configs, licence documents and mod-specific folders.
	// Keep instance identity and launcher-managed binaries outside that namespace.
	switch parts[0] {
	case "instance.json", "settings.json", "accounts.json", "accounts.json.bak", "versions", "libraries", "assets", "runtime", "runtimes", "java", "bin", "saves":
		return false
	}
	return true
}
func allowedPackDownload(raw string) bool {
	if allowedDownload(raw) {
		return true
	}
	u, e := url.Parse(raw)
	if e != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" {
		return false
	}
	switch u.Host {
	case "github.com", "raw.githubusercontent.com", "objects.githubusercontent.com", "release-assets.githubusercontent.com", "gitlab.com":
		return true
	}
	return false
}
func removePackStage(folder string) {
	if filepath.IsAbs(folder) && strings.HasPrefix(filepath.Base(folder), "pack-") {
		if info, e := os.Lstat(folder); e == nil && info.IsDir() && info.Mode()&os.ModeSymlink == 0 {
			_ = os.RemoveAll(folder)
		}
	}
}
func (m *Manager) PreparePack(ctx context.Context, provider, project, version, cacheRoot string) (PackPlan, error) {
	result := PackPlan{Created: time.Now(), Warnings: []string{}}
	if provider != "modrinth" && provider != "curseforge" {
		return result, errors.New("выберите Modrinth или CurseForge")
	}
	info, err := m.Client.Project(ctx, provider, project)
	if err != nil {
		return result, err
	}
	if info.Kind != "modpacks" {
		return result, errors.New("это не сборка")
	}
	v, err := m.Client.Version(ctx, provider, project, version)
	if err != nil {
		return result, err
	}
	if v.Project != info.ID || v.ID != version {
		return result, errors.New("файл не принадлежит выбранной сборке")
	}
	v.Kind = "modpacks"
	if v.URL == "" {
		return result, errors.New("автор сборки запретил автоматическую загрузку этого файла")
	}
	if !filepath.IsAbs(cacheRoot) {
		return result, errors.New("неверная папка кэша")
	}
	if err = os.MkdirAll(cacheRoot, 0700); err != nil {
		return result, err
	}
	folder, err := os.MkdirTemp(cacheRoot, "pack-")
	if err != nil {
		return result, err
	}
	keep := false
	defer func() {
		if !keep {
			removePackStage(folder)
		}
	}()
	archive := filepath.Join(folder, "source.zip")
	if _, err = m.download(ctx, v, archive, func(int64, int64) {}); err != nil {
		return result, err
	}
	result.Project = info
	result.Version = v.ID
	result.folder = folder
	if err = m.parsePack(ctx, archive, &result); err != nil {
		return result, err
	}
	var id [16]byte
	if _, err = rand.Read(id[:]); err != nil {
		return result, err
	}
	result.ID = hex.EncodeToString(id[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, old := range m.packPlans {
		if time.Since(old.Created) > 20*time.Minute {
			delete(m.packPlans, id)
			removePackStage(old.folder)
		}
	}
	if len(m.packPlans) >= 5 {
		for id, old := range m.packPlans {
			delete(m.packPlans, id)
			removePackStage(old.folder)
			break
		}
	}
	m.packPlans[result.ID] = result
	keep = true
	return result, nil
}
func packLoader(game string, deps map[string]string) (string, string, error) {
	if !gameVersion.MatchString(game) {
		return "", "", errors.New("в сборке не указана версия Minecraft")
	}
	loader, version := "vanilla", ""
	for key, v := range deps {
		kind := map[string]string{"fabric-loader": "fabric", "forge": "forge", "neoforge": "neoforge", "quilt-loader": "quilt"}[key]
		if kind == "" {
			if key != "minecraft" {
				return "", "", fmt.Errorf("неподдерживаемая зависимость сборки: %s", key)
			}
			continue
		}
		if loader != "vanilla" {
			return "", "", errors.New("в манифесте несколько загрузчиков")
		}
		loader, version = kind, v
	}
	if loader == "quilt" {
		return "", "", errors.New("для этой сборки нужен Quilt; его поддержка пока не добавлена")
	}
	if loader != "vanilla" && !gameVersion.MatchString(version) {
		return "", "", errors.New("некорректная версия загрузчика")
	}
	if loader == "forge" && !strings.HasPrefix(version, game+"-") {
		version = game + "-" + version
	}
	return loader, version, nil
}
func (m *Manager) parsePack(ctx context.Context, archive string, plan *PackPlan) error {
	reader, err := zip.OpenReader(archive)
	if err != nil {
		return errors.New("архив сборки повреждён")
	}
	defer reader.Close()
	if len(reader.File) > 20000 {
		return errors.New("слишком много файлов в архиве сборки")
	}
	var expanded uint64
	seen := map[string]bool{}
	for _, f := range reader.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := strings.TrimSuffix(f.Name, "/")
		if !safePackRelative(name) || seen[strings.ToLower(name)] || f.Mode()&os.ModeSymlink != 0 || f.Flags&1 != 0 || (!f.FileInfo().IsDir() && !f.Mode().IsRegular()) {
			return errors.New("небезопасные пути или дубликаты в архиве сборки")
		}
		seen[strings.ToLower(name)] = true
		expanded += f.UncompressedSize64
		if expanded > 8<<30 || f.UncompressedSize64 > 1<<30 {
			return errors.New("слишком большой распакованный архив сборки")
		}
	}
	entries := map[string]packEntry{}
	override := "overrides"
	add := func(entry packEntry, replace bool) error {
		parts := strings.SplitN(entry.Path, "/", 2)
		if len(parts) == 2 {
			switch root := strings.ToLower(parts[0]); root {
			case "mods", "config", "defaultconfigs", "resourcepacks", "shaderpacks":
				entry.Path = root + "/" + parts[1]
				if entry.Archive == "" {
					entry.File.Filename = entry.Path
				}
			}
		}
		if !safePackPath(entry.Path) {
			return fmt.Errorf("неподдерживаемый или небезопасный файл сборки: %s", entry.Path)
		}
		key := strings.ToLower(entry.Path)
		if prev, exists := entries[key]; exists && (!replace || prev.Path != entry.Path) {
			return fmt.Errorf("повторяющийся путь в сборке: %s", entry.Path)
		}
		if entry.File.Size < 0 || entry.File.Size > 1<<30 {
			return errors.New("слишком большой файл сборки")
		}
		if entry.Archive == "" {
			digest, err := packFileDigest(entry.File.Algorithm)
			raw, decodeErr := hex.DecodeString(entry.File.Hash)
			if err != nil || decodeErr != nil || len(raw) != digest.Size() {
				return fmt.Errorf("в манифесте нет корректного хеша для %s", entry.Path)
			}
		}
		entries[key] = entry
		return nil
	}
	if plan.Project.Provider == "modrinth" {
		data, e := zipBytes(&reader.Reader, "modrinth.index.json", 4<<20)
		if e != nil {
			return errors.New("в .mrpack нет modrinth.index.json")
		}
		var index struct {
			Format       int               `json:"formatVersion"`
			Game         string            `json:"game"`
			Dependencies map[string]string `json:"dependencies"`
			Files        []struct {
				Path      string            `json:"path"`
				Hashes    map[string]string `json:"hashes"`
				Env       map[string]string `json:"env"`
				Downloads []string          `json:"downloads"`
				Size      int64             `json:"fileSize"`
			} `json:"files"`
		}
		if json.Unmarshal(data, &index) != nil || index.Format != 1 || index.Game != "minecraft" || len(index.Files) > 10000 {
			return errors.New("некорректный формат .mrpack")
		}
		plan.Game = index.Dependencies["minecraft"]
		plan.Loader, plan.LoaderVersion, err = packLoader(plan.Game, index.Dependencies)
		if err != nil {
			return err
		}
		optional := 0
		for _, f := range index.Files {
			if e := ctx.Err(); e != nil {
				return e
			}
			if f.Env["client"] == "unsupported" {
				continue
			}
			if f.Env["client"] == "optional" {
				optional++
				continue
			}
			if f.Env["client"] != "" && f.Env["client"] != "required" {
				return errors.New("некорректная среда файла сборки")
			}
			urls := []string{}
			for _, u := range f.Downloads {
				if allowedPackDownload(u) {
					urls = append(urls, u)
				}
			}
			if len(urls) == 0 {
				return fmt.Errorf("нет разрешённого источника для %s", f.Path)
			}
			algo, hash := "sha512", f.Hashes["sha512"]
			if hash == "" {
				algo, hash = "sha1", f.Hashes["sha1"]
			}
			if e = add(packEntry{Path: f.Path, File: Version{Kind: "packfile", Filename: f.Path, URL: urls[0], URLs: urls, Size: f.Size, Algorithm: algo, Hash: hash}}, false); e != nil {
				return e
			}
		}
		if optional > 0 {
			plan.Warnings = append(plan.Warnings, fmt.Sprintf("Необязательные клиентские файлы пропущены: %d", optional))
		}
	} else {
		data, e := zipBytes(&reader.Reader, "manifest.json", 4<<20)
		if e != nil {
			return errors.New("в сборке CurseForge нет manifest.json")
		}
		var index struct {
			Type      string `json:"manifestType"`
			Version   int    `json:"manifestVersion"`
			Minecraft struct {
				Version string `json:"version"`
				Loaders []struct {
					ID      string `json:"id"`
					Primary bool   `json:"primary"`
				} `json:"modLoaders"`
			} `json:"minecraft"`
			Files     []cursePackReference `json:"files"`
			Overrides string               `json:"overrides"`
		}
		if json.Unmarshal(data, &index) != nil || index.Type != "minecraftModpack" || index.Version != 1 || len(index.Files) > 10000 {
			return errors.New("некорректный формат сборки CurseForge")
		}
		plan.Game = index.Minecraft.Version
		deps := map[string]string{"minecraft": plan.Game}
		chosen := ""
		for _, l := range index.Minecraft.Loaders {
			if chosen == "" || l.Primary {
				chosen = l.ID
				if l.Primary {
					break
				}
			}
		}
		if chosen != "" {
			kind, version, ok := strings.Cut(chosen, "-")
			if !ok {
				return errors.New("неизвестный загрузчик CurseForge")
			}
			if kind == "fabric" {
				kind = "fabric-loader"
			}
			deps[kind] = version
		}
		plan.Loader, plan.LoaderVersion, err = packLoader(plan.Game, deps)
		if err != nil {
			return err
		}
		if index.Overrides != "" {
			override = index.Overrides
		}
		if !safePackRelative(override) {
			return errors.New("небезопасная папка overrides")
		}
		files, e := m.Client.cursePackFiles(ctx, index.Files)
		if e != nil {
			return e
		}
		for _, v := range files {
			if e = add(packEntry{Path: v.Filename, File: v}, false); e != nil {
				return e
			}
		}
	}
	prefixes := []string{override + "/"}
	if plan.Project.Provider == "modrinth" {
		prefixes = append(prefixes, "client-overrides/")
	}
	for _, prefix := range prefixes {
		for _, f := range reader.File {
			if err := ctx.Err(); err != nil {
				return err
			}
			if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) {
				continue
			}
			rel := strings.TrimPrefix(f.Name, prefix)
			if !safePackPath(rel) {
				if strings.HasPrefix(rel, "saves/") {
					plan.Warnings = append(plan.Warnings, "Миры из архива не заменяют ваши сохранения")
					continue
				}
				return fmt.Errorf("неподдерживаемый файл в overrides: %s", rel)
			}
			if err = add(packEntry{Path: rel, Archive: f.Name, File: Version{Size: int64(f.UncompressedSize64)}}, true); err != nil {
				return err
			}
		}
	}
	for _, entry := range entries {
		plan.entries = append(plan.entries, entry)
		plan.Bytes += entry.File.Size
	}
	// Windows merges case variants of directory names. Reject ambiguous file /
	// directory layouts before downloading, rather than racing worker Mkdir calls.
	directories := map[string]string{}
	for _, entry := range plan.entries {
		parts := strings.Split(entry.Path, "/")
		for i := 1; i < len(parts); i++ {
			dir := strings.Join(parts[:i], "/")
			key := strings.ToLower(dir)
			if _, file := entries[key]; file {
				return fmt.Errorf("файл конфликтует с папкой сборки: %s", dir)
			}
			if previous, exists := directories[key]; exists && previous != dir {
				return fmt.Errorf("разный регистр одной папки в сборке: %s и %s", previous, dir)
			}
			directories[key] = dir
		}
	}
	plan.Files = len(plan.entries)
	if plan.Files > 10000 || plan.Bytes > 8<<30 {
		return errors.New("сборка превышает лимиты установки")
	}
	sort.Slice(plan.entries, func(i, j int) bool { return plan.entries[i].Path < plan.entries[j].Path })
	return nil
}

func (c *Client) cursePackFiles(ctx context.Context, refs []cursePackReference) ([]Version, error) {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	results := make([]Version, len(refs))
	queue := make(chan int, len(refs))
	for i, ref := range refs {
		if ref.Required {
			queue <- i
		}
	}
	close(queue)
	errorsOut := make(chan error, 1)
	var workers sync.WaitGroup
	for worker := 0; worker < min(4, len(refs)); worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for i := range queue {
				if run.Err() != nil {
					return
				}
				ref := refs[i]
				v, err := c.Version(run, "curseforge", strconv.Itoa(ref.Project), strconv.Itoa(ref.File))
				if err == nil && (v.Project != strconv.Itoa(ref.Project) || v.ID != strconv.Itoa(ref.File)) {
					err = errors.New("некорректный файл CurseForge")
				}
				if err == nil && v.URL == "" {
					var found bool
					v, found, err = c.modrinthEquivalent(run, v)
					if err == nil && !found {
						err = fmt.Errorf("файл %s недоступен через CurseForge API; точной копии на Modrinth нет", v.Name)
					}
				}
				if err == nil {
					v.Filename, err = c.cursePackPath(run, v)
					v.Kind = "packfile"
				}
				if err != nil {
					select {
					case errorsOut <- fmt.Errorf("CurseForge %d/%d: %w", ref.Project, ref.File, err):
					default:
					}
					cancel()
					return
				}
				results[i] = v
			}
		}()
	}
	workers.Wait()
	select {
	case err := <-errorsOut:
		return nil, err
	default:
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	files := []Version{}
	for i, ref := range refs {
		if ref.Required {
			files = append(files, results[i])
		}
	}
	return files, nil
}

// An author's public Modrinth upload is an independent, permitted source. Never
// construct a disabled CurseForge CDN URL or substitute a different mod version.
func (c *Client) modrinthEquivalent(ctx context.Context, v Version) (Version, bool, error) {
	if v.Algorithm != "sha1" {
		return v, false, nil
	}
	raw, err := hex.DecodeString(v.Hash)
	if err != nil || len(raw) != 20 {
		return v, false, nil
	}
	var row mrVersion
	if err := c.get(ctx, "modrinth", "/version_file/"+strings.ToLower(v.Hash)+"?algorithm=sha1", &row); err != nil {
		// A missing mirror is expected for CurseForge-only files. Cancellation and
		// temporary outages must remain distinguishable from a catalog miss.
		var temporary *transferError
		if ctx.Err() != nil || errors.As(err, &temporary) {
			return v, false, err
		}
		return v, false, nil
	}
	for _, file := range row.Files {
		if strings.EqualFold(file.Hashes["sha1"], v.Hash) && allowedPackDownload(file.URL) {
			v.URL, v.URLs = file.URL, []string{file.URL}
			return v, true, nil
		}
	}
	return v, false, nil
}

func (c *Client) cursePackPath(ctx context.Context, v Version) (string, error) {
	if !safePackRelative(v.Filename) || strings.Contains(v.Filename, "/") {
		return "", errors.New("некорректное имя файла CurseForge")
	}
	switch strings.ToLower(path.Ext(v.Filename)) {
	case ".jar":
		return "mods/" + v.Filename, nil
	case ".zip":
		var project struct{ Data cfProject }
		if err := c.get(ctx, "curseforge", "/mods/"+v.Project, &project); err != nil {
			return "", err
		}
		if strconv.Itoa(project.Data.ID) != v.Project {
			return "", errors.New("файл CurseForge принадлежит другому проекту")
		}
		switch project.Data.ClassID {
		case 12:
			return "resourcepacks/" + v.Filename, nil
		case 6552:
			return "shaderpacks/" + v.Filename, nil
		}
	}
	return "", fmt.Errorf("неподдерживаемый тип файла CurseForge: %s", v.Filename)
}

// Reuse only byte-for-byte verified files. A size estimate from a manifest is
// deliberately not used as an identity check (some real packs are off by a byte).
func copyVerifiedPackFile(ctx context.Context, file Version, root, dest string) (bool, int64, error) {
	if file.Hash == "" || !safePackPath(file.Filename) {
		return false, 0, nil
	}
	source := root
	for _, part := range strings.Split(file.Filename, "/") {
		source = filepath.Join(source, part)
		info, err := os.Lstat(source)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			return false, 0, nil
		}
	}
	info, err := os.Lstat(source)
	if err != nil || !info.Mode().IsRegular() || info.Size() > 1<<30 {
		return false, 0, nil
	}
	want, err := packFileDigest(file.Algorithm)
	if err != nil {
		return false, 0, nil
	}
	input, err := os.Open(source)
	if err != nil {
		return false, 0, nil
	}
	defer input.Close()
	output, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return false, 0, err
	}
	n, err := io.Copy(io.MultiWriter(output, want), io.LimitReader(&packContextReader{ctx, input}, (1<<30)+1))
	if err == nil {
		err = output.Sync()
	}
	closeErr := output.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || n > 1<<30 || !strings.EqualFold(hex.EncodeToString(want.Sum(nil)), file.Hash) {
		_ = os.Remove(dest)
		if ctx.Err() != nil {
			return false, 0, ctx.Err()
		}
		return false, 0, err
	}
	return true, n, nil
}

type packContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *packContextReader) Read(b []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(b)
}

func (m *Manager) PackPlan(id string) (PackPlan, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p, ok := m.packPlans[id]
	if !ok || time.Since(p.Created) > 20*time.Minute {
		return p, errors.New("план сборки устарел; выберите её версию заново")
	}
	return p, nil
}

func (m *Manager) DiscardPack(id string) {
	m.mu.Lock()
	p, ok := m.packPlans[id]
	if ok {
		delete(m.packPlans, id)
	}
	m.mu.Unlock()
	if ok {
		removePackStage(p.folder)
	}
}
