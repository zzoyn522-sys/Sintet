package workshop

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sintet/internal/accountfiles"
)

func (m *Manager) StartPack(ctx context.Context, id string, target Target, finish func(context.Context, PackPlan, func(string, float64)) error) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.Busy {
		return errors.New("дождитесь завершения установки")
	}
	p, ok := m.packPlans[id]
	if !ok || time.Since(p.Created) > 20*time.Minute {
		return errors.New("план сборки устарел; выберите версию ещё раз")
	}
	if target.Game != p.Game || target.Loader != p.Loader || !filepath.IsAbs(target.Directory) {
		return errors.New("версия Minecraft или загрузчик сборки не совпадают")
	}
	delete(m.packPlans, id)
	run, cancel := context.WithCancel(ctx)
	m.cancel = cancel
	m.state = InstallState{Busy: true, Stage: "Загрузка сборки", Total: p.Files}
	progress := func(stage string, percent float64) {
		m.mu.Lock()
		m.state.Stage = stage
		m.state.Percent = percent
		m.mu.Unlock()
	}
	go func() {
		defer cancel()
		defer removePackStage(p.folder)
		err := m.installPack(run, p, target.Directory)
		if err == nil && finish != nil {
			err = finish(run, p, progress)
		}
		if run.Err() != nil {
			err = errors.New("установка сборки отменена")
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		m.cancel = nil
		m.state.Busy = false
		if err != nil {
			m.state.Stage = "Сборка не установлена"
			m.state.Error = err.Error()
		} else {
			m.state.Stage = "Сборка установлена"
			m.state.Percent = 100
			m.state.Done = p.Files
		}
	}()
	return nil
}
func packDirectoryTreeSafe(root string) error {
	return filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("в папке сборки обнаружена ссылка; установка остановлена")
		}
		if !entry.IsDir() && !entry.Type().IsRegular() {
			return errors.New("в сборке обнаружен необычный тип файла")
		}
		return nil
	})
}
func (m *Manager) installPack(ctx context.Context, p PackPlan, target string) error {
	if err := os.MkdirAll(target, 0700); err != nil {
		return err
	}
	rootInfo, err := os.Lstat(target)
	if err != nil || !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return errors.New("папка назначения должна быть обычной папкой")
	}
	content := filepath.Join(p.folder, "content")
	if err = os.Mkdir(content, 0700); err != nil {
		return err
	}
	archive, err := zip.OpenReader(filepath.Join(p.folder, "source.zip"))
	if err != nil {
		return err
	}
	defer archive.Close()
	overrides := map[string]*zip.File{}
	for _, file := range archive.File {
		overrides[file.Name] = file
	}
	queue := make(chan int, len(p.entries))
	for i := range p.entries {
		queue <- i
	}
	close(queue)
	var wg sync.WaitGroup
	errorsOut := make(chan error, 1)
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	var done int
	var bytes int64
	var progressMu sync.Mutex
	report := func(name string, n int64) {
		progressMu.Lock()
		defer progressMu.Unlock()
		bytes += n
		if bytes > 8<<30 {
			select {
			case errorsOut <- errors.New("фактический размер сборки превышает лимит установки"):
			default:
			}
			cancel()
		}
		m.mu.Lock()
		m.state.Stage = "Сборка · " + name
		m.state.Percent = 0
		if p.Bytes > 0 {
			m.state.Percent = min(80, 80*float64(bytes)/float64(p.Bytes))
		}
		m.state.Done = done
		m.mu.Unlock()
	}
	for worker := 0; worker < min(6, max(1, len(p.entries))); worker++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range queue {
				if run.Err() != nil {
					return
				}
				entry := p.entries[i]
				if !safePackPath(entry.Path) {
					select {
					case errorsOut <- errors.New("небезопасный путь сборки"):
					default:
					}
					cancel()
					return
				}
				dest := filepath.Join(content, filepath.FromSlash(entry.Path))
				e := os.MkdirAll(filepath.Dir(dest), 0700)
				if e == nil && entry.Archive != "" {
					file := overrides[entry.Archive]
					if file == nil {
						e = errors.New("не найден файл overrides")
					} else {
						var source io.ReadCloser
						source, e = file.Open()
						if e == nil {
							var output *os.File
							output, e = os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
							if e == nil {
								var n int64
								n, e = io.Copy(output, io.LimitReader(&packContextReader{run, source}, int64(file.UncompressedSize64)+1))
								if e == nil && n != int64(file.UncompressedSize64) {
									e = errors.New("повреждён файл overrides")
								}
								if e == nil {
									e = output.Sync()
								}
								closeErr := output.Close()
								if e == nil {
									e = closeErr
								}
								report(entry.Path, n)
							}
							source.Close()
						}
					}
				} else if e == nil {
					var downloaded int64
					var reused bool
					reused, downloaded, e = copyVerifiedPackFile(run, entry.File, target, dest)
					if e == nil && !reused {
						_, e = m.download(run, entry.File, dest, func(n, total int64) {
							report(entry.Path, n-downloaded)
							downloaded = n
						})
					}
					if e == nil {
						if reused {
							report(entry.Path, downloaded)
						}
					}
				}
				if e != nil {
					select {
					case errorsOut <- fmt.Errorf("%s: %w", entry.Path, e):
					default:
					}
					cancel()
					return
				}
				progressMu.Lock()
				done++
				m.mu.Lock()
				m.state.Done = done
				m.mu.Unlock()
				progressMu.Unlock()
			}
		}()
	}
	wg.Wait()
	select {
	case e := <-errorsOut:
		return e
	default:
	}
	if e := ctx.Err(); e != nil {
		return e
	}
	m.mu.Lock()
	m.state.Stage = "Сохраняем резервную копию"
	m.state.Percent = 82
	m.mu.Unlock()
	backup, err := commitPackContent(ctx, p, target, content)
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.state.Stage = "Файлы сборки установлены"
	if backup != "" {
		m.state.Stage = "Файлы установлены · резервная копия сохранена"
	}
	m.state.Percent = 88
	m.mu.Unlock()
	return nil
}
func commitPackContent(ctx context.Context, p PackPlan, target, content string) (string, error) {
	if err := packDirectoryTreeSafe(content); err != nil {
		return "", err
	}
	top := map[string]bool{"mods": true, "config": true, ".sintet-mods.json": true, ".sintet-resourcepacks.json": true, ".sintet-pack.json": true}
	for _, entry := range p.entries {
		top[strings.SplitN(entry.Path, "/", 2)[0]] = true
	}
	if top["shaderpacks"] {
		top[zipPackRegistry("shaders")] = true
	}
	names := []string{}
	for name := range top {
		names = append(names, name)
	}
	sort.Strings(names)
	backupRoot := filepath.Join(target, ".sintet-pack-backups")
	if info, err := os.Lstat(backupRoot); err == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return "", errors.New("небезопасная папка резервных копий")
	}
	if err := os.MkdirAll(backupRoot, 0700); err != nil {
		return "", err
	}
	backup, err := os.MkdirTemp(backupRoot, "before-")
	if err != nil {
		return "", err
	}
	moved, created := []string{}, []string{}
	committed := false
	defer func() {
		if !committed {
			for i := len(created) - 1; i >= 0; i-- {
				name := created[i]
				_ = os.Rename(filepath.Join(target, name), filepath.Join(content, name))
			}
			for i := len(moved) - 1; i >= 0; i-- {
				name := moved[i]
				_ = os.Rename(filepath.Join(backup, name), filepath.Join(target, name))
			}
		}
		if len(moved) == 0 {
			_ = os.Remove(backup)
		}
	}()
	for _, name := range names {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		old := filepath.Join(target, name)
		if info, e := os.Lstat(old); e == nil {
			if info.Mode()&os.ModeSymlink != 0 {
				return "", errors.New("ссылки в назначении запрещены")
			}
			if info.IsDir() {
				if e = packDirectoryTreeSafe(old); e != nil {
					return "", e
				}
			}
			if e = os.Rename(old, filepath.Join(backup, name)); e != nil {
				return "", e
			}
			moved = append(moved, name)
		} else if !os.IsNotExist(e) {
			return "", e
		}
		fresh := filepath.Join(content, name)
		if _, e := os.Lstat(fresh); e == nil {
			if e = os.Rename(fresh, old); e != nil {
				return "", e
			}
			created = append(created, name)
		} else if !os.IsNotExist(e) {
			return "", e
		}
	}
	metadata := map[string]any{"provider": p.Project.Provider, "project": p.Project.ID, "name": p.Project.Name, "version": p.Version, "game": p.Game, "loader": p.Loader, "loaderVersion": p.LoaderVersion, "installedAt": time.Now().UTC(), "backup": backup}
	data, _ := json.MarshalIndent(metadata, "", "  ")
	if err = accountfiles.Write(filepath.Join(target, ".sintet-pack.json"), data); err != nil {
		return "", err
	}
	committed = true
	if len(moved) == 0 {
		return "", nil
	}
	return backup, nil
}
