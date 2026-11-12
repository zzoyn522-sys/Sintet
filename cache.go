package main

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type CacheGroup struct {
	Name  string `json:"name"`
	Bytes int64  `json:"bytes"`
	Files int    `json:"files"`
}
type CacheReport struct {
	Bytes   int64        `json:"bytes"`
	Files   int          `json:"files"`
	Freed   int64        `json:"freed"`
	Removed int          `json:"removed"`
	Skipped int          `json:"skipped"`
	Groups  []CacheGroup `json:"groups"`
}
type cacheFile struct {
	root, path string
	size       int64
	group      int
	modified   time.Time
}

func safeCacheFile(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return false
	}
	current := root
	if info, e := os.Lstat(current); e != nil || info.Mode()&os.ModeSymlink != 0 {
		return false
	}
	for _, part := range strings.Split(rel, string(os.PathSeparator)) {
		current = filepath.Join(current, part)
		info, e := os.Lstat(current)
		if e != nil || info.Mode()&os.ModeSymlink != 0 {
			return false
		}
	}
	return true
}
func cacheInventory(appRoot, gameRoot, currentLog string, now time.Time) ([]cacheFile, CacheReport, error) {
	var files []cacheFile
	report := CacheReport{Groups: []CacheGroup{{Name: "Временные файлы P2P"}, {Name: "Старые журналы"}, {Name: "Кэш скинов и аватарок"}}}
	roots := []string{appRoot, gameRoot}
	for i, root := range roots {
		resolved, err := filepath.EvalSymlinks(root)
		if os.IsNotExist(err) {
			resolved = root
		} else if err != nil {
			return nil, report, err
		}
		roots[i] = resolved
	}
	appRoot, gameRoot = roots[0], roots[1]
	specs := []struct {
		root, dir string
		group     int
		old       bool
		pngOnly   bool
	}{
		{appRoot, filepath.Join(appRoot, "P2P", "Temp"), 0, true, false},
		{appRoot, filepath.Join(appRoot, "Temp", "P2P"), 0, true, false},
		{gameRoot, filepath.Join(gameRoot, ".sintet", "p2p"), 0, true, false},
		{gameRoot, filepath.Join(gameRoot, ".sintet", "logs"), 1, true, false},
		{appRoot, filepath.Join(appRoot, "Avatars"), 2, false, true},
		{gameRoot, filepath.Join(gameRoot, "assets", "skins"), 2, false, false},
	}
	seen := map[string]bool{}
	for _, spec := range specs {
		if !safeCacheFile(spec.root, spec.dir) {
			continue
		}
		err := filepath.WalkDir(spec.dir, func(path string, entry fs.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if entry.IsDir() {
				if !safeCacheFile(spec.root, path) {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || !safeCacheFile(spec.root, path) || strings.EqualFold(path, currentLog) {
				return nil
			}
			if spec.pngOnly && !cachedAvatarName.MatchString(entry.Name()) {
				return nil
			}
			if spec.group == 1 && !strings.HasSuffix(strings.ToLower(path), ".log") && !strings.HasSuffix(strings.ToLower(path), ".log.gz") {
				return nil
			}
			info, e := entry.Info()
			if e != nil {
				return nil
			}
			age := now.Sub(info.ModTime())
			if spec.old && age < 7*24*time.Hour {
				return nil
			}
			key := strings.ToLower(path)
			if seen[key] {
				return nil
			}
			seen[key] = true
			files = append(files, cacheFile{spec.root, path, info.Size(), spec.group, info.ModTime()})
			report.Bytes += info.Size()
			report.Files++
			report.Groups[spec.group].Bytes += info.Size()
			report.Groups[spec.group].Files++
			return nil
		})
		if err != nil && !os.IsNotExist(err) {
			return nil, report, err
		}
	}
	// P2P diagnostics are a single append-only file, not session credentials.
	path := filepath.Join(appRoot, "p2p.log")
	if info, err := os.Lstat(path); err == nil && info.Mode().IsRegular() && now.Sub(info.ModTime()) >= 7*24*time.Hour && safeCacheFile(appRoot, path) {
		files = append(files, cacheFile{appRoot, path, info.Size(), 1, info.ModTime()})
		report.Bytes += info.Size()
		report.Files++
		report.Groups[1].Bytes += info.Size()
		report.Groups[1].Files++
	}
	return files, report, nil
}
func (a *App) CacheUsage() (CacheReport, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.mu.RLock()
	root := a.settings.GameDirectory
	log := a.service.State().Log
	a.mu.RUnlock()
	_, report, err := cacheInventory(a.profiles.root, root, log, time.Now())
	return report, err
}
func (a *App) ClearCache() (CacheReport, error) {
	a.cacheMu.Lock()
	defer a.cacheMu.Unlock()
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	root := a.settings.GameDirectory
	state := a.service.State()
	a.mu.RUnlock()
	if state.Busy || state.Running || a.workshopBusy() {
		return CacheReport{}, fmt.Errorf("закройте Minecraft и дождитесь завершения загрузок перед очисткой")
	}
	if a.social != nil {
		s := a.social.snapshot()
		if s.Room != nil || s.Busy {
			return CacheReport{}, fmt.Errorf("сначала выйдите из лобби")
		}
	}
	files, report, err := cacheInventory(a.profiles.root, root, state.Log, time.Now())
	if err != nil {
		return report, err
	}
	for _, file := range files {
		info, err := os.Lstat(file.path)
		if err != nil || !info.Mode().IsRegular() || info.Size() != file.size || !info.ModTime().Equal(file.modified) || !safeCacheFile(file.root, file.path) {
			report.Skipped++
			continue
		}
		if err = os.Remove(file.path); err != nil {
			report.Skipped++
			continue
		}
		report.Removed++
		report.Freed += file.size
	}
	return report, nil
}
