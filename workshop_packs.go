package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"sintet/internal/workshop"
)

func (a *App) PrepareCatalogPack(provider, project, version string) (workshop.PackPlan, error) {
	if a.workshop == nil {
		return workshop.PackPlan{}, errors.New("мастерская ещё запускается")
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	a.mu.RLock()
	root := a.settings.GameDirectory
	a.mu.RUnlock()
	return a.workshop.PreparePack(ctx, provider, project, version, filepath.Join(root, ".sintet", "cache", "modpacks"))
}

func (a *App) DiscardCatalogPack(planID string) {
	if a.workshop != nil {
		a.workshop.DiscardPack(planID)
	}
}
func (a *App) CatalogPackTargets(planID string) ([]WorkshopBuild, error) {
	result := []WorkshopBuild{}
	plan, err := a.workshop.PackPlan(planID)
	if err != nil {
		return result, err
	}
	builds, err := a.WorkshopBuilds()
	if err != nil {
		return result, err
	}
	for _, build := range builds {
		if build.Installed && build.Game == plan.Game && build.Loader == plan.Loader {
			result = append(result, build)
		}
	}
	return result, nil
}
func sameLoaderVersion(game, loader, a, b string) bool {
	if loader == "vanilla" {
		return true
	}
	if loader == "forge" {
		a = strings.TrimPrefix(a, game+"-")
		b = strings.TrimPrefix(b, game+"-")
	}
	return a == b
}
func (a *App) InstallCatalogPack(planID, targetID, name string) (WorkshopBuild, error) {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	service, settings := a.service, a.settings
	a.mu.RUnlock()
	if state := service.State(); state.Busy || state.Running || a.workshopBusy() {
		return WorkshopBuild{}, errors.New("закройте Minecraft и дождитесь текущих загрузок")
	}
	plan, err := a.workshop.PackPlan(planID)
	if err != nil {
		return WorkshopBuild{}, err
	}
	store, err := a.readWorkshopBuilds()
	if err != nil {
		return WorkshopBuild{}, err
	}
	var build WorkshopBuild
	newBuild := targetID == ""
	if newBuild {
		if len(store.Builds) >= 500 {
			return build, errors.New("достигнут лимит сборок")
		}
		if name == "" {
			runes := []rune(plan.Project.Name)
			if len(runes) > 40 {
				runes = runes[:40]
			}
			name = string(runes)
		}
		name, err = normalizeWorkshopBuildName(name)
		if err != nil {
			return build, err
		}
		var random [5]byte
		if _, err = rand.Read(random[:]); err != nil {
			return build, err
		}
		build = WorkshopBuild{ID: "build-" + buildSlug(name) + "-" + hex.EncodeToString(random[:]), Name: name, Icon: "grass", Game: plan.Game, Loader: plan.Loader, LoaderVersion: plan.LoaderVersion, UseGlobalSettings: true, MinMemoryMB: 512, MemoryMB: 4096, Width: 1280, Height: 720}
		store.Builds = append(store.Builds, build)
	} else {
		if !regexp.MustCompile(`^build-[a-z0-9-]+-[0-9a-f]{10}$`).MatchString(targetID) {
			return build, errors.New("неверная сборка назначения")
		}
		for _, candidate := range store.Builds {
			if candidate.ID == targetID {
				build = candidate
				break
			}
		}
		if build.ID == "" || !service.Installed(build.ID) {
			return build, errors.New("выберите установленную пользовательскую сборку")
		}
		installed, e := service.ProfileIdentity(build.ID)
		if e != nil || installed.Base != plan.Game || installed.Loader != plan.Loader {
			return build, errors.New("версия Minecraft или загрузчика отличается; создайте отдельную сборку")
		}
	}
	directory := filepath.Join(service.Root, "instances", build.ID)
	if newBuild {
		if err = os.MkdirAll(directory, 0700); err != nil {
			return build, err
		}
		if err = a.writeWorkshopBuildManifest(service.Root, build); err != nil {
			return build, err
		}
		if err = a.saveWorkshopBuilds(store); err != nil {
			return build, err
		}
	}
	ctx := a.ctx
	if ctx == nil {
		ctx = context.Background()
	}
	target := workshop.Target{ID: build.ID, Name: build.Name, Directory: directory, Game: build.Game, Loader: build.Loader}
	err = a.workshop.StartPack(ctx, planID, target, func(ctx context.Context, p workshop.PackPlan, progress func(string, float64)) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		identity, identityErr := service.ProfileIdentity(build.ID)
		needsBase := !service.Installed(build.ID) || identityErr != nil || identity.Base != p.Game || identity.Loader != p.Loader || !sameLoaderVersion(p.Game, p.Loader, identity.LoaderVersion, p.LoaderVersion)
		if !needsBase {
			return nil
		}
		progress("Устанавливаем Minecraft и загрузчик", 90)
		if err := service.StartInstallAs(build.ID, p.Game, p.Loader, p.LoaderVersion, settings.JavaPath); err != nil {
			return err
		}
		done := make(chan struct{})
		go func() { service.WaitInstall(); close(done) }()
		tick := time.NewTicker(500 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-ctx.Done():
				service.Cancel()
				<-done
				return ctx.Err()
			case <-tick.C:
				state := service.State()
				progress(state.Stage, 90+state.Percent*.09)
			case <-done:
				actual, identityErr := service.ProfileIdentity(build.ID)
				if service.State().Error != "" || !service.Installed(build.ID) || identityErr != nil || actual.Base != p.Game || actual.Loader != p.Loader || !sameLoaderVersion(p.Game, p.Loader, actual.LoaderVersion, p.LoaderVersion) {
					message := service.State().Error
					if message == "" {
						message = "не удалось установить основу сборки"
					}
					return errors.New(message)
				}
				a.contentMu.Lock()
				defer a.contentMu.Unlock()
				store, e := a.readWorkshopBuilds()
				if e != nil {
					return e
				}
				for i := range store.Builds {
					if store.Builds[i].ID == build.ID {
						store.Builds[i].Game = p.Game
						store.Builds[i].Loader = p.Loader
						store.Builds[i].LoaderVersion = p.LoaderVersion
						if e = a.writeWorkshopBuildManifest(service.Root, store.Builds[i]); e != nil {
							return e
						}
						return a.saveWorkshopBuilds(store)
					}
				}
				return errors.New("сборка назначения не найдена")
			}
		}
	})
	if err != nil {
		return build, err
	}
	return build, nil
}
