package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sintet/internal/workshop"
)

func newWorkshop(root string) *workshop.Manager {
	key := os.Getenv("SINTET_CURSEFORGE_API_KEY")
	if key == "" {
		key, _ = identity.ReadServiceSecret(filepath.Join(root, "curseforge.dpapi"))
	}
	client := workshop.New(key)
	if identity.ThemePortalIsLocal() && os.Getenv("SINTET_ID_URL") == "" {
		client.Sintet = "http://127.0.0.1:3033/api/catalog"
	}
	return workshop.NewManager(client)
}
func (a *App) SearchMods(provider, query string, offset int) (workshop.SearchResult, error) {
	return a.workshop.Client.Search(context.Background(), provider, query, offset)
}
func (a *App) SearchModsFiltered(provider, query string, offset int, filters workshop.SearchFilters) (workshop.SearchResult, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
	defer cancel()
	return a.workshop.Client.SearchFiltered(ctx, provider, query, offset, filters)
}

type WorkshopFilterOptions struct {
	PackTopics     []workshop.Topic `json:"packTopics"`
	ResourceTopics []workshop.Topic `json:"resourceTopics"`
	Games          []string         `json:"games"`
	Snapshots      []string         `json:"snapshots"`
	Topics         []workshop.Topic `json:"topics"`
}

func (a *App) WorkshopFilterOptions() (WorkshopFilterOptions, error) {
	result := WorkshopFilterOptions{PackTopics: workshop.PackTopics, Games: []string{}, Snapshots: []string{}, Topics: workshop.Topics, ResourceTopics: workshop.ResourceTopics}
	catalog, err := a.ListVersions()
	if err != nil {
		return result, err
	}
	for _, version := range catalog.Versions {
		if version.Type == "release" {
			result.Games = append(result.Games, version.ID)
		} else if version.Type == "snapshot" {
			result.Snapshots = append(result.Snapshots, version.ID)
		}
	}
	return result, nil
}
func (a *App) ModDetails(provider, id string) (workshop.Project, error) {
	return a.workshop.Client.Project(context.Background(), provider, id)
}
func (a *App) TranslateWorkshopDescription(description, language string) (string, error) {
	a.mu.RLock()
	enabled := a.settings.WorkshopLanguage == language
	a.mu.RUnlock()
	if !enabled || language == "" {
		return description, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 18*time.Second)
	defer cancel()
	return a.workshop.Client.TranslateDescription(ctx, description, language)
}
func (a *App) ModVersions(provider, id, game, loader string) ([]workshop.Version, error) {
	return a.workshop.Client.Versions(context.Background(), provider, id, game, loader)
}
func (a *App) WorkshopExtraVersions(provider, id, kind, game, platform string) ([]workshop.Version, error) {
	return a.workshop.Client.ExtraContentVersions(context.Background(), provider, id, kind, game, platform)
}
func (a *App) WorkshopWorlds(targetID string) ([]string, error) {
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return workshop.LocalWorlds(target.Directory)
}
func (a *App) WorkshopLocalDataPacks(targetID string) ([]workshop.WorldContent, error) {
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return nil, err
	}
	return workshop.LocalWorldDataPacks(target.Directory)
}
func (a *App) PrepareWorkshopExtra(provider, project, version, kind, targetID, world, game, platform string) (workshop.Plan, error) {
	if kind == "plugins" {
		target := workshop.Target{Game: game, Loader: platform}
		return a.workshop.PrepareExtraContent(context.Background(), provider, project, version, kind, target, "")
	}
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return workshop.Plan{}, err
	}
	if target.Game != game {
		return workshop.Plan{}, errors.New("выберите сборку с подходящей версией Minecraft")
	}
	return a.workshop.PrepareExtraContent(context.Background(), provider, project, version, kind, target, world)
}
func (a *App) InstallWorkshopPlugin(planID string) (bool, error) {
	server, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Выбери корневую папку сервера Minecraft"})
	if err != nil || server == "" {
		return false, err
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if a.workshopBusy() {
		return false, errors.New("дождитесь окончания установки")
	}
	if err := a.workshop.StartPlugin(a.ctx, planID, server); err != nil {
		return false, err
	}
	return true, nil
}
func (a *App) ResourceVersions(provider, id, game string) ([]workshop.Version, error) {
	return a.workshop.Client.ResourceVersions(context.Background(), provider, id, game)
}
func (a *App) PrepareResource(provider, project, version, targetID string) (workshop.Plan, error) {
	return a.prepareWorkshopZip(provider, project, version, targetID, "resources")
}
func (a *App) ShaderVersions(provider, id, game string) ([]workshop.Version, error) {
	return a.workshop.Client.ZipPackVersions(context.Background(), provider, id, game, "shaders")
}
func (a *App) PrepareShader(provider, project, version, targetID string) (workshop.Plan, error) {
	return a.prepareWorkshopZip(provider, project, version, targetID, "shaders")
}
func (a *App) prepareWorkshopZip(provider, project, version, targetID, kind string) (workshop.Plan, error) {
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return workshop.Plan{}, err
	}
	return a.workshop.PrepareZipPack(context.Background(), provider, project, version, kind, target)
}
func (a *App) workshopBusy() bool {
	return a.remotePackBusy.Load() || a.workshop != nil && a.workshop.State().Busy
}
func (a *App) WorkshopTargets() ([]workshop.Target, error) {
	a.mu.RLock()
	service := a.service
	mode := a.settings.InstanceMode
	a.mu.RUnlock()
	names := a.buildNameMap()
	result := []workshop.Target{}
	for _, profile := range withoutSharedInstallations(service.Installations()) {
		dir, err := service.ProfileDirectory(profile.ID, mode)
		if err != nil {
			return nil, err
		}
		result = append(result, workshop.Target{ID: profile.ID, Name: names[profile.ID], Game: profile.Base, Loader: profile.Loader, Directory: dir})
	}
	return result, nil
}
func (a *App) workshopTarget(id string) (workshop.Target, error) {
	if strings.HasPrefix(id, sharedProfilePrefix) {
		return workshop.Target{}, errors.New("изменяй моды через общую сборку")
	}
	names := a.buildNameMap()
	for _, profile := range a.service.Installations() {
		if profile.ID == id {
			dir, err := a.service.ProfileDirectory(id, a.settings.InstanceMode)
			return workshop.Target{ID: id, Name: names[id], Game: profile.Base, Loader: profile.Loader, Directory: dir}, err
		}
	}
	return workshop.Target{}, errors.New("сборка не установлена")
}
func (a *App) PrepareMod(provider, project, version, targetID string) (workshop.Plan, error) {
	if id := a.sharedWorkshopID(targetID); id != "" {
		build, err := a.SharedBuildDetail(id)
		if err != nil {
			return workshop.Plan{}, err
		}
		return a.sharedBuilds.prepareWorkshopMod(a.sharedBuilds.ctx, a.workshop, build, targetID, provider, project, version)
	}
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return workshop.Plan{}, err
	}
	return a.workshop.Prepare(context.Background(), provider, project, version, target)
}
func (a *App) InstallMod(planID string, dependencies bool) error {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if state := a.State(); state.Busy || state.Running {
		return errors.New("закройте игру и дождитесь установки Minecraft")
	}
	target, err := a.workshop.PlanTarget(planID)
	if err != nil {
		return err
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if target.SharedID != "" {
		m := a.sharedBuilds
		s, err := m.session()
		if err != nil {
			return err
		}
		return a.workshop.StartWithCommit(ctx, planID, dependencies, func(ctx context.Context, plan workshop.Plan, directory string) error {
			return m.commitWorkshopMods(s, ctx, plan, directory)
		})
	}
	if a.sharedWorkshopID(target.ID) != "" {
		return errors.New("общий доступ к сборке изменился; выберите мод заново")
	}
	a.mu.RLock()
	current, err := a.workshopTarget(target.ID)
	a.mu.RUnlock()
	if err != nil || !strings.EqualFold(current.Directory, target.Directory) {
		return errors.New("папка сборки изменилась; выберите мод заново")
	}
	return a.workshop.Start(ctx, planID, dependencies)
}
func (a *App) lockSintedotContent() bool {
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	deadline := time.NewTimer(5 * time.Second)
	defer deadline.Stop()
	for {
		if a.contentMu.TryLock() {
			return true
		}
		if a.workshopBusy() {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-deadline.C:
			return false
		case <-tick.C:
		}
	}
}

func (a *App) RunSintedot(targetID, mode string) error {
	return a.RunSintedotWithOptions(targetID, mode, workshop.SintedotOptions{CheckConfigs: true})
}
func (a *App) RunSintedotWithOptions(targetID, mode string, options workshop.SintedotOptions) error {
	if !a.lockSintedotContent() {
		return errors.New("дождись завершения текущей операции")
	}
	defer a.contentMu.Unlock()
	return a.runSintedotLocked(targetID, mode, options)
}

func (a *App) runSintedotLocked(targetID, mode string, options workshop.SintedotOptions) error {
	if state := a.State(); state.Busy || state.Running || a.workshopBusy() {
		return errors.New("закрой игру и дождись завершения установки")
	}
	if id := a.sintedotSharedID(targetID); id != "" {
		return a.runSharedSintedot(targetID, id, mode, options)
	}
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	service := a.service
	instanceMode := a.settings.InstanceMode
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	var findings []string
	if !strings.HasPrefix(targetID, "build-") && instanceMode != "profile" {
		options.DryRun = true
		findings = append(findings, "Эту папку используют несколько профилей. Доступна проверка без изменений.")
	}
	if mode == "repair" {
		if journal, err := service.Journal(""); err == nil && journal.Profile == targetID {
			for _, finding := range journal.Findings {
				findings = append(findings, finding.Title+" — "+finding.Advice)
			}
		}
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	return a.workshop.RunSintedotWithOptions(ctx, target, mode, findings, options, workshop.SintedotHooks{})
}

// RunSintedotReport keeps the terminal stage and structured details in one
// response. Preflight errors may occur before the manager creates a run.
func (a *App) RunSintedotReport(targetID, mode string, options workshop.SintedotOptions) workshop.SintedotState {
	if !a.lockSintedotContent() {
		return workshop.SintedotState{Target: targetID, InstallState: workshop.InstallState{Stage: "Другая операция уже выполняется", Error: "Дождись завершения текущей операции"}}
	}
	defer a.contentMu.Unlock()
	before := a.SintedotState().RunID
	err := a.runSintedotLocked(targetID, mode, options)
	state := a.SintedotState()
	if state.Target == targetID && state.RunID != before && !state.Busy {
		return state
	}
	if err == nil {
		return workshop.SintedotState{Target: targetID, InstallState: workshop.InstallState{Stage: "Готово", Percent: 100}}
	}
	return workshop.SintedotState{Target: targetID, Preview: options.DryRun, InstallState: workshop.InstallState{Stage: "Ошибка Sintedot", Error: err.Error()}}
}

func (a *App) SintedotCanRollback(profileID string) bool {
	if id := a.sintedotSharedID(profileID); id != "" {
		build, err := a.SharedBuildDetail(id)
		return err == nil && build.SintedotCanRollback && sharedEditProblem(build) == ""
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	target, err := a.workshopTarget(profileID)
	return err == nil && (strings.HasPrefix(profileID, "build-") || a.settings.InstanceMode == "profile") &&
		workshop.CanRollbackSintedot(target.Directory)
}

func (a *App) RollbackLastSintedot(profileID string) error {
	if !a.lockSintedotContent() {
		return errors.New("дождись завершения текущей операции")
	}
	defer a.contentMu.Unlock()
	if state := a.State(); state.Busy || state.Running || a.workshopBusy() {
		return errors.New("закрой игру и дождись завершения установки")
	}
	if id := a.sintedotSharedID(profileID); id != "" {
		m := a.sharedBuilds
		if !m.operation.TryLock() {
			return errors.New("дождись окончания операции со сборкой")
		}
		defer m.operation.Unlock()
		session, err := m.session()
		if err != nil {
			return err
		}
		build, err := m.build(session, "detail", id, nil)
		if err != nil {
			return err
		}
		return session.request(m.ctx, "sintedot-rollback", id, map[string]any{"version": build.DraftVersion}, nil)
	}
	a.mu.RLock()
	target, err := a.workshopTarget(profileID)
	mode := a.settings.InstanceMode
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	if !strings.HasPrefix(profileID, "build-") && mode != "profile" {
		return errors.New("выбери сборку с отдельной папкой")
	}
	return workshop.RollbackSintedot(target.Directory)
}

func (a *App) SintedotState() workshop.SintedotState { return a.workshop.SintedotState() }
func (a *App) CancelSintedot()                       { a.workshop.CancelSintedot() }

func (a *App) ModInstallState() workshop.InstallState { return a.workshop.State() }
func (a *App) CancelModInstall()                      { a.workshop.Cancel() }
func (a *App) LocalWorkshop(targetID, kind string) ([]workshop.LocalItem, error) {
	if kind == "mods" {
		if id := a.sharedWorkshopID(targetID); id != "" {
			build, err := a.SharedBuildDetail(id)
			if err != nil {
				return nil, err
			}
			items := a.sharedWorkshopItems(build)
			if build.LocalProfile != nil && build.LocalProfile.ID != "" {
				a.mu.RLock()
				target, targetErr := a.workshopTarget(build.LocalProfile.ID)
				a.mu.RUnlock()
				if targetErr == nil {
					items = workshop.AnalyzeLocalCompatibility(target.Directory, kind, target.Game, target.Loader, items)
				}
			}
			return items, nil
		}
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	target, err := a.workshopTarget(targetID)
	if err != nil {
		return nil, err
	}
	items, err := workshop.Scan(target.Directory, kind)
	if err != nil {
		return nil, err
	}
	items = workshop.AnalyzeLocalCompatibility(target.Directory, kind, target.Game, target.Loader, items)
	for i := range items {
		if data, e := workshop.Icon(target.Directory, kind, items[i].Filename); e == nil {
			digest := sha256.Sum256(data)
			name := hex.EncodeToString(digest[:]) + ".png"
			path := filepath.Join(a.profiles.root, "WorkshopIcons", name)
			if _, e = os.Stat(path); os.IsNotExist(e) {
				e = accountfiles.Write(path, data)
			}
			if e == nil {
				items[i].Icon = "/workshop-icons/" + name
			}
		}
	}
	return items, nil
}
func (a *App) ToggleMod(targetID, filename string, enabled bool) error {
	return a.ToggleLocalWorkshopFile(targetID, "mods", filename, enabled)
}

func (a *App) ToggleLocalWorkshopFile(targetID, kind, filename string, enabled bool) error {
	if kind == "mods" {
		if id := a.sharedWorkshopID(targetID); id != "" {
			return a.changeSharedWorkshopMod(id, filename, "toggle", enabled)
		}
	}
	return a.changeLocalWorkshop(targetID, func(target workshop.Target) error {
		return workshop.ToggleLocal(target.Directory, kind, filename, enabled)
	})
}

func (a *App) RemoveLocalWorkshopFile(targetID, kind, filename string) error {
	if kind == "mods" {
		if id := a.sharedWorkshopID(targetID); id != "" {
			return a.changeSharedWorkshopMod(id, filename, "remove", false)
		}
	}
	return a.changeLocalWorkshop(targetID, func(target workshop.Target) error {
		return workshop.RemoveLocal(target.Directory, kind, filename)
	})
}

func (a *App) PickLocalWorkshopFiles(targetID, kind string) (int, error) {
	var filter runtime.FileFilter
	switch kind {
	case "mods":
		filter = runtime.FileFilter{DisplayName: "Моды Minecraft", Pattern: "*.jar"}
	case "resources":
		filter = runtime.FileFilter{DisplayName: "Ресурс-паки Minecraft", Pattern: "*.zip"}
	case "shaders":
		filter = runtime.FileFilter{DisplayName: "Шейдеры Minecraft", Pattern: "*.zip"}
	default:
		return 0, errors.New("неизвестный тип файлов")
	}
	paths, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Добавить файлы в сборку", Filters: []runtime.FileFilter{filter},
	})
	if err != nil || len(paths) == 0 {
		return 0, err
	}
	return a.AddLocalWorkshopFiles(targetID, kind, paths)
}

func (a *App) AddLocalWorkshopFiles(targetID, kind string, paths []string) (int, error) {
	if kind == "mods" {
		if id := a.sharedWorkshopID(targetID); id != "" {
			build, err := a.SharedBuildDetail(id)
			if err != nil {
				return 0, err
			}
			return a.SharedBuildAddFiles(id, build.DraftVersion, paths)
		}
	}
	var count int
	err := a.changeLocalWorkshop(targetID, func(target workshop.Target) error {
		var err error
		count, err = workshop.AddLocalFiles(target.Directory, kind, paths)
		return err
	})
	return count, err
}

func (a *App) changeLocalWorkshop(targetID string, change func(workshop.Target) error) error {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	if state := a.service.State(); state.Running || state.Busy || a.workshopBusy() {
		return errors.New("закройте игру и дождитесь завершения загрузки")
	}
	target, err := a.workshopTarget(targetID)
	if err != nil {
		return err
	}
	return change(target)
}
func (a *App) OpenWorkshopFolder(targetID, kind string) error {
	if kind != "mods" && kind != "resourcepacks" && kind != "shaderpacks" && kind != "saves" {
		return errors.New("неизвестная папка")
	}
	if id, ok := strings.CutPrefix(targetID, sharedWorkshopPrefix); ok && sharedUUID(id) && kind == "mods" {
		build, err := a.SharedBuildDetail(id)
		if err != nil {
			return err
		}
		if build.LocalProfile == nil {
			return errors.New("сначала скачай ревизию сборки")
		}
		a.mu.RLock()
		dir, err := a.service.ProfileDirectory(build.LocalProfile.ID, "profile")
		a.mu.RUnlock()
		if err != nil {
			return err
		}
		return exec.Command("explorer.exe", filepath.Join(dir, kind)).Start()
	}
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	dir := filepath.Join(target.Directory, kind)
	if err = os.MkdirAll(dir, 0755); err != nil {
		return err
	}
	return exec.Command("explorer.exe", dir).Start()
}

func (a *App) OpenWorkshopAuthorLink(raw string) error {
	if a.ctx == nil || len(raw) > 500 {
		return errors.New("неверная ссылка проекта")
	}
	address, err := url.Parse(raw)
	if err != nil || (address.Scheme != "https" && address.Scheme != "http") || address.User != nil || address.Hostname() == "" {
		return errors.New("неверная ссылка проекта")
	}
	runtime.BrowserOpenURL(a.ctx, address.String())
	return nil
}

func (a *App) OpenWorkshopProject(provider, id string) error {
	info, err := a.workshop.Client.Project(context.Background(), provider, id)
	if err != nil {
		return err
	}
	address, err := url.Parse(info.URL)
	if err != nil || address.Scheme != "https" || address.User != nil {
		return errors.New("неверная ссылка проекта")
	}
	if address.Host != "modrinth.com" && address.Host != "www.curseforge.com" && address.Host != "curseforge.com" {
		return errors.New("неверный сайт проекта")
	}
	runtime.BrowserOpenURL(a.ctx, address.String())
	return nil
}
