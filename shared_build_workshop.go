package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"sintet/internal/accountfiles"
	"sintet/internal/packsync"
	"sintet/internal/workshop"
)

const sharedWorkshopPrefix = "shared:"

func (m *sharedBuildManager) bindLegacyOrigins(s sharedBuildSession, builds []SharedBuild) {
	claimed := make(map[string]bool)
	for _, build := range builds {
		if build.Role == "owner" && build.OriginProfile != "" {
			claimed[build.OriginProfile] = true
		}
	}
	if !hasUnboundOwner(builds) {
		return
	}
	store, err := m.app.readWorkshopBuilds()
	if err != nil {
		return
	}
	matches := make(map[int][]string)
	uses := make(map[string]int)
	for i, build := range builds {
		if build.Role != "owner" || build.OriginProfile != "" {
			continue
		}
		for _, local := range store.Builds {
			if shareableWorkshopProfile(local.ID) && !claimed[local.ID] &&
				strings.EqualFold(build.Name, local.Name) && sharedSameBase(build, local) {
				matches[i] = append(matches[i], local.ID)
				uses[local.ID]++
			}
		}
	}
	for i, ids := range matches {
		if len(ids) == 1 && uses[ids[0]] == 1 {
			if err := s.request(m.ctx, "bind", builds[i].ID, map[string]any{"origin": ids[0]}, nil); err == nil {
				builds[i].OriginProfile = ids[0]
			}
		}
	}
}

func hasUnboundOwner(builds []SharedBuild) bool {
	for _, build := range builds {
		if build.Role == "owner" && build.OriginProfile == "" {
			return true
		}
	}
	return false
}

func (a *App) ModWorkshopTargets() ([]workshop.Target, error) {
	targets, localErr := a.WorkshopTargets()
	if a.sharedBuilds == nil {
		return targets, localErr
	}
	builds, err := a.SharedBuilds()
	if err != nil {
		return targets, localErr
	}
	for _, build := range builds {
		if (build.Role != "owner" && build.Role != "editor") || !sharedUUID(build.ID) {
			continue
		}
		target := workshop.Target{
			ID: sharedWorkshopPrefix + build.ID, Name: build.Name, Game: build.Game,
			Loader: build.Loader, SharedID: build.ID, ReadOnly: sharedEditProblem(build), RequiresApproval: build.Role == "editor",
		}
		linked := false
		if build.Role == "owner" && build.OriginProfile != "" {
			for i := range targets {
				if targets[i].ID == build.OriginProfile {
					targets[i].SharedID, targets[i].ReadOnly = target.SharedID, target.ReadOnly
					linked = true
					break
				}
			}
		}
		if !linked {
			targets = append(targets, target)
		}
	}
	if len(targets) == 0 {
		return targets, localErr
	}
	return targets, nil
}

func (a *App) sharedWorkshopID(target string) string {
	if id, ok := strings.CutPrefix(target, sharedWorkshopPrefix); ok && sharedUUID(id) {
		return id
	}
	m := a.sharedBuilds
	if m != nil && shareableWorkshopProfile(target) {
		if session, err := m.session(); err == nil {
			return m.linkedBuild(session, target)
		}
	}
	return ""
}

func sharedEditProblem(build SharedBuild) string {
	switch {
	case build.OfflineReason != "":
		return build.OfflineReason
	case build.Role == "owner":
		return ""
	case !build.AccessOpen:
		return "Лидер закрыл общий доступ"
	case !build.OwnerOnline:
		return "Лидер сборки офлайн"
	default:
		return ""
	}
}

func (a *App) sharedWorkshopItems(build SharedBuild) []workshop.LocalItem {
	items := make([]workshop.LocalItem, 0, len(build.Mods))
	for _, mod := range build.Mods {
		filename := filepath.Base(mod.File.Path)
		name := mod.Metadata.Name
		if name == "" {
			name = filename
		}
		items = append(items, workshop.LocalItem{
			Name: name, Filename: filename, Size: mod.File.Size, Version: mod.Metadata.Version,
			Loader: build.Loader, Icon: mod.Metadata.Icon, Provider: mod.Metadata.Provider,
			Project: mod.Metadata.Project, Authors: mod.AuthorName, ModID: strings.TrimPrefix(mod.Key, "mod:"),
			Enabled: mod.Enabled, Approved: mod.Approved, Shared: true, ReadOnly: sharedEditProblem(build),
		})
	}
	return items
}

func (a *App) changeSharedWorkshopMod(id, filename, action string, enabled bool) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	build, err := m.build(s, "detail", id, nil)
	if err != nil {
		return err
	}
	if reason := sharedEditProblem(build); reason != "" {
		return errors.New(reason)
	}
	for _, mod := range build.Mods {
		if strings.EqualFold(filepath.Base(mod.File.Path), filename) {
			return s.request(m.ctx, action, id, map[string]any{
				"key": mod.Key, "version": build.DraftVersion, "enabled": enabled,
			}, nil)
		}
	}
	return errors.New("мод уже изменился в общей сборке")
}

func (m *sharedBuildManager) prepareWorkshopMod(ctx context.Context, manager *workshop.Manager, build SharedBuild, target, provider, project, release string) (workshop.Plan, error) {
	if reason := sharedEditProblem(build); reason != "" {
		return workshop.Plan{}, errors.New(reason)
	}
	mods := make([]workshop.Installed, 0, len(build.Mods))
	for _, mod := range build.Mods {
		if !mod.Enabled || mod.Metadata.Project == "" {
			continue
		}
		entry := workshop.Installed{Provider: mod.Metadata.Provider, Project: mod.Metadata.Project,
			Filename: filepath.Base(mod.File.Path), Name: mod.Metadata.Name, Hash: mod.File.SHA256, SHA1: mod.File.SHA1}
		for _, source := range mod.File.Sources {
			if source.Provider == entry.Provider {
				entry.URL = source.URL
				break
			}
		}
		mods = append(mods, entry)
	}
	return manager.PrepareSharedMod(ctx, provider, project, release, workshop.Target{
		ID: target, Name: build.Name, Game: build.Game, Loader: build.Loader, SharedID: build.ID, RequiresApproval: build.Role == "editor",
	}, build.DraftVersion, mods)
}

func (m *sharedBuildManager) commitWorkshopMods(s sharedBuildSession, ctx context.Context, plan workshop.Plan, directory string) error {
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	active, err := m.session()
	if err != nil || active.account != s.account {
		return errors.New("аккаунт изменился")
	}
	build, err := m.editingBuild(s, plan.Target.SharedID, plan.DraftVersion)
	if err != nil {
		return err
	}
	mods := make([]sharedBuildMod, 0, len(plan.Entries))
	for _, entry := range plan.Entries {
		file, key, err := cacheSharedMod(ctx, m.cache(s, build.ID), filepath.Join(directory, "mods", entry.Version.Filename))
		if err != nil {
			return err
		}
		provider := entry.Project.Provider
		if provider == "modrinth" || provider == "curseforge" {
			file.Sources = []packsync.Source{{Provider: provider, URL: entry.Version.URL}}
		}
		mods = append(mods, sharedBuildMod{Key: key, File: file, Metadata: sharedModMetadata{
			Name: entry.Project.Name, Version: entry.Version.Name, Icon: entry.Project.Icon, Provider: provider, Project: entry.Project.ID,
		}})
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	return s.request(ctx, "add", build.ID, map[string]any{"version": build.DraftVersion, "mods": mods}, nil)
}

// Older launchers installed catalogue mods straight into the leader's directory.
// Recover those verified local entries once, without replacing shared choices.
func (m *sharedBuildManager) recoverWorkshopMods(s sharedBuildSession, build SharedBuild) (SharedBuild, error) {
	if build.OwnerID != s.account || !shareableWorkshopProfile(build.OriginProfile) || build.UpdatedAt.IsZero() {
		return build, nil
	}
	marker := filepath.Join(m.buildRoot(s), build.ID, "workshop-recovery-v1")
	if _, err := os.Stat(marker); !os.IsNotExist(err) {
		return build, err
	}
	a := m.app
	a.mu.RLock()
	dir, err := a.service.ProfileDirectory(build.OriginProfile, "profile")
	a.mu.RUnlock()
	if err != nil {
		return build, nil
	}
	records, err := workshop.RegisteredMods(dir)
	if err != nil {
		return build, nil
	}
	cache := m.cache(s, build.ID)
	cutoff := build.UpdatedAt
	objects, _ := os.ReadDir(cache)
	for _, object := range objects {
		if !sharedDigest.MatchString(object.Name()) {
			continue
		}
		if info, err := object.Info(); err == nil && info.Mode().IsRegular() && info.ModTime().Before(cutoff) {
			cutoff = info.ModTime()
		}
	}
	keys, paths := make(map[string]bool), make(map[string]bool)
	for _, mod := range build.Mods {
		keys[mod.Key] = true
		paths[strings.ToLower(mod.File.Path)] = true
	}
	var recovered []sharedBuildMod
	for _, record := range records {
		source := filepath.Join(dir, "mods", record.Filename)
		info, err := os.Lstat(source)
		if err != nil || !info.Mode().IsRegular() || !info.ModTime().After(cutoff) ||
			!sharedDigest.MatchString(record.Hash) || paths[strings.ToLower("mods/"+record.Filename)] {
			continue
		}
		if cached, err := os.Stat(filepath.Join(cache, record.Hash)); err == nil && cached.ModTime().Before(info.ModTime()) {
			continue
		}
		file, key, err := cacheSharedMod(m.ctx, cache, source)
		if err != nil || file.SHA256 != record.Hash || keys[key] {
			continue
		}
		keys[key], paths[strings.ToLower(file.Path)] = true, true
		meta := workshop.ModMetadata(filepath.Join(m.cache(s, build.ID), file.SHA256))
		recovered = append(recovered, sharedBuildMod{Key: key, File: file, Metadata: sharedModMetadata{
			Name: record.Name, Version: meta.Version, Icon: record.Icon, Provider: record.Provider, Project: record.Project,
		}})
	}
	if len(recovered) > 0 {
		if err := s.request(m.ctx, "add", build.ID, map[string]any{"version": build.DraftVersion, "mods": recovered}, nil); err != nil {
			return build, err
		}
		build, err = m.build(s, "detail", build.ID, nil)
		if err != nil {
			return build, err
		}
	}
	return build, accountfiles.Write(marker, []byte("1"))
}
