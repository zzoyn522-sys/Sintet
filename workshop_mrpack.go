package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/workshop"
)

var mrpackFilter = []runtime.FileFilter{{DisplayName: "Modrinth Modpack", Pattern: "*.mrpack"}}

func (a *App) PrepareWorkshopPackImport() (*workshop.PackPlan, error) {
	if a.workshop == nil || a.ctx == nil {
		return nil, errors.New("мастерская ещё запускается")
	}
	path, err := runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Импорт .mrpack", Filters: mrpackFilter,
	})
	if err != nil || path == "" {
		return nil, err
	}
	if !strings.EqualFold(filepath.Ext(path), ".mrpack") {
		return nil, errors.New("выберите файл .mrpack")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 8<<30 {
		return nil, errors.New("архив сборки превышает лимит установки или недоступен")
	}
	a.mu.RLock()
	root := a.settings.GameDirectory
	a.mu.RUnlock()
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	fallback := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	plan, err := a.workshop.PrepareLocalPack(ctx, file, fallback, filepath.Join(root, ".sintet", "cache", "modpacks"))
	if err != nil {
		return nil, err
	}
	// Return a name the user can edit in the import dialog.
	choices := []string{plan.Project.Name, fallback, "Modrinth"}
	for _, value := range choices {
		runes := []rune(strings.TrimSpace(value))
		if len(runes) > 40 {
			runes = runes[:40]
		}
		if name, nameErr := normalizeWorkshopBuildName(string(runes)); nameErr == nil {
			plan.Project.Name = name
			break
		}
	}
	return &plan, nil
}

func (a *App) ExportWorkshopPack(id string) (bool, error) {
	if a.workshop == nil || a.ctx == nil {
		return false, errors.New("мастерская ещё запускается")
	}
	if !strings.HasPrefix(id, "build-") {
		return false, errors.New("выберите установленную сборку")
	}
	build, ok := a.workshopBuildByID(id)
	if !ok {
		return false, errors.New("сборка не найдена")
	}
	path, err := runtime.SaveFileDialog(a.ctx, runtime.SaveDialogOptions{
		Title: "Экспорт .mrpack", DefaultFilename: buildSlug(build.Name) + ".mrpack",
		Filters: mrpackFilter,
	})
	if err != nil || path == "" {
		return false, err
	}
	if filepath.Ext(path) == "" {
		path += ".mrpack"
	}
	if !strings.EqualFold(filepath.Ext(path), ".mrpack") {
		return false, errors.New("используйте расширение .mrpack")
	}
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	service := a.service
	a.mu.RUnlock()
	if state := service.State(); state.Busy || state.Running || a.workshopBusy() {
		return false, errors.New("закройте Minecraft и дождитесь текущих загрузок")
	}
	build, ok = a.workshopBuildByID(id)
	if !ok {
		return false, errors.New("сборка не найдена")
	}
	installed, err := service.ProfileIdentity(id)
	if err != nil {
		return false, err
	}
	dir, err := service.ProfileDirectory(id, "profile")
	if err != nil {
		return false, err
	}
	ctx, cancel := context.WithTimeout(a.ctx, 12*time.Minute)
	defer cancel()
	if err := a.workshop.ExportPack(ctx, dir, path, build.Name, installed.Base, installed.Loader, installed.LoaderVersion); err != nil {
		return false, err
	}
	return true, nil
}
