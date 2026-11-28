package workshop

import (
	"archive/zip"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

func isExtraContent(kind string) bool {
	return kind == "worlds" || kind == "datapacks" || kind == "plugins"
}

func extraContentProvider(kind string) string {
	if kind == "worlds" {
		return "curseforge"
	}
	return "modrinth"
}

func extraContentLoader(kind, platform string) (string, bool) {
	switch kind {
	case "worlds":
		return "", platform == ""
	case "datapacks":
		return "datapack", platform == ""
	case "plugins":
		return platform, contains([]string{"bukkit", "spigot", "paper", "folia", "purpur", "velocity", "bungeecord", "waterfall"}, platform)
	}
	return "", false
}

func (c *Client) ExtraContentVersions(ctx context.Context, provider, id, kind, game, platform string) ([]Version, error) {
	loader, ok := extraContentLoader(kind, platform)
	if !ok || provider != extraContentProvider(kind) || !gameVersion.MatchString(game) {
		return nil, errors.New("выберите версию Minecraft и подходящую платформу")
	}
	versions, err := c.Versions(ctx, provider, id, game, loader)
	if err != nil {
		return nil, err
	}
	matching := []Version{}
	for _, v := range versions {
		if v.Project == id && contains(v.Games, game) && (loader == "" || contains(v.platforms, loader)) && safeContentFilename(v.Filename, kind) {
			v.Kind = kind
			v.Unavailable = v.URL == "" || v.Hash == ""
			matching = append(matching, v)
		}
	}
	return matching, nil
}

func (m *Manager) PrepareExtraContent(ctx context.Context, provider, project, version, kind string, target Target, world string) (Plan, error) {
	plan := Plan{Kind: kind, Target: target, world: world, Created: time.Now()}
	platform := ""
	if kind == "plugins" {
		platform = target.Loader
	}
	loader, ok := extraContentLoader(kind, platform)
	if !ok || provider != extraContentProvider(kind) || !gameVersion.MatchString(target.Game) {
		return plan, errors.New("неподдерживаемая категория или платформа")
	}
	if kind != "plugins" {
		folder, err := extraContentFolder(plan)
		if err != nil {
			return plan, err
		}
		plan.Destination = folder
	}
	info, err := m.Client.Project(ctx, provider, project)
	if err != nil {
		return plan, err
	}
	if info.ID != project || info.Kind != kind && (loader == "" || !contains(info.Platforms, loader)) {
		return plan, errors.New("проект не подходит для этой категории")
	}
	v, err := m.Client.Version(ctx, provider, project, version)
	if err != nil {
		return plan, err
	}
	if v.Project != project || v.ID != version || !contains(v.Games, target.Game) ||
		loader != "" && !contains(v.platforms, loader) || !safeContentFilename(v.Filename, kind) {
		return plan, errors.New("файл не подходит для выбранной версии Minecraft или платформы")
	}
	if v.URL == "" || v.Hash == "" {
		return plan, errors.New("источник не предоставил файл для автоматического скачивания")
	}
	v.Kind = kind
	info.Kind = kind
	plan.Entries = []PlanEntry{{Project: info, Version: v}}
	for _, dependency := range v.Dependencies {
		if dependency.Kind == "required" {
			plan.Warnings = []string{"У файла есть обязательные зависимости. Перед запуском установи их по инструкции автора на странице проекта."}
			break
		}
	}
	var token [16]byte
	if _, err := rand.Read(token[:]); err != nil {
		return plan, err
	}
	plan.ID = hex.EncodeToString(token[:])
	m.mu.Lock()
	defer m.mu.Unlock()
	for id, pending := range m.plans {
		if time.Since(pending.Created) > 10*time.Minute {
			delete(m.plans, id)
		}
	}
	m.plans[plan.ID] = plan
	return plan, nil
}

func ordinaryContentPath(name string, directory bool) error {
	info, err := os.Lstat(name)
	if err != nil {
		return err
	}
	wide, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	attributes, err := windows.GetFileAttributes(wide)
	if err != nil {
		return err
	}
	if attributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 ||
		(directory && !info.IsDir()) || (!directory && !info.Mode().IsRegular()) {
		return errors.New("нужна обычная папка или файл Minecraft")
	}
	return nil
}

func validWorldName(name string) bool {
	return safePackRelative(name) && !strings.Contains(name, "/")
}

func localWorldPath(saves, world string) (string, error) {
	if !validWorldName(world) {
		return "", errors.New("выберите существующий мир")
	}
	if err := ordinaryContentPath(saves, true); err != nil {
		return "", err
	}
	dir := filepath.Join(saves, world)
	if err := ordinaryContentPath(dir, true); err != nil {
		return "", err
	}
	return dir, ordinaryContentPath(filepath.Join(dir, "level.dat"), false)
}

func LocalWorlds(instance string) ([]string, error) {
	worlds := []string{}
	if err := ordinaryContentPath(instance, true); err != nil {
		return worlds, err
	}
	saves := filepath.Join(instance, "saves")
	if err := ordinaryContentPath(saves, true); err != nil {
		if os.IsNotExist(err) {
			return worlds, nil
		}
		return worlds, err
	}
	children, err := os.ReadDir(saves)
	if err != nil {
		return worlds, err
	}
	for _, child := range children {
		if child.IsDir() {
			if _, err := localWorldPath(saves, child.Name()); err == nil {
				worlds = append(worlds, child.Name())
			}
		}
	}
	return worlds, nil
}

func extraContentFolder(plan Plan) (string, error) {
	root := plan.Target.Directory
	if !filepath.IsAbs(root) {
		return "", errors.New("выберите папку установки")
	}
	if err := ordinaryContentPath(root, true); err != nil {
		return "", err
	}
	switch plan.Kind {
	case "worlds":
		return filepath.Join(root, "saves"), nil
	case "datapacks":
		world, err := localWorldPath(filepath.Join(root, "saves"), plan.world)
		if err != nil {
			return "", errors.New("выберите существующий локальный мир Minecraft")
		}
		return filepath.Join(world, "datapacks"), nil
	case "plugins":
		return filepath.Join(root, "plugins"), nil
	}
	return "", errors.New("неизвестный тип контента")
}

func (m *Manager) installExtraContent(ctx context.Context, plan Plan) error {
	folder, err := extraContentFolder(plan)
	if err != nil {
		return err
	}
	if err := os.Mkdir(folder, 0755); err != nil && !os.IsExist(err) {
		return err
	}
	if err := ordinaryContentPath(folder, true); err != nil {
		return err
	}
	stage, err := os.MkdirTemp(folder, ".sintet-content-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	entry := plan.Entries[0]
	v := entry.Version
	m.mu.Lock()
	m.state.Stage = "Скачивание " + entry.Project.Name
	m.mu.Unlock()
	file := filepath.Join(stage, v.Filename)
	if _, err := m.download(ctx, v, file, func(done, total int64) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.state.Percent = 85 * float64(done) / float64(total)
	}); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if plan.Kind == "worlds" {
		m.mu.Lock()
		m.state.Stage = "Распаковка мира"
		m.mu.Unlock()
		return installWorldArchive(ctx, file, stage, folder, strings.TrimSuffix(v.Filename, filepath.Ext(v.Filename)))
	}
	if err := validateExtraArchive(file, plan.Kind); err != nil {
		return err
	}
	return moveLocalFile(file, filepath.Join(folder, v.Filename))
}

func validateExtraArchive(filename, kind string) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	if kind == "plugins" {
		for _, name := range []string{"plugin.yml", "paper-plugin.yml", "bungee.yml", "velocity-plugin.json"} {
			if data, err := zipBytes(&archive.Reader, name, 1<<20); err == nil && len(data) > 0 {
				return nil
			}
		}
		return errors.New("в JAR нет описания серверного плагина")
	}
	data, err := zipBytes(&archive.Reader, "pack.mcmeta", 1<<20)
	var meta struct{ Pack map[string]json.RawMessage }
	if err != nil || json.Unmarshal(data, &meta) != nil || len(meta.Pack) == 0 {
		return errors.New("в ZIP нет корректного pack.mcmeta")
	}
	for _, file := range archive.File {
		if file.Mode().IsRegular() && safePackRelative(file.Name) && strings.HasPrefix(file.Name, "data/") {
			return nil
		}
	}
	return errors.New("в ZIP нет данных Minecraft")
}

func installWorldArchive(ctx context.Context, filename, stage, saves, name string) error {
	archive, err := zip.OpenReader(filename)
	if err != nil {
		return err
	}
	defer archive.Close()
	if len(archive.File) > 20000 {
		return errors.New("в архиве карты слишком много файлов")
	}
	root, depth, matches := "", 20001, 0
	for _, file := range archive.File {
		if !file.Mode().IsRegular() || !safePackRelative(file.Name) || path.Base(file.Name) != "level.dat" || strings.HasPrefix(file.Name, "__MACOSX/") {
			continue
		}
		d := strings.Count(file.Name, "/")
		if d < depth {
			root, depth, matches = strings.TrimSuffix(file.Name, "level.dat"), d, 0
		}
		if d == depth {
			matches++
		}
	}
	if matches != 1 {
		return errors.New("в ZIP должен быть один мир Minecraft Java с level.dat")
	}
	world := filepath.Join(stage, "extracted")
	if err := os.Mkdir(world, 0755); err != nil {
		return err
	}
	var expanded uint64
	seen := map[string]bool{}
	for _, file := range archive.File {
		if err := ctx.Err(); err != nil {
			return err
		}
		relative, included := strings.CutPrefix(file.Name, root)
		relative = strings.TrimSuffix(relative, "/")
		if !included || relative == "" || relative == "session.lock" || strings.HasPrefix(relative, "__MACOSX/") {
			continue
		}
		key := strings.ToLower(relative)
		if !safePackPath("world/"+relative) || seen[key] || file.Flags&1 != 0 ||
			(!file.Mode().IsDir() && !file.Mode().IsRegular()) {
			return errors.New("недопустимые файлы или пути в ZIP карты")
		}
		seen[key] = true
		if file.Mode().IsDir() {
			continue
		}
		if file.UncompressedSize64 > 1<<30 || file.UncompressedSize64 > (8<<30)-expanded {
			return errors.New("распакованная карта превышает лимит размера")
		}
		expanded += file.UncompressedSize64
		if err := extractWorldFile(ctx, file, filepath.Join(world, filepath.FromSlash(relative))); err != nil {
			return err
		}
	}
	for i := 1; i <= 100; i++ {
		folder := name
		if i > 1 {
			folder = fmt.Sprintf("%s (%d)", name, i)
		}
		destination := filepath.Join(saves, folder)
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := moveLocalFile(world, destination); err == nil {
			return nil
		} else if _, statErr := os.Lstat(destination); statErr != nil {
			return err
		}
	}
	return errors.New("не удалось подобрать свободное имя папки мира")
}

func extractWorldFile(ctx context.Context, file *zip.File, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0755); err != nil {
		return err
	}
	src, err := file.Open()
	if err != nil {
		return err
	}
	defer src.Close()
	out, err := os.OpenFile(destination, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	length, err := io.Copy(out, io.LimitReader(contextArchiveReader{ctx: ctx, Reader: src}, int64(file.UncompressedSize64)+1))
	closeErr := out.Close()
	if err != nil {
		return err
	}
	if uint64(length) != file.UncompressedSize64 {
		return errors.New("некорректный размер файла в архиве карты")
	}
	return closeErr
}
