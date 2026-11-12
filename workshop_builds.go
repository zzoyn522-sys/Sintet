package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"sintet/internal/accountfiles"
	"sintet/internal/launcher"
)

type WorkshopBuild struct {
	ID                string `json:"id"`
	ImportedFrom      string `json:"importSource,omitempty"`
	Name              string `json:"name"`
	Icon              string `json:"icon,omitempty"`
	Game              string `json:"game"`
	Loader            string `json:"loader"`
	LoaderVersion     string `json:"loaderVersion"`
	UseGlobalSettings bool   `json:"useGlobalSettings"`
	MinMemoryMB       int    `json:"minMemoryMB,omitempty"`
	MemoryMB          int    `json:"memoryMB,omitempty"`
	JavaPath          string `json:"javaPath,omitempty"`
	JVMArgs           string `json:"jvmArgs,omitempty"`
	Fullscreen        bool   `json:"fullscreen,omitempty"`
	Width             int    `json:"width,omitempty"`
	Height            int    `json:"height,omitempty"`
	Installed         bool   `json:"installed"`
	Installing        bool   `json:"installing"`
}

type WorkshopBuildSettings struct {
	Name              string `json:"name"`
	Icon              string `json:"icon"`
	UseGlobalSettings bool   `json:"useGlobalSettings"`
	MinMemoryMB       int    `json:"minMemoryMB"`
	MemoryMB          int    `json:"memoryMB"`
	JavaPath          string `json:"javaPath"`
	JVMArgs           string `json:"jvmArgs"`
	Fullscreen        bool   `json:"fullscreen"`
	Width             int    `json:"width"`
	Height            int    `json:"height"`
}

type WorkshopJavaRuntime struct {
	Path    string `json:"path"`
	Major   int    `json:"major"`
	Version string `json:"version"`
	Label   string `json:"label"`
}

type workshopBuildStore struct {
	Builds []WorkshopBuild `json:"builds"`
}

func (a *App) workshopBuildsPath() string {
	return filepath.Join(filepath.Dir(a.settingsPath), "Builds", "builds.json")
}

func (a *App) readWorkshopBuilds() (workshopBuildStore, error) {
	var store workshopBuildStore
	data, err := os.ReadFile(a.workshopBuildsPath())
	if os.IsNotExist(err) {
		return store, nil
	}
	if err != nil {
		return store, err
	}
	if len(data) > 1024*1024 {
		return store, errors.New("список сборок слишком большой")
	}
	if err = json.Unmarshal(data, &store); err != nil {
		return store, errors.New("не удалось прочитать список сборок")
	}
	if len(store.Builds) > 500 {
		return store, errors.New("в списке слишком много сборок")
	}
	return store, nil
}

func (a *App) saveWorkshopBuilds(store workshopBuildStore) error {
	data, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return err
	}
	return accountfiles.Write(a.workshopBuildsPath(), data)
}

func (a *App) WorkshopBuilds() ([]WorkshopBuild, error) {
	canWrite := a.contentMu.TryLock()
	if canWrite {
		defer a.contentMu.Unlock()
	}
	a.mu.RLock()
	service := a.service
	state := service.State()
	a.mu.RUnlock()
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return nil, err
	}
	installed := make(map[string]WorkshopBuild)
	for _, profile := range service.Installations() {
		name := fmt.Sprintf("%s · %s", profile.Base, profile.Loader)
		if profile.Loader == "vanilla" {
			name = profile.Base + " · Vanilla"
		}
		installed[profile.ID] = WorkshopBuild{ID: profile.ID, Name: name, Game: profile.Base, Loader: profile.Loader, LoaderVersion: profile.LoaderVersion, Installed: true}
	}
	result := make([]WorkshopBuild, 0, len(store.Builds))
	seen := make(map[string]bool)
	dirty := false
	for _, build := range store.Builds {
		if build.ID == "" || seen[build.ID] {
			continue
		}
		seen[build.ID] = true
		if build.Width == 0 {
			build.Width = 1280
		}
		if build.Height == 0 {
			build.Height = 720
		}
		if build.MemoryMB == 0 {
			build.MemoryMB = 4096
			build.UseGlobalSettings = true
		}
		if build.MinMemoryMB == 0 {
			build.MinMemoryMB = 512
		}
		if current, ok := installed[build.ID]; ok {
			build.Installed = true
			if !state.Busy && (build.Game != current.Game || build.Loader != current.Loader || build.LoaderVersion != current.LoaderVersion) {
				build.Game, build.Loader, build.LoaderVersion = current.Game, current.Loader, current.LoaderVersion
				if canWrite {
					_ = a.writeWorkshopBuildManifest(service.Root, build)
				}
				dirty = true
			}
			if build.Name == "" {
				build.Name = current.Name
				dirty = true
			}
			delete(installed, build.ID)
		}
		build.Installing = state.Busy && state.Profile == build.ID
		result = append(result, build)
	}
	if dirty && canWrite {
		_ = a.saveWorkshopBuilds(store)
	}
	return result, nil
}

func normalizeWorkshopBuildName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if !utf8.ValidString(name) || utf8.RuneCountInString(name) < 2 || utf8.RuneCountInString(name) > 40 || strings.ContainsAny(name, "\r\n\x00") {
		return "", errors.New("название сборки должно содержать от 2 до 40 символов")
	}
	return name, nil
}

func buildSlug(name string) string {
	value := strings.ToLower(name)
	value = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, "-")
	value = strings.Trim(value, "-")
	if len(value) > 24 {
		value = value[:24]
	}
	if value == "" {
		value = "instance"
	}
	return value
}

func (a *App) CreateWorkshopBuild(name, game, loader, loaderVersion string) (WorkshopBuild, error) {
	name, err := normalizeWorkshopBuildName(name)
	if err != nil {
		return WorkshopBuild{}, err
	}
	if !regexp.MustCompile(`^(?:1\.[0-9]+(?:\.[0-9]+)?|[0-9]+\.[0-9]+(?:-snapshot-[0-9]+)?)$`).MatchString(game) {
		return WorkshopBuild{}, errors.New("выбери корректную версию Minecraft")
	}
	if loader != "vanilla" && loader != "fabric" && loader != "forge" && loader != "neoforge" {
		return WorkshopBuild{}, errors.New("выбери поддерживаемый загрузчик")
	}
	if loader == "vanilla" {
		loaderVersion = ""
	} else if loaderVersion == "" || len(loaderVersion) > 64 || !regexp.MustCompile(`^[A-Za-z0-9_.+\-]+$`).MatchString(loaderVersion) {
		return WorkshopBuild{}, errors.New("выбери версию загрузчика")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	if state := a.service.State(); state.Busy || state.Running || a.workshop.State().Busy {
		a.mu.RUnlock()
		return WorkshopBuild{}, errors.New("дождись завершения текущей операции")
	}
	if a.remotePackBusy.Load() {
		a.mu.RUnlock()
		return WorkshopBuild{}, errors.New("дождитесь синхронизации сборки")
	}
	service := a.service
	java := a.settings.JavaPath
	root := a.settings.GameDirectory
	a.mu.RUnlock()
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return WorkshopBuild{}, err
	}
	var random [5]byte
	if _, err = rand.Read(random[:]); err != nil {
		return WorkshopBuild{}, err
	}
	id := "build-" + buildSlug(name) + "-" + hex.EncodeToString(random[:])
	build := WorkshopBuild{ID: id, Name: name, Icon: "grass", Game: game, Loader: loader, LoaderVersion: loaderVersion, UseGlobalSettings: true, MinMemoryMB: 512, MemoryMB: 4096, Width: 1280, Height: 720}
	store.Builds = append(store.Builds, build)
	if err = a.saveWorkshopBuilds(store); err != nil {
		return WorkshopBuild{}, err
	}
	instance := filepath.Join(root, "instances", id)
	for _, folder := range []string{"mods", "config", "resourcepacks", "saves"} {
		if err = os.MkdirAll(filepath.Join(instance, folder), 0755); err != nil {
			return WorkshopBuild{}, err
		}
	}
	metadata := struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Icon          string `json:"icon"`
		Game          string `json:"gameVersion"`
		Loader        string `json:"loader"`
		LoaderVersion string `json:"loaderVersion,omitempty"`
	}{id, name, build.Icon, game, loader, loaderVersion}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return WorkshopBuild{}, err
	}
	if err = accountfiles.Write(filepath.Join(instance, "instance.json"), data); err != nil {
		return WorkshopBuild{}, err
	}
	if err = service.StartInstallAs(id, game, loader, loaderVersion, java); err != nil {
		return WorkshopBuild{}, err
	}
	build.Installing = true
	return build, nil
}

func (a *App) RenameWorkshopBuild(id, name string) error {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if !strings.HasPrefix(id, "build-") {
		return errors.New("переименовывать можно только сборки мастерской")
	}
	name, err := normalizeWorkshopBuildName(name)
	if err != nil {
		return err
	}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return err
	}
	for i := range store.Builds {
		if store.Builds[i].ID == id {
			store.Builds[i].Name = name
			if err = a.saveWorkshopBuilds(store); err != nil {
				return err
			}
			a.mu.RLock()
			instanceRoot := a.settings.GameDirectory
			a.mu.RUnlock()
			return a.writeWorkshopBuildManifest(instanceRoot, store.Builds[i])
		}
	}
	return errors.New("сборка не найдена")
}

func (a *App) SaveWorkshopBuildSettings(id string, settings WorkshopBuildSettings) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return errors.New("неверная сборка")
	}
	name, err := normalizeWorkshopBuildName(settings.Name)
	if err != nil {
		return err
	}
	icons := map[string]bool{"grass": true, "sword": true, "crystal": true, "copper": true, "chest": true, "end": true}
	if !icons[settings.Icon] && !a.hasCustomBuildIcon(settings.Icon) {
		return errors.New("выбери доступную иконку сборки")
	}
	if settings.Width < 640 || settings.Width > 16384 || settings.Height < 480 || settings.Height > 16384 {
		return errors.New("разрешение должно быть от 640×480 до 16384×16384")
	}
	if settings.MemoryMB < 2048 || settings.MemoryMB > 131072 || settings.MemoryMB%512 != 0 {
		return errors.New("максимальная память должна быть от 2 ГБ с шагом 512 МБ")
	}
	if settings.MinMemoryMB < 512 || settings.MinMemoryMB > settings.MemoryMB || settings.MinMemoryMB%512 != 0 {
		return errors.New("минимальная память должна быть от 512 МБ и не превышать максимум")
	}
	if len(settings.JVMArgs) > 512 || strings.ContainsAny(settings.JVMArgs, "\r\n\x00") {
		return errors.New("аргументы JVM слишком длинные или содержат недопустимые символы")
	}
	settings.JavaPath = strings.TrimSpace(settings.JavaPath)
	if settings.JavaPath != "" {
		if !filepath.IsAbs(settings.JavaPath) {
			return errors.New("укажи полный путь к javaw.exe")
		}
		if info, statErr := os.Stat(settings.JavaPath); statErr != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(settings.JavaPath), ".exe") {
			return errors.New("файл Java не найден")
		}
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	state := a.service.State()
	root := a.settings.GameDirectory
	a.mu.RUnlock()
	if state.Busy || state.Running || a.workshop.State().Busy {
		return errors.New("закрой игру перед изменением параметров сборки")
	}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return err
	}
	for i := range store.Builds {
		build := &store.Builds[i]
		if build.ID != id {
			continue
		}
		build.Name, build.Icon = name, settings.Icon
		build.UseGlobalSettings, build.MinMemoryMB, build.MemoryMB = settings.UseGlobalSettings, settings.MinMemoryMB, settings.MemoryMB
		build.JavaPath, build.JVMArgs = settings.JavaPath, strings.TrimSpace(settings.JVMArgs)
		build.Fullscreen, build.Width, build.Height = settings.Fullscreen, settings.Width, settings.Height
		if err = a.saveWorkshopBuilds(store); err != nil {
			return err
		}
		return a.writeWorkshopBuildManifest(root, *build)
	}
	return errors.New("сборка не найдена")
}

func (a *App) writeWorkshopBuildManifest(root string, build WorkshopBuild) error {
	return writeWorkshopBuildManifestAt(filepath.Join(root, "instances", build.ID), build)
}

func writeWorkshopBuildManifestAt(directory string, build WorkshopBuild) error {
	metadata := struct {
		ID            string `json:"id"`
		Name          string `json:"name"`
		Icon          string `json:"icon"`
		Game          string `json:"gameVersion"`
		Loader        string `json:"loader"`
		LoaderVersion string `json:"loaderVersion,omitempty"`
	}{build.ID, build.Name, build.Icon, build.Game, build.Loader, build.LoaderVersion}
	data, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return err
	}
	return accountfiles.Write(filepath.Join(directory, "instance.json"), data)
}

func (a *App) UpdateWorkshopBuildVersion(id, game, loader, loaderVersion string) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) || !regexp.MustCompile(`^(?:1\.[0-9]+(?:\.[0-9]+)?|[0-9]+\.[0-9]+(?:-snapshot-[0-9]+)?)$`).MatchString(game) {
		return errors.New("неверная сборка или версия Minecraft")
	}
	if loader != "vanilla" && loader != "fabric" && loader != "forge" && loader != "neoforge" {
		return errors.New("выбери поддерживаемый загрузчик")
	}
	if loader == "vanilla" {
		loaderVersion = ""
	} else if loaderVersion == "" || len(loaderVersion) > 64 || !regexp.MustCompile(`^[A-Za-z0-9_.+\-]+$`).MatchString(loaderVersion) {
		return errors.New("выбери версию загрузчика")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return err
	}
	for i := range store.Builds {
		build := &store.Builds[i]
		if build.ID != id {
			continue
		}
		a.mu.RLock()
		service, java := a.service, a.settings.JavaPath
		state := service.State()
		a.mu.RUnlock()
		if state.Busy || state.Running || a.workshop.State().Busy {
			return errors.New("дождись завершения текущей операции")
		}
		oldGame, oldLoader, oldVersion := build.Game, build.Loader, build.LoaderVersion
		build.Game, build.Loader, build.LoaderVersion = game, loader, loaderVersion
		if err = a.saveWorkshopBuilds(store); err != nil {
			return err
		}
		a.mu.RLock()
		root := a.settings.GameDirectory
		a.mu.RUnlock()
		if err = a.writeWorkshopBuildManifest(root, *build); err != nil {
			build.Game, build.Loader, build.LoaderVersion = oldGame, oldLoader, oldVersion
			_ = a.saveWorkshopBuilds(store)
			return err
		}
		if err = service.StartInstallAs(id, game, loader, loaderVersion, java); err != nil {
			build.Game, build.Loader, build.LoaderVersion = oldGame, oldLoader, oldVersion
			_ = a.saveWorkshopBuilds(store)
			_ = a.writeWorkshopBuildManifest(root, *build)
			return err
		}
		return nil
	}
	return errors.New("сборка не найдена")
}

func (a *App) AddWorkshopBuildMods(id string) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return errors.New("неверная сборка")
	}
	_, err := a.PickLocalWorkshopFiles(id, "mods")
	return err
}

func (a *App) AddWorkshopBuildFiles(id string, paths []string) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return errors.New("неверная сборка")
	}
	_, err := a.AddLocalWorkshopFiles(id, "mods", paths)
	return err
}

func copyWorkshopFile(source, destination string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(output, input)
	syncErr := output.Sync()
	closeErr := output.Close()
	if copyErr != nil {
		_ = os.Remove(destination)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(destination)
		return syncErr
	}
	if closeErr != nil {
		_ = os.Remove(destination)
		return closeErr
	}
	return nil
}

func (a *App) DuplicateWorkshopBuild(id string) (WorkshopBuild, error) {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return WorkshopBuild{}, errors.New("неверная сборка")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return WorkshopBuild{}, err
	}
	var original *WorkshopBuild
	for i := range store.Builds {
		if store.Builds[i].ID == id {
			original = &store.Builds[i]
			break
		}
	}
	if original == nil {
		return WorkshopBuild{}, errors.New("сборка не найдена")
	}
	a.mu.RLock()
	service, java, root := a.service, a.settings.JavaPath, a.settings.GameDirectory
	state := service.State()
	a.mu.RUnlock()
	if state.Busy || state.Running || a.workshop.State().Busy {
		return WorkshopBuild{}, errors.New("дождись завершения текущей операции")
	}
	nameRunes := []rune(original.Name)
	if len(nameRunes) > 31 {
		nameRunes = nameRunes[:31]
	}
	name, _ := normalizeWorkshopBuildName(string(nameRunes) + " (копия)")
	var random [5]byte
	if _, err = rand.Read(random[:]); err != nil {
		return WorkshopBuild{}, err
	}
	copyBuild := *original
	copyBuild.ID = "build-" + buildSlug(name) + "-" + hex.EncodeToString(random[:])
	copyBuild.Name, copyBuild.Installed, copyBuild.Installing = name, false, false
	copyBuild.ImportedFrom = ""
	sourceDir := filepath.Join(root, "instances", id)
	destDir := filepath.Join(root, "instances", copyBuild.ID)
	if err = copyWorkshopTree(sourceDir, destDir); err != nil {
		return WorkshopBuild{}, err
	}
	if err = a.writeWorkshopBuildManifest(root, copyBuild); err != nil {
		_ = os.RemoveAll(destDir)
		return WorkshopBuild{}, err
	}
	store.Builds = append(store.Builds, copyBuild)
	if err = a.saveWorkshopBuilds(store); err != nil {
		_ = os.RemoveAll(destDir)
		return WorkshopBuild{}, err
	}
	if err = service.StartInstallAs(copyBuild.ID, copyBuild.Game, copyBuild.Loader, copyBuild.LoaderVersion, java); err != nil {
		_ = os.RemoveAll(destDir)
		store.Builds = store.Builds[:len(store.Builds)-1]
		_ = a.saveWorkshopBuilds(store)
		return WorkshopBuild{}, err
	}
	copyBuild.Installing = true
	return copyBuild, nil
}

func copyWorkshopTree(source, destination string) error {
	return filepath.WalkDir(source, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return errors.New("сборка содержит символическую ссылку, копирование остановлено")
		}
		rel, err := filepath.Rel(source, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(destination, 0700)
		}
		if rel == "instance.json" {
			return nil
		}
		to := filepath.Join(destination, rel)
		if entry.IsDir() {
			return os.MkdirAll(to, 0700)
		}
		if !entry.Type().IsRegular() {
			return nil
		}
		return copyWorkshopFile(path, to)
	})
}

func (a *App) DeleteWorkshopBuild(id string) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return errors.New("неверная сборка")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	service := a.service
	state := service.State()
	root := service.Root
	a.mu.RUnlock()
	if state.Busy || state.Running || a.workshop.State().Busy {
		return errors.New("закрой игру и дождись завершения текущей операции")
	}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return err
	}
	index := -1
	for i := range store.Builds {
		if store.Builds[i].ID == id {
			index = i
			break
		}
	}
	if index < 0 {
		return errors.New("сборка не найдена")
	}
	for _, path := range []string{filepath.Join(root, "instances", id), filepath.Join(root, ".sintet", "installed", id+".json"), filepath.Join(root, ".sintet", "profiles", id+".json")} {
		if err = os.RemoveAll(path); err != nil {
			return err
		}
	}
	store.Builds = append(store.Builds[:index], store.Builds[index+1:]...)
	return a.saveWorkshopBuilds(store)
}

func (a *App) InstallWorkshopBuild(id string) error {
	if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(id) {
		return errors.New("неверная сборка")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return err
	}
	var build *WorkshopBuild
	for i := range store.Builds {
		if store.Builds[i].ID == id {
			build = &store.Builds[i]
			break
		}
	}
	if build == nil {
		return errors.New("сборка не найдена")
	}
	a.mu.RLock()
	if state := a.service.State(); state.Busy || state.Running || a.workshop.State().Busy {
		a.mu.RUnlock()
		return errors.New("дождись завершения текущей операции")
	}
	err = a.service.StartInstallAs(id, build.Game, build.Loader, build.LoaderVersion, a.settings.JavaPath)
	a.mu.RUnlock()
	return err
}

func (a *App) OpenWorkshopBuildFolder(id string) error {
	a.mu.RLock()
	target, err := a.workshopTarget(id)
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	return exec.Command("explorer.exe", target.Directory).Start()
}

func (a *App) buildNameMap() map[string]string {
	store, _ := a.readWorkshopBuilds()
	result := make(map[string]string, len(store.Builds))
	for _, build := range store.Builds {
		result[build.ID] = build.Name
	}
	return result
}

func (a *App) workshopBuildByID(id string) (WorkshopBuild, bool) {
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return WorkshopBuild{}, false
	}
	for _, build := range store.Builds {
		if build.ID == id {
			if build.MemoryMB == 0 {
				build.MemoryMB = 4096
				build.UseGlobalSettings = true
			}
			if build.MinMemoryMB == 0 {
				build.MinMemoryMB = 512
			}
			if build.Width == 0 {
				build.Width = 1280
			}
			if build.Height == 0 {
				build.Height = 720
			}
			return build, true
		}
	}
	return WorkshopBuild{}, false
}

func (a *App) WorkshopJavaRuntimes() []WorkshopJavaRuntime {
	a.mu.RLock()
	root := a.service.Root
	a.mu.RUnlock()
	paths := []string{}
	seen := map[string]bool{}
	add := func(path string) {
		if path == "" {
			return
		}
		key := strings.ToLower(filepath.Clean(path))
		if !seen[key] {
			seen[key] = true
			paths = append(paths, path)
		}
	}
	for _, major := range []int{8, 17, 21} {
		base := filepath.Join(root, "runtime", fmt.Sprintf("java-%d", major))
		_ = filepath.WalkDir(base, func(path string, entry os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if !entry.IsDir() && (strings.EqualFold(entry.Name(), "javaw.exe") || strings.EqualFold(entry.Name(), "java.exe")) {
				add(path)
				return filepath.SkipDir
			}
			return nil
		})
	}
	if path, err := exec.LookPath("javaw.exe"); err == nil {
		add(path)
	}
	if path, err := exec.LookPath("java.exe"); err == nil {
		add(path)
	}
	result := []WorkshopJavaRuntime{}
	for _, path := range paths {
		info, err := launcher.CheckJava(context.Background(), path)
		if err != nil {
			continue
		}
		label := fmt.Sprintf("Java %d · %s", info.Major, filepath.Base(filepath.Dir(filepath.Dir(path))))
		result = append(result, WorkshopJavaRuntime{Path: path, Major: info.Major, Version: info.Version, Label: label})
	}
	return result
}
