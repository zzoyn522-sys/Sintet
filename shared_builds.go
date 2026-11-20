package main

import (
	"archive/zip"
	"bytes"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"io"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sintet/internal/packsync"
	"sintet/internal/workshop"
)

const sharedProfilePrefix = "build-shared-"

var sharedModKey = regexp.MustCompile("^[a-z0-9_.:-]{1,140}$")
var sharedDigest = regexp.MustCompile("^[a-f0-9]{64}$")
var sharedMemberQuery = regexp.MustCompile("^@?[a-zA-Z0-9_-]{2,36}$")

type SharedBuild struct {
	ID                  string                `json:"id"`
	Name                string                `json:"name"`
	Game                string                `json:"game"`
	Loader              string                `json:"loader"`
	LoaderVersion       string                `json:"loader_version"`
	OwnerID             string                `json:"owner_id"`
	OwnerName           string                `json:"owner_name"`
	OwnerAvatar         string                `json:"ownerAvatar,omitempty"`
	OwnerDigest         string                `json:"ownerDigest,omitempty"`
	Role                string                `json:"role"`
	DraftVersion        int                   `json:"draft_version"`
	CurrentRevision     int                   `json:"current_revision"`
	SintedotAvailable   bool                  `json:"sintedot_available,omitempty"`
	SintedotCanRollback bool                  `json:"sintedot_can_rollback,omitempty"`
	CanRollback         bool                  `json:"can_rollback,omitempty"`
	UpdatedAt           time.Time             `json:"updated_at"`
	OriginProfile       string                `json:"origin_profile,omitempty"`
	OwnerOnline         bool                  `json:"owner_online"`
	AccessOpen          bool                  `json:"access_open"`
	LocalProfile        *WorkshopBuild        `json:"localProfile,omitempty"`
	OfflineReason       string                `json:"offlineReason,omitempty"`
	Mods                []sharedBuildMod      `json:"mods"`
	Members             []sharedBuildMember   `json:"members"`
	Revisions           []sharedBuildRevision `json:"revisions"`
}

type sharedBuildMod struct {
	Key          string            `json:"key"`
	File         packsync.File     `json:"file"`
	Author       string            `json:"author,omitempty"`
	AuthorName   string            `json:"authorName,omitempty"`
	Enabled      bool              `json:"enabled,omitempty"`
	Approved     bool              `json:"approved,omitempty"`
	Metadata     sharedModMetadata `json:"metadata,omitempty"`
	Available    bool              `json:"available,omitempty"`
	AuthorAvatar string            `json:"authorAvatar,omitempty"`
	AuthorDigest string            `json:"authorDigest,omitempty"`
}

type sharedModMetadata struct {
	Name     string `json:"name,omitempty"`
	Version  string `json:"version,omitempty"`
	Icon     string `json:"icon,omitempty"`
	Provider string `json:"provider,omitempty"`
	Project  string `json:"project,omitempty"`
}

type sharedBuildMember struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	Handle       string `json:"handle"`
	Accepted     bool   `json:"accepted"`
	AvatarDigest string `json:"avatarDigest,omitempty"`
	AvatarURL    string `json:"avatarUrl,omitempty"`
}

type sharedBuildRevision struct {
	Number    int       `json:"number"`
	Count     int       `json:"count"`
	CreatedAt time.Time `json:"created_at"`
}

type SharedBuildProgress struct {
	Stage string `json:"stage"`
	File  string `json:"file,omitempty"`
	Done  int64  `json:"done"`
	Total int64  `json:"total"`
}

type sharedBuildManager struct {
	app       *App
	ctx       context.Context
	operation sync.Mutex
	indexMu   sync.Mutex
	mu        sync.Mutex
	progress  map[string]SharedBuildProgress
	uploads   map[string]struct{}
	streams   chan struct{}
	metadata  map[string]sharedModMetadata
	hostID    string
	host      sharedBuildSession
	hostMu    sync.Mutex
	hostEnded bool
}

type sharedBuildSession struct {
	account string
	client  *identity.Client
	social  *socialManager
}

func newSharedBuildManager(app *App, ctx context.Context) *sharedBuildManager {
	return &sharedBuildManager{app: app, ctx: ctx, progress: make(map[string]SharedBuildProgress), uploads: make(map[string]struct{}), streams: make(chan struct{}, 2), metadata: make(map[string]sharedModMetadata), hostID: uuid.NewString()}
}

func (m *sharedBuildManager) session() (sharedBuildSession, error) {
	social := m.app.social
	m.app.authMu.Lock()
	slot := m.app.profiles.find(m.app.profiles.active)
	if slot != nil && slot.sintet != nil {
		client := slot.sintet
		m.app.authMu.Unlock()
		if account := client.SavedAccount(); account != nil && sharedUUID(account.ID) {
			return sharedBuildSession{account: account.ID, client: client, social: social}, nil
		}
		return sharedBuildSession{}, errors.New("войдите через Sintet ID")
	}
	m.app.authMu.Unlock()
	if social != nil {
		social.identity()
		social.mu.Lock()
		client := social.client
		social.mu.Unlock()
		if client != nil {
			if account := client.SavedAccount(); account != nil && sharedUUID(account.ID) {
				return sharedBuildSession{account: account.ID, client: client, social: social}, nil
			}
		}
	}
	return sharedBuildSession{}, errors.New("ожидаем подключение профиля к сервису сборок")
}

func sharedUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func (s sharedBuildSession) request(ctx context.Context, action, build string, input map[string]any, result any) error {
	if build != "" && !sharedUUID(build) {
		return errors.New("неверный идентификатор сборки")
	}
	request := make(map[string]any, len(input)+2)
	for key, value := range input {
		request[key] = value
	}
	request["action"], request["build"] = action, build
	call, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()
	return s.client.SharedBuildRequest(call, s.account, request, result)
}

func (a *App) SharedBuilds() ([]SharedBuild, error) {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return nil, nil
	}
	var builds []SharedBuild
	index := filepath.Join(m.buildRoot(s), "builds.json")
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	if err = s.request(m.ctx, "list", "", nil, &builds); err != nil {
		if !identity.TemporaryServiceError(err) {
			return nil, err
		}
		data, e := os.ReadFile(index)
		if e != nil || json.Unmarshal(data, &builds) != nil {
			return nil, err
		}
		for i := range builds {
			builds[i].OwnerOnline = false
			builds[i].OfflineReason = "Сервис общих сборок недоступен"
		}
	} else {
		m.bindLegacyOrigins(s, builds)
		saveSharedSnapshot(index, builds)
	}
	for i := range builds {
		m.attachLocalProfile(s, &builds[i])
	}
	return builds, nil
}

func (a *App) SharedBuildDetail(id string) (SharedBuild, error) {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return SharedBuild{}, err
	}
	build, err := m.build(s, "detail", id, nil)
	if err != nil {
		if !identity.TemporaryServiceError(err) || !sharedUUID(id) {
			return build, err
		}
		data, e := os.ReadFile(filepath.Join(m.buildRoot(s), id, "detail.json"))
		if e != nil || json.Unmarshal(data, &build) != nil || build.ID != id {
			return build, err
		}
		build.OwnerOnline = false
		build.OfflineReason = "Сервис общих сборок недоступен"
	} else {
		if m.operation.TryLock() {
			build, err = m.recoverWorkshopMods(s, build)
			m.operation.Unlock()
			if err != nil {
				return build, err
			}
		}
		saveSharedSnapshot(filepath.Join(m.buildRoot(s), id, "detail.json"), build)
	}
	m.attachLocalProfile(s, &build)
	build.OwnerAvatar = m.avatar(build.OwnerID, build.OwnerDigest)
	for i := range build.Members {
		person := &build.Members[i]
		person.AvatarURL = m.avatar(person.ID, person.AvatarDigest)
	}
	for i := range build.Mods {
		mod := &build.Mods[i]
		mod.AuthorAvatar = m.avatar(mod.Author, mod.AuthorDigest)
		// Opening the window reads cached file metadata. Hashes are checked when
		// downloading, approving and materializing a revision.
		if !sharedDigest.MatchString(mod.File.SHA256) {
			continue
		}
		stat, e := os.Lstat(filepath.Join(m.cache(s, id), mod.File.SHA256))
		mod.Available = e == nil && stat.Mode().IsRegular() && stat.Size() == mod.File.Size
		if mod.Available && (mod.Metadata.Name == "" || mod.Metadata.Icon == "") {
			m.mu.Lock()
			meta, cached := m.metadata[mod.File.SHA256]
			m.mu.Unlock()
			if !cached {
				info := workshop.ModMetadata(filepath.Join(m.cache(s, id), mod.File.SHA256))
				meta = sharedModMetadata{Name: info.Name, Version: info.Version}
				if png, e := workshop.ModIcon(filepath.Join(m.cache(s, id), mod.File.SHA256)); e == nil {
					meta.Icon = m.saveSharedModIcon(png)
				}
				m.mu.Lock()
				if len(m.metadata) >= 2000 {
					clear(m.metadata)
				}
				m.metadata[mod.File.SHA256] = meta
				m.mu.Unlock()
			}
			if mod.Metadata.Name == "" {
				mod.Metadata.Name = meta.Name
			}
			if mod.Metadata.Version == "" {
				mod.Metadata.Version = meta.Version
			}
			if mod.Metadata.Icon == "" {
				mod.Metadata.Icon = meta.Icon
			}
		}
	}
	return build, nil
}

func (m *sharedBuildManager) saveSharedModIcon(data []byte) string {
	hash := sha256.Sum256(data)
	name := hex.EncodeToString(hash[:]) + ".png"
	dest := filepath.Join(m.app.profiles.root, "WorkshopIcons", name)
	if _, err := os.Stat(dest); err != nil {
		if err := accountfiles.Write(dest, data); err != nil {
			return ""
		}
	}
	return "/workshop-icons/" + name
}

func (a *App) SharedBuildModIcons(id string, hashes []string) (map[string]string, error) {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return nil, err
	}
	if len(hashes) > 18 || !sharedUUID(id) {
		return nil, errors.New("некорректный запрос иконок")
	}
	build, err := m.build(s, "detail", id, nil)
	if err != nil {
		return nil, err
	}
	if !build.AccessOpen || !build.OwnerOnline {
		return nil, nil
	}
	const maxIcon = 512 << 10
	icons := make(map[string]string)
	var mods []sharedBuildMod
	seen := make(map[string]bool)
	for _, mod := range build.Mods {
		hash := mod.File.SHA256
		if mod.Metadata.Icon == "" && sharedDigest.MatchString(hash) && slices.Contains(hashes, hash) && !seen[hash] {
			seen[hash] = true
			mods = append(mods, mod)
		}
	}
	cache := m.cache(s, id)
	fetcher := newSharedBuildFetcher(s, build, mods, cache)
	defer fetcher.close()
	ctx, cancel := context.WithTimeout(m.ctx, 35*time.Second)
	defer cancel()
	for _, mod := range mods {
		hash := mod.File.SHA256
		data, e := workshop.ModIcon(filepath.Join(cache, hash))
		if e != nil {
			res, e := fetcher.PackGet(ctx, "/icon/"+hash)
			if e != nil {
				continue
			}
			data, e = io.ReadAll(io.LimitReader(res.Body, maxIcon+1))
			res.Body.Close()
			if e != nil || res.StatusCode != http.StatusOK || len(data) > maxIcon {
				continue
			}
			config, e := png.DecodeConfig(bytes.NewReader(data))
			if e != nil || config.Width > 1024 || config.Height > 1024 {
				continue
			}
		}
		if icon := m.saveSharedModIcon(data); icon != "" {
			icons[hash] = icon
		}
	}
	return icons, nil
}

func saveSharedSnapshot(file string, value any) {
	data, err := json.Marshal(value)
	if err == nil {
		if previous, e := os.ReadFile(file); e != nil || string(previous) != string(data) {
			_ = accountfiles.Write(file, data)
		}
	}
}

func (m *sharedBuildManager) buildRoot(s sharedBuildSession) string {
	return filepath.Join(m.app.profiles.root, "SharedBuilds", s.account)
}

func (a *App) SharedBuildSearch(query string) ([]sharedBuildMember, error) {
	query = strings.TrimSpace(query)
	if !sharedMemberQuery.MatchString(query) {
		return nil, nil
	}
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return nil, err
	}
	var people []sharedBuildMember
	err = s.request(m.ctx, "search", "", map[string]any{"query": query}, &people)
	for i := range people {
		people[i].AvatarURL = m.avatar(people[i].ID, people[i].AvatarDigest)
	}
	return people, err
}

func (m *sharedBuildManager) avatar(id, digest string) string {
	if m.app.avatarDB == nil || !avatarAccountID.MatchString(id) || !avatarDigest.MatchString(digest) {
		return ""
	}
	return strings.TrimRight(m.app.avatarDB.ImageOrigin, "/") + "/avatars/" + id + "/" + digest + ".png"
}

func (m *sharedBuildManager) build(s sharedBuildSession, action, id string, input map[string]any) (SharedBuild, error) {
	var build SharedBuild
	err := s.request(m.ctx, action, id, input, &build)
	if err == nil && (!sharedUUID(id) || build.ID != id || !sharedUUID(build.OwnerID) ||
		(build.Role != "owner" && build.Role != "editor")) {
		err = errors.New("некорректные данные общей сборки")
	}
	return build, err
}

func (a *App) SharedBuildChange(id, action, key string, version int, enabled bool) error {
	if action == "delete" {
		return a.DeleteSharedWorkshopBuild(id)
	}
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	var fields map[string]any
	switch action {
	case "accept", "decline", "leave":
	case "invite", "revoke":
		fields = map[string]any{"member": key}
	case "access":
		fields = map[string]any{"enabled": enabled}
	case "rename":
		name, err := normalizeWorkshopBuildName(key)
		if err != nil {
			return err
		}
		fields = map[string]any{"name": name}
	case "toggle", "remove":
		fields = map[string]any{"key": key, "version": version, "enabled": enabled}
	default:
		return errors.New("неизвестное действие")
	}
	err = s.request(m.ctx, action, id, fields, nil)
	if err == nil && (action == "leave" || action == "decline") {
		m.forgetSharedBuild(s, id)
	}
	if err == nil && action == "access" && enabled {
		m.refreshHost()
	}
	return err
}

func (a *App) DeleteSharedWorkshopBuild(id string) error {
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
	if build.Role != "owner" || build.OwnerID != s.account {
		return errors.New("удалить общую сборку может только лидер")
	}
	// The server owns the membership. Do not remove local files if it refuses deletion.
	removed := false
	removeShared := func() error {
		err := s.request(m.ctx, "delete", id, nil, nil)
		removed = err == nil
		return err
	}
	localID := m.localProfileID(s, build)
	if _, exists := a.workshopBuildByID(localID); exists {
		err = a.deleteWorkshopBuild(localID, removeShared)
	} else {
		err = removeShared()
	}
	if removed {
		m.forgetSharedBuild(s, id)
		if err != nil {
			return fmt.Errorf("общая сборка удалена, но не удалось удалить локальные файлы: %w", err)
		}
	}
	return err
}

func (m *sharedBuildManager) forgetSharedBuild(s sharedBuildSession, id string) {
	m.indexMu.Lock()
	defer m.indexMu.Unlock()
	root := m.buildRoot(s)
	index := filepath.Join(root, "builds.json")
	_ = os.Remove(filepath.Join(root, id, "detail.json"))
	data, err := os.ReadFile(index)
	var builds []SharedBuild
	if err == nil && json.Unmarshal(data, &builds) == nil {
		builds = slices.DeleteFunc(builds, func(build SharedBuild) bool { return build.ID == id })
		saveSharedSnapshot(index, builds)
	}
}

func (a *App) SharedBuildBind(id, localID string) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	build, err := m.build(s, "detail", id, nil)
	if err != nil {
		return err
	}
	local, ok := a.workshopBuildByID(localID)
	if build.Role != "owner" || !ok || !shareableWorkshopProfile(localID) || !sharedSameBase(build, local) {
		return errors.New("выбери исходную сборку с той же версией Minecraft и загрузчика")
	}
	if err := s.request(m.ctx, "bind", id, map[string]any{"origin": localID}, nil); err != nil {
		return err
	}
	_, _ = a.SharedBuilds()
	return nil
}

func (a *App) SharedBuildCreate(localID, name string) (string, error) {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return "", err
	}
	name, err = normalizeWorkshopBuildName(name)
	if err != nil {
		return "", err
	}
	if !m.operation.TryLock() {
		return "", errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	a.mu.RLock()
	service, mode := a.service, a.settings.InstanceMode
	a.mu.RUnlock()
	profile, err := service.ProfileIdentity(localID)
	if err != nil || !shareableWorkshopProfile(localID) || !slices.Contains([]string{"fabric", "forge", "neoforge"}, profile.Loader) {
		return "", errors.New("выбери установленную сборку Fabric, Forge или NeoForge")
	}
	directory, err := service.ProfileDirectory(localID, mode)
	if err != nil {
		return "", err
	}
	files, err := listSharedMods(directory)
	if err != nil {
		return "", err
	}
	id := uuid.NewString()
	build := SharedBuild{ID: id, Name: name, Game: profile.Base, Loader: profile.Loader, LoaderVersion: profile.LoaderVersion}
	input := map[string]any{"name": name, "game": build.Game, "loader": build.Loader, "loaderVersion": build.LoaderVersion, "origin": localID}
	if len(files) > 0 {
		mods, err := m.stageMods(s, build, files, nil)
		if err != nil {
			return "", err
		}
		input["mods"] = mods
	}
	if err := s.request(m.ctx, "create", id, input, nil); err != nil {
		return "", err
	}
	_ = os.MkdirAll(m.buildRoot(s), 0700)
	_, _ = a.SharedBuilds()
	go m.refreshHost()
	return id, nil
}

func listSharedMods(directory string) ([]string, error) {
	dir := filepath.Join(directory, "mods")
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("папка модов должна быть обычной папкой")
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if strings.EqualFold(filepath.Ext(entry.Name()), ".jar") {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files, nil
}

func (a *App) SharedBuildPickMods(id string, version int) (int, error) {
	files, err := runtime.OpenMultipleFilesDialog(a.ctx, runtime.OpenDialogOptions{
		Title:   "Добавить моды в общую сборку",
		Filters: []runtime.FileFilter{{DisplayName: "Моды Minecraft", Pattern: "*.jar"}},
	})
	if err != nil || len(files) == 0 {
		return 0, err
	}
	return a.SharedBuildAddFiles(id, version, files)
}

func (a *App) SharedBuildAddFiles(id string, version int, files []string) (int, error) {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil || len(files) == 0 {
		return 0, err
	}
	if !m.operation.TryLock() {
		return 0, errors.New("дождись окончания операции со сборкой")
	}
	defer m.operation.Unlock()
	build, err := m.editingBuild(s, id, version)
	if err != nil {
		return 0, err
	}
	mods, err := m.stageMods(s, build, files, nil)
	if err != nil {
		return 0, err
	}
	return len(mods), s.request(m.ctx, "add", id, map[string]any{"version": version, "mods": mods}, nil)
}

func (m *sharedBuildManager) editingBuild(s sharedBuildSession, id string, version int) (SharedBuild, error) {
	build, err := m.build(s, "detail", id, nil)
	if err == nil && build.Role != "owner" {
		switch {
		case !build.AccessOpen:
			err = errors.New("лидер закрыл общий доступ")
		case !build.OwnerOnline:
			err = errors.New("лидер сборки офлайн")
		}
	}
	if err == nil && build.DraftVersion != version {
		err = errors.New("сборка обновилась; проверь изменения и повтори")
	}
	return build, err
}

func (m *sharedBuildManager) cache(s sharedBuildSession, build string) string {
	return filepath.Join(m.buildRoot(s), build, "objects")
}

func (m *sharedBuildManager) setProgress(id string, value SharedBuildProgress) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if value.Stage == "" {
		delete(m.progress, id)
	} else {
		m.progress[id] = value
	}
}

func (a *App) SharedBuildStatus(id string) SharedBuildProgress {
	m := a.sharedBuilds
	m.mu.Lock()
	value := m.progress[id]
	m.mu.Unlock()
	return a.sharedLauncherProgress(value)
}

func (m *sharedBuildManager) stageMods(s sharedBuildSession, build SharedBuild, paths []string, sources map[string]packsync.Source) ([]sharedBuildMod, error) {
	if len(paths) > 1000 {
		return nil, errors.New("в сборке может быть до 1000 модов")
	}
	mods := make([]sharedBuildMod, 0, len(paths))
	seen := make(map[string]bool)
	var total int64
	for _, filename := range paths {
		file, key, err := cacheSharedMod(m.ctx, m.cache(s, build.ID), filename)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(filename), err)
		}
		if source, ok := sources[file.Path]; ok {
			file.Sources = []packsync.Source{source}
		}
		if seen[key] {
			return nil, fmt.Errorf("выбрано несколько файлов мода %s", key)
		}
		seen[key] = true
		total += file.Size
		if total > packsync.MaxTotal {
			return nil, errors.New("файлы сборки превышают 2 ГБ")
		}
		info := workshop.ModMetadata(filepath.Join(m.cache(s, build.ID), file.SHA256))
		mods = append(mods, sharedBuildMod{Key: key, File: file, Metadata: sharedModMetadata{Name: info.Name, Version: info.Version}})
	}
	return mods, nil
}

func cacheSharedMod(ctx context.Context, cache, filename string) (packsync.File, string, error) {
	relative := "mods/" + filepath.Base(filename)
	if !packsync.AllowedPath(relative) || strings.HasPrefix(path.Base(relative), ".") || strings.HasPrefix(path.Base(relative), " ") {
		return packsync.File{}, "", errors.New("недопустимое имя JAR")
	}
	info, err := os.Lstat(filename)
	if err != nil {
		return packsync.File{}, "", err
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > packsync.MaxMod {
		return packsync.File{}, "", errors.New("нужен обычный JAR размером до 256 МБ")
	}
	input, err := os.Open(filename)
	if err != nil {
		return packsync.File{}, "", err
	}
	defer input.Close()
	if err := os.MkdirAll(cache, 0700); err != nil {
		return packsync.File{}, "", err
	}
	out, err := os.CreateTemp(cache, "incoming-*.tmp")
	if err != nil {
		return packsync.File{}, "", err
	}
	defer os.Remove(out.Name())
	h, old := sha256.New(), sha1.New()
	n, copyErr := io.Copy(io.MultiWriter(out, h, old), io.LimitReader(sharedReader{ctx, input}, packsync.MaxMod+1))
	if copyErr == nil {
		copyErr = out.Sync()
	}
	closeErr := out.Close()
	if err := errors.Join(copyErr, closeErr); err != nil {
		return packsync.File{}, "", err
	}
	if n != info.Size() {
		return packsync.File{}, "", errors.New("JAR изменился во время чтения")
	}
	if err := validateSharedJar(out.Name()); err != nil {
		return packsync.File{}, "", err
	}
	file := packsync.File{Path: relative, Size: n, SHA256: hex.EncodeToString(h.Sum(nil)), SHA1: hex.EncodeToString(old.Sum(nil))}
	file.CurseFingerprint, err = packsync.CurseFingerprint(ctx, out.Name())
	if err != nil {
		return packsync.File{}, "", err
	}
	key := "file:" + file.SHA256
	for _, id := range workshop.ModIDs(out.Name()) {
		if candidate := "mod:" + strings.ToLower(id); sharedModKey.MatchString(candidate) {
			key = candidate
			break
		}
	}
	dest := filepath.Join(cache, file.SHA256)
	if err := os.Rename(out.Name(), dest); err != nil && !packsync.ValidCached(cache, file) {
		return packsync.File{}, "", err
	}
	return file, key, nil
}

type sharedReader struct {
	context.Context
	io.Reader
}

func (r sharedReader) Read(p []byte) (int, error) {
	if err := r.Err(); err != nil {
		return 0, err
	}
	return r.Reader.Read(p)
}

func validateSharedJar(filename string) error {
	jar, err := zip.OpenReader(filename)
	if err != nil {
		return errors.New("JAR повреждён")
	}
	defer jar.Close()
	if len(jar.File) == 0 || len(jar.File) > 100000 {
		return errors.New("некорректное содержимое JAR")
	}
	for _, file := range jar.File {
		entry := strings.TrimSuffix(file.Name, "/")
		if !filepath.IsLocal(filepath.FromSlash(entry)) || strings.ContainsAny(entry, "\\:\x00") ||
			path.Clean(entry) != entry || file.Mode()&os.ModeSymlink != 0 || file.Flags&1 != 0 {
			return errors.New("JAR содержит недопустимую запись")
		}
	}
	return nil
}

func (a *App) SharedBuildAddCatalog(id string, version int, provider, project, release string) error {
	m := a.sharedBuilds
	s, err := m.session()
	if err != nil {
		return err
	}
	build, err := m.editingBuild(s, id, version)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(m.ctx, 12*time.Minute)
	defer cancel()
	catalog := workshop.NewManager(a.workshop.Client)
	plan, err := m.prepareWorkshopMod(ctx, catalog, build, sharedWorkshopPrefix+id, provider, project, release)
	if err != nil {
		return err
	}
	if err := catalog.StartWithCommit(ctx, plan.ID, true, func(ctx context.Context, plan workshop.Plan, directory string) error {
		return m.commitWorkshopMods(s, ctx, plan, directory)
	}); err != nil {
		return err
	}
	defer m.setProgress(id, SharedBuildProgress{})
	ticker := time.NewTicker(200 * time.Millisecond)
	defer ticker.Stop()
	for {
		state := catalog.State()
		if !state.Busy {
			if state.Error != "" {
				return errors.New(state.Error)
			}
			return nil
		}
		m.setProgress(id, SharedBuildProgress{Stage: state.Stage, Done: int64(state.Percent), Total: 100})
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}
