package workshop

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sintet/internal/accountfiles"
	"strings"
	"time"
)

func isZipPack(kind string) bool { return kind == "resources" || kind == "shaders" }

func zipPackRegistry(kind string) string {
	if kind == "shaders" {
		return ".sintet-shaderpacks.json"
	}
	return ".sintet-resourcepacks.json"
}

func isShaderArchive(archive *zip.Reader) bool {
	for _, file := range archive.File {
		if !file.Mode().IsRegular() || !safePackRelative(file.Name) || !strings.HasPrefix(file.Name, "shaders/") {
			continue
		}
		switch strings.ToLower(filepath.Ext(file.Name)) {
		case ".vsh", ".fsh", ".gsh", ".csh", ".glsl":
			return true
		}
	}
	return false
}

func safeContentFilename(name, kind string) bool {
	if kind == "modpacks" {
		base := strings.TrimSuffix(strings.TrimSuffix(name, ".mrpack"), ".zip")
		return base != name && safeFilename(base+".jar")
	}
	if kind == "packfile" {
		return safePackPath(name)
	}
	if !isZipPack(kind) {
		return safeFilename(name)
	}
	return strings.HasSuffix(strings.ToLower(name), ".zip") && safeFilename(name[:len(name)-4]+".jar")
}
func resourceRegistry(dir string) (map[string]Installed, error) {
	return readZipPackRegistry(dir, zipPackRegistry("resources"))
}
func readZipPackRegistry(dir, name string) (map[string]Installed, error) {
	records := map[string]Installed{}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if os.IsNotExist(err) {
		return records, nil
	}
	if err != nil {
		return nil, err
	}
	if json.Unmarshal(data, &records) != nil {
		return nil, errors.New("повреждён список установленных паков, файлы сохранены")
	}
	return records, nil
}
func zipPackInstalled(folder, kind string, r Installed) bool {
	if !safeContentFilename(r.Filename, kind) {
		return false
	}
	sum, err := fileHash(filepath.Join(folder, r.Filename))
	return err == nil && sum == r.Hash
}
func (c *Client) ResourceVersions(ctx context.Context, provider, id, game string) ([]Version, error) {
	return c.ZipPackVersions(ctx, provider, id, game, "resources")
}
func (c *Client) ZipPackVersions(ctx context.Context, provider, id, game, kind string) ([]Version, error) {
	if !isZipPack(kind) {
		return nil, errors.New("неизвестный тип пака")
	}
	info, err := c.Project(ctx, provider, id)
	if err != nil {
		return nil, err
	}
	if info.Kind != kind {
		return nil, errors.New("тип проекта не совпадает с выбранной категорией")
	}
	versions, err := c.Versions(ctx, provider, id, game, "")
	for i := range versions {
		versions[i].Kind = kind
	}
	return versions, err
}
func (m *Manager) PrepareResource(ctx context.Context, provider, project, version string, target Target) (Plan, error) {
	return m.PrepareZipPack(ctx, provider, project, version, "resources", target)
}
func (m *Manager) PrepareZipPack(ctx context.Context, provider, project, version, kind string, target Target) (Plan, error) {
	plan := Plan{Kind: kind, Target: target, Entries: []PlanEntry{}, Warnings: []string{}, Created: time.Now()}
	if !isZipPack(kind) {
		return plan, errors.New("неизвестный тип пака")
	}
	if !filepath.IsAbs(target.Directory) || target.Game == "" {
		return plan, errors.New("выберите установленную сборку")
	}
	folder, err := localFolder(target.Directory, kind)
	if err != nil {
		return plan, err
	}
	records, _, err := localRegistry(target.Directory, kind)
	if err != nil {
		return plan, err
	}
	seen := map[string]string{}
	var visit func(string, string) error
	visit = func(id, versionID string) error {
		if len(seen) >= 32 {
			return errors.New("слишком много связанных паков")
		}
		if previous, ok := seen[id]; id != "" && ok {
			if versionID != "" && previous != versionID {
				return errors.New("конфликт версий ресурс-паков")
			}
			return nil
		}
		var v Version
		var err error
		if versionID != "" {
			v, err = m.Client.Version(ctx, provider, id, versionID)
		} else {
			var files []Version
			files, err = m.Client.ZipPackVersions(ctx, provider, id, target.Game, kind)
			if len(files) > 0 {
				v = files[0]
			}
		}
		if err != nil {
			return err
		}
		if v.ID == "" || v.Project == "" {
			return errors.New("нет файла для выбранной версии Minecraft")
		}
		info, err := m.Client.Project(ctx, provider, v.Project)
		if err != nil {
			return err
		}
		if info.Kind != kind {
			plan.Warnings = append(plan.Warnings, "Требуется мод «"+info.Name+"». Установи его в разделе модов.")
			return nil
		}
		if id != "" && v.Project != id {
			return errors.New("неверный файл ресурс-пака")
		}
		v.Kind = kind
		if !v.Compatible(target.Game, "") {
			return errors.New("пак не поддерживает выбранную версию Minecraft")
		}
		if previous, ok := seen[v.Project]; ok {
			if previous != v.ID {
				return errors.New("конфликт версий ресурс-паков")
			}
			return nil
		}
		seen[v.Project] = v.ID
		if v.URL == "" {
			return fmt.Errorf("автор «%s» отключил автоматическое скачивание", info.Name)
		}
		if !safeContentFilename(v.Filename, kind) {
			return errors.New("пак должен быть ZIP-архивом")
		}
		old := records[key(provider, v.Project)]
		plan.Entries = append(plan.Entries, PlanEntry{Project: info, Version: v, Installed: old.Version == v.ID && zipPackInstalled(folder, kind, old)})
		for _, dep := range v.Dependencies {
			if dep.Kind != "required" {
				continue
			}
			if dep.Project == "" && dep.Version == "" {
				return errors.New("неизвестная обязательная зависимость пака")
			}
			if dep.Project != "" {
				dependency, err := m.Client.Project(ctx, provider, dep.Project)
				if err != nil {
					return err
				}
				if dependency.Kind != kind {
					plan.Warnings = append(plan.Warnings, "Требуется мод «"+dependency.Name+"». Установи его в разделе модов.")
					continue
				}
			}
			if err = visit(dep.Project, dep.Version); err != nil {
				return err
			}
		}
		return nil
	}
	if err = visit(project, version); err != nil {
		return plan, err
	}
	if len(plan.Entries) == 0 {
		return plan, errors.New("тип проекта не совпадает с выбранной категорией")
	}
	var token [16]byte
	if _, err = rand.Read(token[:]); err != nil {
		return plan, err
	}
	plan.ID = hex.EncodeToString(token[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, old := range m.plans {
		if time.Since(old.Created) > 10*time.Minute {
			delete(m.plans, id)
		}
	}
	m.plans[plan.ID] = plan
	return plan, nil
}
func (m *Manager) installResources(ctx context.Context, plan Plan) error {
	dir := plan.Target.Directory
	folder, err := localFolder(dir, plan.Kind)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(folder, 0755); err != nil {
		return err
	}
	if err := localDirectory(folder); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(dir, ".sintet-pack-stage-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	records, registryName, err := localRegistry(dir, plan.Kind)
	if err != nil {
		return err
	}
	type pending struct {
		entry      PlanEntry
		path, hash string
	}
	files := []pending{}
	names := map[string]bool{}
	for i, entry := range plan.Entries {
		v := entry.Version
		if !safeContentFilename(v.Filename, plan.Kind) || names[strings.ToLower(v.Filename)] {
			return errors.New("неверное или повторяющееся имя ZIP")
		}
		names[strings.ToLower(v.Filename)] = true
		old := records[key(entry.Project.Provider, v.Project)]
		if old.Version == v.ID && zipPackInstalled(folder, plan.Kind, old) {
			continue
		}
		m.mu.Lock()
		m.state.Stage = "Скачивание " + entry.Project.Name
		m.mu.Unlock()
		path := filepath.Join(stage, v.Filename)
		sum, err := m.download(ctx, v, path, func(done, total int64) {
			m.mu.Lock()
			defer m.mu.Unlock()
			m.state.Percent = (float64(i) + float64(done)/float64(total)) * 95 / float64(len(plan.Entries))
			m.state.Done = i
		})
		if err != nil {
			return fmt.Errorf("%s: %w", entry.Project.Name, err)
		}
		files = append(files, pending{entry, path, sum})
	}
	if ctx.Err() != nil {
		return errors.New("установка отменена")
	}
	type backup struct{ from, to string }
	backups := []backup{}
	created := []string{}
	committed := false
	defer func() {
		if !committed {
			for _, path := range created {
				_ = os.Remove(path)
			}
			for i := len(backups) - 1; i >= 0; i-- {
				_ = os.Rename(backups[i].to, backups[i].from)
			}
		}
	}()
	for _, file := range files {
		v, p := file.entry.Version, file.entry.Project
		k := key(p.Provider, v.Project)
		if old, ok := records[k]; ok && safeContentFilename(old.Filename, plan.Kind) {
			oldPath := filepath.Join(folder, old.Filename)
			if _, err := os.Lstat(oldPath); os.IsNotExist(err) {
				oldPath += disabledSuffix
			}
			if _, err := os.Lstat(oldPath); err == nil {
				sum, e := fileHash(oldPath)
				if e != nil || sum != old.Hash {
					return errors.New("существующий пак изменён вручную; обновление остановлено")
				}
				to := filepath.Join(stage, fmt.Sprintf("backup-%d", len(backups)))
				if err = os.Rename(oldPath, to); err != nil {
					return err
				}
				backups = append(backups, backup{oldPath, to})
			}
		}
		dest := filepath.Join(folder, v.Filename)
		if _, err := os.Lstat(dest); err == nil {
			return fmt.Errorf("файл %s уже существует; он не будет перезаписан", v.Filename)
		} else if !os.IsNotExist(err) {
			return err
		}
		if err := localNameAvailable(dest + disabledSuffix); err != nil {
			return err
		}
		if err = os.Rename(file.path, dest); err != nil {
			return err
		}
		created = append(created, dest)
		records[k] = Installed{Provider: p.Provider, Project: v.Project, Version: v.ID, Filename: v.Filename, Name: p.Name, Hash: file.hash, Icon: p.Icon}
	}
	data, err := json.MarshalIndent(records, "", "  ")
	if err != nil {
		return err
	}
	if err = accountfiles.Write(filepath.Join(dir, registryName), data); err != nil {
		return err
	}
	committed = true
	return nil
}
