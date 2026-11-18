package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sintet/internal/identity"
	"sintet/internal/launcher"
	"sintet/internal/packsync"
)

func sharedBuildProfile(account, build string) string {
	sum := sha256.Sum256([]byte(account + ":" + build))
	return sharedProfilePrefix + hex.EncodeToString(sum[:16])
}

func shareableWorkshopProfile(id string) bool {
	return strings.HasPrefix(id, "build-") && !strings.HasPrefix(id, sharedProfilePrefix)
}

func sharedSameBase(build SharedBuild, local WorkshopBuild) bool {
	return local.Game == build.Game && local.Loader == build.Loader && local.LoaderVersion == build.LoaderVersion
}

func (m *sharedBuildManager) attachLocalProfile(s sharedBuildSession, build *SharedBuild) {
	id := m.localProfileID(s, *build)
	a := m.app
	a.mu.RLock()
	_, receiptErr := a.service.ProfileIdentity(id)
	a.mu.RUnlock()
	if profile, ok := a.workshopBuildByID(id); receiptErr == nil && ok && sharedSameBase(*build, profile) {
		profile.Installed = true
		build.LocalProfile = &profile
	}
}

func (m *sharedBuildManager) localProfileID(s sharedBuildSession, build SharedBuild) string {
	if build.OwnerID == s.account && shareableWorkshopProfile(build.OriginProfile) {
		if profile, ok := m.app.workshopBuildByID(build.OriginProfile); ok && sharedSameBase(build, profile) {
			return build.OriginProfile
		}
	}
	return sharedBuildProfile(s.account, build.ID)
}

func (a *App) SharedBuildLaunchLocal(id string) error {
	if !sharedUUID(id) {
		return errors.New("неверный идентификатор сборки")
	}
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	profile := sharedBuildProfile(s.account, id)
	if _, ok := a.workshopBuildByID(profile); !ok {
		return errors.New("сначала скачай сборку")
	}
	builds, err := a.SharedBuilds()
	if err != nil {
		return err
	}
	for _, build := range builds {
		if build.ID == id && build.Role != "invited" {
			return a.launchSharedInstance(profile, "Player", 0, false, nil, s.account, true)
		}
	}
	return errors.New("сборка недоступна")
}

func (m *sharedBuildManager) linkedBuild(s sharedBuildSession, profile string) string {
	data, err := os.ReadFile(filepath.Join(m.buildRoot(s), "builds.json"))
	var builds []SharedBuild
	if err == nil && json.Unmarshal(data, &builds) == nil {
		for _, build := range builds {
			if build.OriginProfile == profile && build.OwnerID == s.account && sharedUUID(build.ID) {
				return build.ID
			}
		}
	}
	return ""
}

func (a *App) SharedBuildApprove(id, key, hash string, version int) error {
	return a.approveSharedMods(id, key, hash, version, false)
}

func (a *App) SharedBuildApproveAll(id string, version int) error {
	return a.approveSharedMods(id, "", "", version, true)
}

func (a *App) approveSharedMods(id, key, hash string, version int, all bool) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	build, err := m.editingBuild(s, id, version)
	if err != nil {
		return err
	}
	if build.Role != "owner" || build.OwnerID != s.account {
		return errors.New("подтвердить мод может только лидер")
	}
	if all {
		var pending []sharedBuildMod
		for _, mod := range build.Mods {
			if !mod.Approved {
				pending = append(pending, mod)
			}
		}
		if len(pending) == 0 {
			return nil
		}
		if err := m.download(s, build, pending); err != nil {
			return err
		}
		if active, err := m.session(); err != nil || active.account != s.account {
			return errors.New("аккаунт изменился")
		}
		return s.request(m.ctx, "approve-all", id, map[string]any{"version": version}, nil)
	}
	for _, mod := range build.Mods {
		if mod.Key != key || mod.File.SHA256 != hash {
			continue
		}
		if err := m.download(s, build, []sharedBuildMod{mod}); err != nil {
			return err
		}
		return s.request(m.ctx, "approve", id, map[string]any{"version": version, "key": key, "hash": hash}, nil)
	}
	return errors.New("файл уже изменился")
}

func (a *App) SharedBuildRun(id string, version, revision int, launch bool) error {
	return a.runSharedBuild(id, version, revision, launch, "Player")
}

func (a *App) SharedBuildRollback(id string, version int) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if state := a.State(); state.Busy || state.Running || a.workshopBusy() {
		return errors.New("закрой Minecraft и дождись завершения установки")
	}
	return s.request(m.ctx, "rollback", id, map[string]any{"version": version}, nil)
}

// Revision zero prepares and publishes the draft; positive revisions are immutable.
func (a *App) runSharedBuild(id string, version, revision int, launch bool, name string) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	if revision < 0 {
		return errors.New("выбери сохранённую версию сборки")
	}
	if state := a.State(); state.Busy || state.Running || a.workshopBusy() {
		return errors.New("закрой Minecraft и дождись завершения установки")
	}
	var build SharedBuild
	if revision == 0 {
		build, err = m.editingBuild(s, id, version)
	} else {
		build, err = m.build(s, "revision", id, map[string]any{"revision": revision})
	}
	if err != nil {
		return err
	}
	if revision == 0 && (build.Role != "owner" || build.OwnerID != s.account) {
		return errors.New("запустить сборку с новыми модами может только лидер")
	}
	if revision == 0 {
		build, err = m.recoverWorkshopMods(s, build)
		if err != nil {
			return err
		}
		version = build.DraftVersion
	}
	mods := make([]sharedBuildMod, 0, len(build.Mods))
	for _, mod := range build.Mods {
		if !mod.Enabled {
			continue
		}
		if !mod.Approved {
			return errors.New("подтверди новые моды перед запуском")
		}
		mods = append(mods, mod)
	}
	if err := m.download(s, build, mods); err != nil {
		return err
	}
	defer m.setProgress(id, SharedBuildProgress{})
	active, err := m.session()
	if err != nil || active.account != s.account {
		return errors.New("аккаунт изменился")
	}
	if revision > 0 {
		// Check access again before writing a completed transfer to the instance.
		if _, err := m.build(s, "revision", id, map[string]any{"revision": revision}); err != nil {
			return err
		}
	}
	profile, err := m.install(s, build, mods, revision)
	if err != nil {
		return err
	}
	if revision == 0 {
		if err := s.request(m.ctx, "publish", id, map[string]any{"version": version}, nil); err != nil {
			return err
		}
	}
	if launch {
		return a.launchSharedInstance(profile, name, 0, false, nil, s.account, revision > 0)
	}
	return nil
}

func sharedManifest(build SharedBuild, mods []sharedBuildMod) (packsync.Manifest, error) {
	meta := packsync.Manifest{Name: build.Name, Game: build.Game, Loader: build.Loader, LoaderVersion: build.LoaderVersion}
	for _, mod := range mods {
		if !strings.HasPrefix(mod.File.Path, "mods/") {
			return packsync.Manifest{}, errors.New("общая сборка принимает только JAR")
		}
		meta.Files = append(meta.Files, mod.File)
	}
	return packsync.Seal(meta)
}

func (m *sharedBuildManager) download(s sharedBuildSession, build SharedBuild, mods []sharedBuildMod) error {
	return m.downloadSharedFiles(m.ctx, s, build, mods, nil)
}

func (m *sharedBuildManager) downloadSharedFiles(parent context.Context, s sharedBuildSession, build SharedBuild, mods []sharedBuildMod, progress func(string, float64)) error {
	if len(mods) == 0 {
		return nil
	}
	manifest, err := sharedManifest(build, mods)
	if err != nil {
		return err
	}
	cache := m.cache(s, build.ID)
	fetcher := newSharedBuildFetcher(s, build, mods, cache)
	defer fetcher.close()
	ctx, cancel := context.WithTimeout(parent, 9*time.Minute)
	defer cancel()
	m.setProgress(build.ID, SharedBuildProgress{Stage: "Сверка файлов сборки"})
	defer m.setProgress(build.ID, SharedBuildProgress{})
	// Catalogue URLs describe provenance. Only authenticated peers send
	// shared mod bytes between participants.
	for i := range manifest.Files {
		manifest.Files[i].Sources = nil
	}
	options := packsync.DownloadOptions{Resolvers: []packsync.Resolver{}}
	err = packsync.DownloadWithOptions(ctx, manifest, cache, fetcher, nil, func(file string, done, total int64) {
		m.setProgress(build.ID, SharedBuildProgress{Stage: "Скачивание модов", File: filepath.Base(file), Done: done, Total: total})
		if progress != nil && total > 0 {
			progress("Скачивание "+filepath.Base(file), 10*float64(done)/float64(total))
		}
	}, options)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return errors.New("передача заняла слишком много времени; проверь, что лаунчер автора или лидера открыт")
		}
		return err
	}
	for _, file := range manifest.Files {
		if err := validateSharedJar(filepath.Join(cache, file.SHA256)); err != nil {
			return fmt.Errorf("%s: %w", filepath.Base(file.Path), err)
		}
	}
	return nil
}

func (m *sharedBuildManager) install(s sharedBuildSession, build SharedBuild, mods []sharedBuildMod, revision int) (string, error) {
	a := m.app
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	service, javaPath := a.service, a.settings.JavaPath
	a.mu.RUnlock()
	state := service.State()
	if state.Busy || state.Running || a.workshopBusy() {
		return "", errors.New("закрой Minecraft и дождись завершения установки")
	}
	manifest, err := sharedManifest(build, mods)
	if err != nil {
		return "", err
	}
	id := m.localProfileID(s, build)
	if !service.Installed(id) {
		m.setProgress(build.ID, SharedBuildProgress{Stage: "Установка Minecraft"})
		if err := service.StartInstallAs(id, build.Game, build.Loader, build.LoaderVersion, javaPath); err != nil {
			return "", err
		}
		service.WaitInstall()
		if !service.Installed(id) {
			if problem := service.State().Error; problem != "" {
				return "", errors.New(problem)
			}
			return "", errors.New("установка Minecraft не завершилась")
		}
	}
	directory, err := service.ProfileDirectory(id, "profile")
	if err != nil {
		return "", err
	}
	m.setProgress(build.ID, SharedBuildProgress{Stage: "Подготовка сборки"})
	original := id == build.OriginProfile
	if err := replaceSharedMods(m.ctx, m.cache(s, build.ID), directory, manifest, original); err != nil {
		return "", err
	}
	if original {
		return id, nil
	}
	info := WorkshopBuild{ID: id, Name: build.Name, Game: build.Game, Loader: build.Loader, LoaderVersion: build.LoaderVersion, Icon: "grass", UseGlobalSettings: true, SharedRevision: revision}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return "", err
	}
	found := false
	for i := range store.Builds {
		if store.Builds[i].ID == id {
			// Local Java, resolution and icon settings belong to this instance.
			store.Builds[i].Name = info.Name
			store.Builds[i].SharedRevision = revision
			info, found = store.Builds[i], true
			break
		}
	}
	if !found {
		store.Builds = append(store.Builds, info)
	}
	if err := a.writeWorkshopBuildManifest(service.Root, info); err != nil {
		return "", err
	}
	return id, a.saveWorkshopBuilds(store)
}

func replaceSharedMods(ctx context.Context, cache, directory string, manifest packsync.Manifest, preserveLocal bool) error {
	// The live mods directory moves only after all new JARs have been verified.
	// Other folders, including saves and runtime configs, stay in place.
	stage, err := os.MkdirTemp(directory, ".sintet-shared-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	payload := filepath.Join(stage, "payload")
	if err := packsync.Populate(ctx, manifest, cache, payload); err != nil {
		return err
	}
	next := filepath.Join(payload, "mods")
	if err := os.MkdirAll(next, 0700); err != nil {
		return err
	}
	live := filepath.Join(directory, "mods")
	previous := filepath.Join(directory, ".sintet-shared-mods-previous")
	if info, err := os.Lstat(previous); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return errors.New("недопустимая папка резервной копии модов")
		}
		liveFiles, readErr := os.ReadDir(live)
		if os.IsNotExist(readErr) || readErr == nil && len(liveFiles) == 0 {
			if readErr == nil {
				if err := os.Remove(live); err != nil {
					return err
				}
			}
			if err := os.Rename(previous, live); err != nil {
				return err
			}
		} else {
			// Recover the last completed replacement after an interrupted run.
			if err := os.Rename(previous, filepath.Join(stage, "previous")); err != nil {
				return err
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	info, err := os.Lstat(live)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("недопустимая папка модов общей сборки")
	}
	if preserveLocal {
		if err := preserveSharedLocalFiles(live, next, manifest); err != nil {
			return err
		}
	}
	if err := os.Rename(live, previous); err != nil {
		return err
	}
	if err := os.Rename(next, live); err != nil {
		return errors.Join(err, os.Rename(previous, live))
	}
	return os.Rename(previous, filepath.Join(stage, "old-mods"))
}

func preserveSharedLocalFiles(live, next string, manifest packsync.Manifest) error {
	published := make(map[string]string, len(manifest.Files))
	for _, file := range manifest.Files {
		published[strings.ToLower(filepath.Base(file.Path))] = file.SHA256
	}
	entries, err := os.ReadDir(live)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		source := filepath.Join(live, entry.Name())
		dest := filepath.Join(next, entry.Name())
		if entry.IsDir() {
			if err := os.CopyFS(dest, os.DirFS(source)); err != nil {
				return err
			}
			continue
		}
		info, err := entry.Info()
		if err != nil || !info.Mode().IsRegular() {
			return errors.New("в папке модов есть файл неподдерживаемого типа")
		}
		if strings.EqualFold(filepath.Ext(entry.Name()), ".jar") {
			publishHash := published[strings.ToLower(entry.Name())]
			disabled := dest + ".disabled"
			_, collision := os.Lstat(filepath.Join(live, entry.Name()+".disabled"))
			if publishHash != "" || collision == nil {
				input, err := os.Open(source)
				if err != nil {
					return err
				}
				hasher := sha256.New()
				_, copyErr := io.Copy(hasher, input)
				if err := errors.Join(copyErr, input.Close()); err != nil {
					return err
				}
				digest := hex.EncodeToString(hasher.Sum(nil))
				if publishHash == digest {
					continue
				}
				if collision == nil {
					disabled = filepath.Join(next, "sintet-"+digest[:16]+".jar.disabled")
				}
			}
			dest = disabled
		}
		if _, err := os.Stat(dest); os.IsNotExist(err) {
			if err := copyWorkshopFile(source, dest); err != nil {
				return err
			}
		} else if err != nil {
			return err
		}
	}
	return nil
}

func (a *App) launchLinkedBuild(profile, name string) (bool, error) {
	m := a.sharedBuilds
	if m == nil || !shareableWorkshopProfile(profile) {
		return false, nil
	}
	s, err := m.session()
	if err != nil {
		return false, nil
	}
	id := m.linkedBuild(s, profile)
	if id == "" {
		return false, nil
	}
	if a.internetOffline.Load() {
		return true, a.launchSharedInstance(profile, name, 0, false, nil, s.account, true)
	}
	build, err := m.build(s, "detail", id, nil)
	if identity.TemporaryServiceError(err) || err != nil && err.Error() == "Сборка недоступна" {
		return true, a.launchSharedInstance(profile, name, 0, false, nil, s.account, true)
	}
	if err != nil {
		return true, err
	}
	return true, a.runSharedBuild(id, build.DraftVersion, 0, true, name)
}

func (a *App) sharedLauncherProgress(value SharedBuildProgress) SharedBuildProgress {
	if value.Stage != "Установка Minecraft" {
		return value
	}
	state := a.State()
	if state.Busy && strings.HasPrefix(state.Profile, sharedProfilePrefix) {
		value.Done, value.Total = int64(state.Percent), 100
	}
	return value
}

func withoutSharedInstallations(profiles []launcher.InstalledProfile) []launcher.InstalledProfile {
	result := make([]launcher.InstalledProfile, 0, len(profiles))
	for _, profile := range profiles {
		if !strings.HasPrefix(profile.ID, sharedProfilePrefix) {
			result = append(result, profile)
		}
	}
	return result
}
