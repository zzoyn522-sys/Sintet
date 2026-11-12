package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
	"sintet/internal/launcher"
)

type launcherImportProgress struct {
	Phase       string                  `json:"phase"`
	Current     string                  `json:"current"`
	Percent     int                     `json:"percent"`
	CopiedBytes int64                   `json:"copiedBytes"`
	TotalBytes  int64                   `json:"totalBytes"`
	Completed   int                     `json:"completed"`
	Total       int                     `json:"total"`
	Failures    []launcherImportFailure `json:"failures,omitempty"`
}

type launcherImportFailure struct {
	Name   string `json:"name"`
	Reason string `json:"reason"`
}

type launcherImporter struct {
	mu         sync.Mutex
	sources    map[string]launcherImportSource
	progress   launcherImportProgress
	marker     string
	onboarding bool
}

func newLauncherImporter(root string, firstLaunch bool) *launcherImporter {
	marker := filepath.Join(root, "import-offer.pending")
	if firstLaunch {
		_ = accountfiles.Write(marker, []byte("pending\n"))
	}
	_, err := os.Stat(marker)
	return &launcherImporter{marker: marker, onboarding: firstLaunch || err == nil, sources: map[string]launcherImportSource{}}
}

func (a *App) LauncherImportOffer() bool {
	m := a.importer
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.onboarding
}

func (a *App) FinishLauncherImportOffer() error {
	m := a.importer
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := os.Remove(m.marker); err != nil && !os.IsNotExist(err) {
		return err
	}
	m.onboarding = false
	return nil
}

func (a *App) DetectLauncherImports() []launcherImportSource {
	return a.findLauncherImports("")
}

func (a *App) ChooseLauncherImportFolder(title string) ([]launcherImportSource, error) {
	if title == "" || len(title) > 256 {
		title = "Выберите папку старого лаунчера"
	}
	folder, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: title})
	if err != nil || folder == "" {
		return nil, err
	}
	return a.findLauncherImports(folder), nil
}

func (a *App) findLauncherImports(folder string) []launcherImportSource {
	home, _ := os.UserHomeDir()
	roaming, _ := os.UserConfigDir()
	a.mu.RLock()
	target := a.settings.GameDirectory
	a.mu.RUnlock()
	found := detectLauncherImports(roaming, home, documentsFolder(), folder, target)
	store, _ := a.readWorkshopBuilds()
	imported := make(map[string]bool)
	for _, build := range store.Builds {
		imported[build.ImportedFrom] = true
	}
	m := a.importer
	m.mu.Lock()
	defer m.mu.Unlock()
	visible := make([]launcherImportSource, 0, len(found))
	for _, source := range found {
		if !imported[source.ID] {
			m.sources[source.ID] = source
			visible = append(visible, source)
		}
	}
	return visible
}

func (a *App) LauncherImportProgress() launcherImportProgress {
	m := a.importer
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.progress
	state.Failures = append([]launcherImportFailure(nil), state.Failures...)
	return state
}

func (m *launcherImporter) update(update func(*launcherImportProgress)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	update(&m.progress)
}

func importInProgress(phase string) bool {
	return phase == "scanning" || phase == "copying" || phase == "installing"
}

func (a *App) StartLauncherImport(ids []string) error {
	if len(ids) == 0 || len(ids) > 250 {
		return errors.New("Выбери хотя бы одну сборку")
	}
	a.mu.RLock()
	service := a.service
	java := a.settings.JavaPath
	a.mu.RUnlock()
	if !a.contentMu.TryLock() {
		return errors.New("Дождись завершения текущей операции")
	}
	started := false
	defer func() {
		if !started {
			a.contentMu.Unlock()
		}
	}()
	a.mu.RLock()
	current := a.service
	a.mu.RUnlock()
	state := service.State()
	if service != current || state.Busy || state.Running || a.workshopBusy() || a.remotePackBusy.Load() {
		return errors.New("Закрой Minecraft и дождись завершения текущей операции")
	}

	m := a.importer
	m.mu.Lock()
	defer m.mu.Unlock()
	if importInProgress(m.progress.Phase) {
		return errors.New("Перенос уже идёт")
	}
	var sources []launcherImportSource
	seen := map[string]bool{}
	for _, id := range ids {
		source, ok := m.sources[id]
		if !ok {
			return errors.New("Сборка больше недоступна. Повтори поиск")
		}
		if !seen[id] {
			sources = append(sources, source)
			seen[id] = true
		}
	}
	m.progress = launcherImportProgress{Phase: "scanning", Total: len(sources)}
	started = true
	go a.runLauncherImport(service, java, sources)
	return nil
}

func (a *App) runLauncherImport(service *launcher.Service, java string, sources []launcherImportSource) {
	defer a.contentMu.Unlock()
	m := a.importer
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		m.update(func(state *launcherImportProgress) {
			state.Phase = "failed"
			state.Failures = []launcherImportFailure{{Reason: "Не удалось открыть список сборок"}}
		})
		return
	}
	if len(store.Builds)+len(sources) > 500 {
		m.update(func(state *launcherImportProgress) {
			state.Phase = "failed"
			state.Failures = []launcherImportFailure{{Reason: "В списке слишком много сборок"}}
		})
		return
	}
	imported := map[string]bool{}
	for _, build := range store.Builds {
		imported[build.ImportedFrom] = true
	}
	type planned struct {
		source launcherImportSource
		files  []launcherImportFile
		bytes  int64
	}
	var plans []planned
	var totalBytes int64
	fail := func(name, reason string) {
		m.update(func(state *launcherImportProgress) {
			state.Failures = append(state.Failures, launcherImportFailure{Name: name, Reason: reason})
		})
	}
	for _, source := range sources {
		if ctx.Err() != nil {
			return
		}
		if imported[source.ID] {
			continue
		}
		m.update(func(state *launcherImportProgress) { state.Current = source.Name })
		files, bytes, err := planLauncherImport(source.Folder)
		if err != nil {
			fail(source.Name, "Не удалось прочитать файлы сборки")
			continue
		}
		totalBytes += bytes
		plans = append(plans, planned{source, files, bytes})
	}

	m.update(func(state *launcherImportProgress) { state.Phase, state.TotalBytes = "copying", totalBytes })
	var copied, processed int64
	var builds []WorkshopBuild
	progress := func(n int64) {
		copied += n
		processed += n
		m.update(func(state *launcherImportProgress) {
			state.CopiedBytes = copied
			if totalBytes > 0 {
				state.Percent = int(70 * min(processed, totalBytes) / totalBytes)
			}
		})
	}
	for _, plan := range plans {
		if ctx.Err() != nil {
			return
		}
		m.update(func(state *launcherImportProgress) { state.Current = plan.source.Name })
		initial := copied
		var build WorkshopBuild
		err := service.CheckDiskSpace(plan.bytes)
		if err == nil {
			build, err = a.commitLauncherImport(ctx, service.Root, plan.source, plan.files, &store, progress)
		}
		processed += plan.bytes - (copied - initial)
		if err != nil {
			reason := "Не удалось скопировать сборку"
			if launcher.IsDiskFull(err) {
				reason = "Недостаточно места на диске"
			}
			fail(plan.source.Name, reason)
			copied = initial
		} else {
			builds = append(builds, build)
		}
		progress(0)
	}
	for index, build := range builds {
		if ctx.Err() != nil {
			return
		}
		m.update(func(state *launcherImportProgress) {
			state.Phase, state.Current = "installing", build.Name
			state.Percent = 70 + 30*index/len(builds)
		})
		if !service.Installed(build.ID) && !installImportedBuild(ctx, service, java, build, func(percent int) {
			m.update(func(state *launcherImportProgress) { state.Percent = 70 + (30*index+30*percent/100)/len(builds) })
		}) {
			fail(build.Name, "Файлы перенесены. Установи Minecraft для этой сборки в мастерской")
		}
	}
	m.update(func(state *launcherImportProgress) {
		state.Phase, state.Percent, state.Completed = "done", 100, len(builds)
		if len(state.Failures) != 0 {
			state.Phase = "partial"
			if len(builds) == 0 {
				state.Phase = "failed"
			}
		}
	})
}

func installImportedBuild(ctx context.Context, service *launcher.Service, java string, build WorkshopBuild, progress func(int)) bool {
	if err := service.StartInstallAs(build.ID, build.Game, build.Loader, build.LoaderVersion, java); err != nil {
		return false
	}
	done := make(chan struct{})
	go func() { service.WaitInstall(); close(done) }()
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			service.Cancel()
			<-done
			return false
		case <-done:
			return service.Installed(build.ID)
		case <-tick.C:
			progress(int(service.State().Percent))
		}
	}
}

type launcherImportFile struct {
	relative string
	size     int64
	modified time.Time
}

var importFolders = []string{"mods", "config", "defaultconfigs", "saves", "resourcepacks", "shaderpacks", "screenshots", "kubejs", "scripts", "datapacks", "global_packs", "schematics"}
var importRootFiles = []string{"options.txt", "optionsof.txt", "optionsshaders.txt", "servers.dat", "hotbar.nbt"}

func planLauncherImport(source string) ([]launcherImportFile, int64, error) {
	if !safeImportDir(source) {
		return nil, 0, os.ErrNotExist
	}
	var files []launcherImportFile
	var bytes int64
	add := func(path string, info os.FileInfo) error {
		if len(files) >= 150000 || info.Size() > (1<<44)-bytes {
			return errors.New("слишком много файлов сборки")
		}
		relative, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		files = append(files, launcherImportFile{relative, info.Size(), info.ModTime()})
		bytes += info.Size()
		return nil
	}
	for _, name := range importFolders {
		folder := filepath.Join(source, name)
		if !safeImportDir(folder) {
			continue
		}
		err := filepath.WalkDir(folder, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			attrs, err := importAttributes(path)
			if err != nil {
				return err
			}
			if attrs&importReparsePoint != 0 || entry.Type()&os.ModeSymlink != 0 {
				if entry.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			if !entry.Type().IsRegular() || strings.EqualFold(entry.Name(), "session.lock") {
				return nil
			}
			info, err := entry.Info()
			if err != nil {
				return err
			}
			return add(path, info)
		})
		if err != nil {
			return nil, 0, err
		}
	}
	for _, name := range importRootFiles {
		path := filepath.Join(source, name)
		if regularImportFile(path) {
			info, err := os.Stat(path)
			if err != nil {
				return nil, 0, err
			}
			if err = add(path, info); err != nil {
				return nil, 0, err
			}
		}
	}
	return files, bytes, nil
}

func (a *App) commitLauncherImport(ctx context.Context, root string, source launcherImportSource, files []launcherImportFile, store *workshopBuildStore, progress func(int64)) (WorkshopBuild, error) {
	build := WorkshopBuild{ID: "build-" + buildSlug(source.Name) + "-" + source.ID[:10], Name: source.Name, Icon: "grass", Game: source.Game, Loader: source.Loader, LoaderVersion: source.LoaderVersion, UseGlobalSettings: true, MinMemoryMB: 512, MemoryMB: 4096, Width: 1280, Height: 720, ImportedFrom: source.ID}
	parent := filepath.Join(root, "instances")
	final := filepath.Join(parent, build.ID)
	if err := os.MkdirAll(parent, 0700); err != nil {
		return build, err
	}
	if _, err := os.Lstat(final); err == nil {
		// The previous run may have closed between publishing files and registering the build.
		var staged WorkshopBuild
		if !readImportJSON(filepath.Join(final, ".sintet-import.json"), &staged) ||
			staged.ID != build.ID || staged.ImportedFrom != source.ID {
			return build, os.ErrExist
		}
		build = staged
	} else if !os.IsNotExist(err) {
		return build, err
	} else {
		staging, err := os.MkdirTemp(parent, ".sintet-import-")
		if err != nil {
			return build, err
		}
		defer removeImportStaging(parent, staging)
		if err = copyLauncherImport(ctx, source.Folder, staging, files, progress); err != nil {
			return build, err
		}
		if regularImportFile(source.icon) {
			if data, err := readLimitedFile(source.icon, iconFileLimit); err == nil {
				if name, err := saveCustomIcon(filepath.Join(filepath.Dir(a.settingsPath), "BuildIcons"), data); err == nil {
					build.Icon = "custom:" + strings.TrimSuffix(name, ".png")
				}
			}
		}
		if err := writeWorkshopBuildManifestAt(staging, build); err != nil {
			return build, err
		}
		marker, err := json.Marshal(build)
		if err != nil {
			return build, err
		}
		if err = accountfiles.Write(filepath.Join(staging, ".sintet-import.json"), marker); err != nil {
			return build, err
		}
		if err = os.Rename(staging, final); err != nil {
			return build, err
		}
	}
	store.Builds = append(store.Builds, build)
	if err := a.saveWorkshopBuilds(*store); err != nil {
		store.Builds = store.Builds[:len(store.Builds)-1]
		return build, err
	}
	_ = os.Remove(filepath.Join(final, ".sintet-import.json"))
	return build, nil
}

func removeImportStaging(parent, staging string) {
	parent, parentErr := filepath.EvalSymlinks(parent)
	staging, stageErr := filepath.EvalSymlinks(staging)
	if parentErr == nil && stageErr == nil && strings.EqualFold(filepath.Dir(staging), parent) &&
		strings.HasPrefix(filepath.Base(staging), ".sintet-import-") {
		_ = os.RemoveAll(staging)
	}
}

type importCopyWriter struct {
	writer   io.Writer
	ctx      context.Context
	progress func(int64)
}

func (w importCopyWriter) Write(data []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.writer.Write(data)
	w.progress(int64(n))
	return n, err
}

func copyLauncherImport(ctx context.Context, source, target string, files []launcherImportFile, progress func(int64)) error {
	root, err := os.OpenRoot(source)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, name := range []string{"mods", "config", "resourcepacks", "shaderpacks", "saves"} {
		if err = os.Mkdir(filepath.Join(target, name), 0700); err != nil {
			return err
		}
	}
	for _, file := range files {
		if err = ctx.Err(); err != nil {
			return err
		}
		if !regularImportFile(filepath.Join(source, file.relative)) {
			return os.ErrNotExist
		}
		if err = copyLauncherImportFile(ctx, root, target, file, progress); err != nil {
			return err
		}
	}
	return nil
}

func copyLauncherImportFile(ctx context.Context, root *os.Root, target string, file launcherImportFile, progress func(int64)) error {
	input, err := root.Open(file.relative)
	if err != nil {
		return err
	}
	defer input.Close()
	destination := filepath.Join(target, file.relative)
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return err
	}
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	n, copyErr := io.Copy(importCopyWriter{output, ctx, progress}, io.LimitReader(input, file.size+1))
	closeErr := output.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if n != file.size || info.Size() != file.size || !info.ModTime().Equal(file.modified) {
		return errors.New("исходный файл изменился во время копирования")
	}
	return nil
}

func (a *App) RestartAfterLauncherImport() error {
	state := a.LauncherImportProgress()
	if state.Phase != "done" && state.Phase != "partial" {
		return errors.New("Дождись завершения переноса")
	}
	if a.State().Running || a.State().Busy {
		return errors.New("Закрой Minecraft и дождись завершения текущей операции")
	}
	if err := a.FinishLauncherImportOffer(); err != nil {
		return err
	}
	if err := startImportRelaunch(); err != nil {
		return err
	}
	a.ExitLauncher()
	return nil
}
