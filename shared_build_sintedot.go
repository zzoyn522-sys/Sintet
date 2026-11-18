package main

import (
	"context"
	"crypto/sha1"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sintet/internal/packsync"
	"sintet/internal/workshop"
)

func (a *App) sintedotSharedID(target string) string {
	if id := a.sharedWorkshopID(target); id != "" {
		return id
	}
	m := a.sharedBuilds
	if m == nil || !strings.HasPrefix(target, sharedProfilePrefix) {
		return ""
	}
	s, err := m.session()
	if err != nil {
		return ""
	}
	builds, err := a.SharedBuilds()
	if err != nil {
		return ""
	}
	for _, build := range builds {
		if m.localProfileID(s, build) == target {
			return build.ID
		}
	}
	return ""
}

func (a *App) runSharedSintedot(targetID, id, mode string, options workshop.SintedotOptions) error {
	m := a.sharedBuilds
	if m == nil {
		return errors.New("сервис общих сборок ещё запускается")
	}
	if !m.operation.TryLock() {
		return errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	s, err := m.session()
	if err != nil {
		return err
	}
	build, err := a.SharedBuildDetail(id)
	if err != nil {
		return err
	}
	notes := []string{"Проверяются моды общего черновика. Конфиги на компьютерах участников не меняются."}
	if reason := sharedEditProblem(build); reason != "" {
		options.DryRun = true
		notes = append(notes, reason+". Доступна проверка без изменений по скачанным файлам.")
	} else if !build.SintedotAvailable {
		notes = append(notes, "На этом сервере правки сохраняются по очереди. Sintedot остановится, если кто-то изменит черновик между шагами.")
	} else if build.Role == "editor" && !options.DryRun {
		notes = append(notes, "Новые версии модов потребуют подтверждения лидера.")
	}
	options.CheckConfigs = false
	workspace, err := os.MkdirTemp("", "sintedot-shared-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(workspace)
	target := workshop.Target{ID: targetID, Name: build.Name, Game: build.Game, Loader: build.Loader, Directory: workspace, SharedID: id}
	return a.workshop.RunSintedotWithOptions(m.ctx, target, mode, notes, options, workshop.SintedotHooks{
		Prepare: func(ctx context.Context, report func(string, float64)) error {
			return m.prepareSharedSintedot(ctx, s, build, target, options.DryRun, report)
		},
		Commit: func(ctx context.Context, stage string) error {
			active, err := m.session()
			if err != nil || active.account != s.account {
				return errors.New("аккаунт изменился")
			}
			mods, disabled, err := m.sintedotSharedPatch(ctx, s, build, stage)
			if err != nil {
				return err
			}
			if len(mods) == 0 && len(disabled) == 0 {
				return nil
			}
			return applySharedSintedotPatch(ctx, s, build, mods, disabled)
		},
	})
}

// Older servers require a separate version-checked write for each toggle.
func applySharedSintedotPatch(ctx context.Context, s sharedBuildSession, build SharedBuild, mods []sharedBuildMod, disabled []string) error {
	if build.SintedotAvailable {
		return writeSharedSintedotPatch(ctx, s, build, "sintedot-apply", map[string]any{"mods": mods, "disable": disabled}, mods, disabled)
	}
	existing := map[string]sharedBuildMod{}
	for _, mod := range build.Mods {
		existing[mod.Key] = mod
	}
	var activate, updates []sharedBuildMod
	for _, mod := range mods {
		old, exists := existing[mod.Key]
		if !exists || old.Enabled {
			updates = append(updates, mod)
			continue
		}
		if old.File.SHA256 != mod.File.SHA256 || old.File.Path != mod.File.Path {
			return errors.New("для замены отключённого мода нужен обновлённый сервис общих сборок")
		}
		activate = append(activate, mod)
	}
	applied := 0
	write := func(action string, input map[string]any, changed []sharedBuildMod, off []string) error {
		err := writeSharedSintedotPatch(ctx, s, build, action, input, changed, off)
		if err != nil && applied > 0 {
			return fmt.Errorf("часть правок уже сохранена (%d). Обнови черновик и повтори ремонт: %w", applied, err)
		}
		if err == nil {
			applied++
			build.DraftVersion++
		}
		return err
	}
	if len(updates) > 0 {
		if err := write("add", map[string]any{"mods": updates}, updates, nil); err != nil {
			return err
		}
	}
	for _, mod := range activate {
		if err := write("toggle", map[string]any{"key": mod.Key, "enabled": true}, []sharedBuildMod{mod}, nil); err != nil {
			return err
		}
	}
	for _, key := range disabled {
		if err := write("toggle", map[string]any{"key": key, "enabled": false}, nil, []string{key}); err != nil {
			return err
		}
	}
	return nil
}

func writeSharedSintedotPatch(ctx context.Context, s sharedBuildSession, build SharedBuild, action string, input map[string]any, mods []sharedBuildMod, disabled []string) error {
	input["version"] = build.DraftVersion
	err := s.request(ctx, action, build.ID, input, nil)
	if err == nil || (!identity.TemporaryServiceError(err) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)) {
		return err
	}
	// The server may have saved the draft before the connection dropped.
	var current SharedBuild
	if readErr := s.request(context.WithoutCancel(ctx), "detail", build.ID, nil, &current); readErr == nil &&
		current.DraftVersion > build.DraftVersion && sharedSintedotPatchPresent(current.Mods, mods, disabled) {
		return nil
	}
	return fmt.Errorf("сервис не подтвердил сохранение общего черновика: %w", err)
}

func sharedSintedotPatchPresent(current, mods []sharedBuildMod, disabled []string) bool {
	entries := map[string]sharedBuildMod{}
	for _, mod := range current {
		entries[mod.Key] = mod
	}
	for _, mod := range mods {
		got := entries[mod.Key]
		if !got.Enabled || got.File.SHA256 != mod.File.SHA256 || got.File.Path != mod.File.Path {
			return false
		}
	}
	for _, key := range disabled {
		if entries[key].Enabled {
			return false
		}
	}
	return true
}

func (m *sharedBuildManager) prepareSharedSintedot(ctx context.Context, s sharedBuildSession, build SharedBuild, target workshop.Target, preview bool, report func(string, float64)) error {
	enabled := make([]sharedBuildMod, 0, len(build.Mods))
	for _, mod := range build.Mods {
		if mod.Enabled {
			enabled = append(enabled, mod)
		}
	}
	report("Сверка файлов общей сборки", 2)
	if err := m.downloadSharedFiles(ctx, s, build, enabled, report); err != nil {
		if preview && sharedEditProblem(build) != "" {
			return fmt.Errorf("для проверки нужны скачанные моды, а часть файлов ещё недоступна: %w", err)
		}
		return err
	}
	folder := filepath.Join(target.Directory, "mods")
	if err := os.Mkdir(folder, 0700); err != nil {
		return err
	}
	records := map[string]workshop.Installed{}
	lists := map[string][]workshop.Version{}
	for _, mod := range enabled {
		if err := ctx.Err(); err != nil {
			return err
		}
		if !packsync.AllowedPath(mod.File.Path) || !strings.HasPrefix(mod.File.Path, "mods/") || !sharedDigest.MatchString(mod.File.SHA256) {
			return errors.New("общая сборка содержит некорректный файл")
		}
		name := filepath.Base(mod.File.Path)
		if err := os.Link(filepath.Join(m.cache(s, build.ID), mod.File.SHA256), filepath.Join(folder, name)); err != nil {
			source, err := os.Open(filepath.Join(m.cache(s, build.ID), mod.File.SHA256))
			if err != nil {
				return err
			}
			err = packsync.CopyVerified(ctx, source, filepath.Join(target.Directory, "objects"), mod.File, nil)
			_ = source.Close()
			if err != nil {
				return err
			}
			if err = os.Rename(filepath.Join(target.Directory, "objects", mod.File.SHA256), filepath.Join(folder, name)); err != nil {
				return err
			}
		}
		provider, project := mod.Metadata.Provider, mod.Metadata.Project
		if (provider != "modrinth" && provider != "curseforge" && provider != "sintedmods") || project == "" {
			continue
		}
		key := provider + ":" + project
		if _, ok := lists[key]; !ok {
			rows, _ := m.app.workshop.Client.Versions(ctx, provider, project, target.Game, target.Loader)
			lists[key] = rows
		}
		if len(lists[key]) == 0 {
			continue
		}
		file, err := os.Open(filepath.Join(folder, name))
		if err != nil {
			return err
		}
		h1, h512 := sha1.New(), sha512.New()
		_, err = io.Copy(io.MultiWriter(h1, h512), file)
		_ = file.Close()
		if err != nil {
			return err
		}
		digests := map[string]string{"sha1": hex.EncodeToString(h1.Sum(nil)), "sha512": hex.EncodeToString(h512.Sum(nil)), "sha256": mod.File.SHA256}
		for _, v := range lists[key] {
			if v.Project != project || v.Size != mod.File.Size || v.Hash == "" || !strings.EqualFold(digests[v.Algorithm], v.Hash) {
				continue
			}
			records[key] = workshop.Installed{Provider: provider, Project: project, Version: v.ID, Filename: name, Name: mod.Metadata.Name, Icon: mod.Metadata.Icon, Hash: mod.File.SHA256, URL: v.URL}
			break
		}
	}
	data, err := json.Marshal(records)
	if err != nil {
		return err
	}
	return accountfiles.Write(filepath.Join(target.Directory, ".sintet-mods.json"), data)
}

func (m *sharedBuildManager) sintedotSharedPatch(ctx context.Context, s sharedBuildSession, build SharedBuild, stage string) ([]sharedBuildMod, []string, error) {
	records, err := workshop.RegisteredMods(stage)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]workshop.Installed{}
	for _, r := range records {
		byName[strings.ToLower(r.Filename)] = r
	}
	priorPaths := map[string]sharedBuildMod{}
	for _, mod := range build.Mods {
		priorPaths[strings.ToLower(mod.File.Path)] = mod
	}
	entries, err := os.ReadDir(filepath.Join(stage, "mods"))
	if err != nil {
		return nil, nil, err
	}
	updates, disabled := []sharedBuildMod{}, []string{}
	seen := map[string]bool{}
	for _, entry := range entries {
		name := entry.Name()
		if !entry.Type().IsRegular() || !strings.HasSuffix(strings.ToLower(name), ".jar") {
			continue
		}
		file, key, err := cacheSharedMod(ctx, m.cache(s, build.ID), filepath.Join(stage, "mods", name))
		if err != nil {
			return nil, nil, err
		}
		if original := priorPaths[strings.ToLower(file.Path)]; original.Enabled && original.File.SHA256 == file.SHA256 {
			seen[original.Key] = true
			continue
		}
		if seen[key] {
			return nil, nil, fmt.Errorf("общий черновик содержит несколько файлов %s", key)
		}
		seen[key] = true
		r := byName[strings.ToLower(name)]
		if r.URL != "" && (r.Provider == "modrinth" || r.Provider == "curseforge") {
			file.Sources = []packsync.Source{{Provider: r.Provider, URL: r.URL}}
		}
		metadata := workshop.ModMetadata(filepath.Join(stage, "mods", name))
		updates = append(updates, sharedBuildMod{Key: key, File: file, Metadata: sharedModMetadata{
			Name: metadata.Name, Version: metadata.Version, Provider: r.Provider, Project: r.Project, Icon: r.Icon,
		}})
	}
	for _, mod := range build.Mods {
		if mod.Enabled && !seen[mod.Key] {
			disabled = append(disabled, mod.Key)
		}
	}
	return updates, disabled, nil
}
