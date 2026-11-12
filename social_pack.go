package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"sintet/internal/accountfiles"
	"sintet/internal/launcher"
	"sintet/internal/packsync"
	p2p "sintet/internal/social"
)

type packStatus struct {
	Phase         string `json:"phase"`
	Name          string `json:"name"`
	Game          string `json:"game"`
	Loader        string `json:"loader"`
	LoaderVersion string `json:"loaderVersion"`
	Digest        string `json:"digest"`
	Missing       int    `json:"missing"`
	Bytes         int64  `json:"bytes"`
	Done          int64  `json:"done"`
	Total         int64  `json:"total"`
	Current       string `json:"current"`
	Restart       bool   `json:"restart"`
	NeedsGame     bool   `json:"needsGame"`
	Error         string `json:"error"`
	Profile       string `json:"profile,omitempty"`
}
type guestPack struct {
	manifest   packsync.Manifest
	peer       *p2p.Peer
	epoch      uint64
	host       string
	profile    string
	local      map[string]string
	fromInvite bool
}

const lobbyDiskFullMessage = "Не хватает места для сборки лобби. Освободи место или перенеси папку Minecraft на другой диск."

func (m *socialManager) packCache() string {
	m.app.mu.RLock()
	gameRoot := m.app.settings.GameDirectory
	m.app.mu.RUnlock()
	appRoot := m.app.profiles.root
	if gameRoot != "" && !strings.EqualFold(filepath.VolumeName(gameRoot), filepath.VolumeName(appRoot)) {
		return filepath.Join(gameRoot, ".sintet", "p2p", "objects")
	}
	return filepath.Join(appRoot, "P2P", "Temp", "objects")
}
func packMeta(profile launcher.InstalledProfile, name string) packsync.Manifest {
	return packsync.Manifest{Name: name, Game: profile.Base, Loader: profile.Loader, LoaderVersion: profile.LoaderVersion}
}

func (m *socialManager) captureHostPack(ctx context.Context, profiles ...string) (packsync.Snapshot, error) {
	m.app.contentMu.Lock()
	defer m.app.contentMu.Unlock()
	m.app.mu.RLock()
	service := m.app.service
	mode := m.app.settings.InstanceMode
	m.app.mu.RUnlock()
	state := service.State()
	profile := state.Profile
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	info, err := service.ProfileIdentity(profile)
	if err != nil {
		return packsync.Snapshot{}, err
	}
	directory := ""
	if state.Running && state.Profile == profile {
		directory = state.Directory
	}
	if directory == "" {
		directory, err = service.ProfileDirectory(profile, mode)
		if err != nil {
			return packsync.Snapshot{}, err
		}
	}
	name := "Minecraft " + info.Base
	if build, ok := m.app.workshopBuildByID(profile); ok {
		name = build.Name
	}
	snapshot, err := packsync.Scan(ctx, directory, packMeta(info, name), m.packCache())
	if err != nil {
		return snapshot, err
	}
	if m.app.workshop != nil {
		client := m.app.workshop.Client
		snapshot.Manifest = packsync.AddSourceHints(ctx, snapshot.Manifest, []packsync.Resolver{packsync.CurseForgeResolver{HTTP: client.HTTP, BaseURL: client.CurseForge, Key: client.Key}})
	}
	return snapshot, nil
}
func (m *socialManager) prepareGuestPack(ctx context.Context, peer *p2p.Peer, epoch uint64, host socialAccount, fromInvite bool) error {
	m.mu.Lock()
	if m.epoch != epoch || !peer.Connected() {
		m.mu.Unlock()
		return context.Canceled
	}
	if m.packChecking {
		m.mu.Unlock()
		return nil
	}
	m.packChecking = true
	m.guestPack = nil
	m.state.Pack = &packStatus{Phase: "checking", Name: host.Username}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		if m.epoch == epoch {
			m.packChecking = false
		}
		m.mu.Unlock()
	}()
	check, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	manifest, err := readLobbyManifest(check, peer)
	if err != nil {
		m.logError(fmt.Errorf("P2P: чтение манифеста сборки: %w", err))
		m.packFailure(epoch, lobbyManifestError(err))
		return nil
	}
	plan := &guestPack{manifest: manifest, peer: peer, epoch: epoch, host: host.ID, local: map[string]string{}, fromInvite: fromInvite}
	m.app.mu.RLock()
	service := m.app.service
	mode := m.app.settings.InstanceMode
	m.app.mu.RUnlock()
	running := service.State()
	sameRunning := false
	for _, profile := range service.Installations() {
		if err := check.Err(); err != nil {
			m.packFailure(epoch, errors.New("сверка сборки заняла слишком долго; подключитесь к лобби ещё раз"))
			return nil
		}
		if profile.Base != manifest.Game || profile.Loader != manifest.Loader || !sameLoaderVersion(profile.Base, profile.Loader, profile.LoaderVersion, manifest.LoaderVersion) {
			continue
		}
		directory, e := service.ProfileDirectory(profile.ID, mode)
		if e != nil {
			continue
		}
		if running.Running && running.Profile == profile.ID && running.Directory != "" {
			directory = running.Directory
		}
		local, e := packsync.ScanMods(check, directory, packMeta(profile, ""))
		if e != nil {
			continue
		}
		for _, file := range local.Manifest.Files {
			plan.local[file.SHA256] = filepath.Join(directory, filepath.FromSlash(file.Path))
		}
		if packsync.Compatible(local.Manifest, manifest) {
			plan.profile = profile.ID
			if running.Running && running.Profile == profile.ID {
				sameRunning = true
				break
			}
		}
	}
	status := packStatus{Phase: "required", Name: manifest.Name, Game: manifest.Game, Loader: manifest.Loader, LoaderVersion: manifest.LoaderVersion, Digest: manifest.Digest, Restart: running.Running && !sameRunning, NeedsGame: plan.profile == ""}
	if mode, _, _ := m.game(); mode == "world" && sameRunning && peer.GameConnections() == 0 {
		sameRunning = false
		status.Restart = true
	}
	cache := m.packCache()
	for _, file := range manifest.Files {
		if plan.local[file.SHA256] == "" && !packsync.ValidCached(cache, file) {
			status.Missing++
			status.Bytes += file.Size
		}
	}
	if sameRunning || (!running.Running && plan.profile != "") {
		status.Phase = "ready"
		status.Profile = plan.profile
		status.Missing, status.Bytes = 0, 0
		status.Restart, status.NeedsGame = false, false
	}
	m.mu.Lock()
	if m.epoch != epoch || m.peers[m.guest] != peer || !peer.Connected() {
		m.mu.Unlock()
		return context.Canceled
	}
	m.guestPack = plan
	m.state.Pack = &status
	if m.state.WorldReady && (m.guestWorldDigest == "" || m.guestWorldDigest == manifest.Digest) {
		m.state.LocalPort = m.guestPort
	}
	m.mu.Unlock()
	if status.Phase != "ready" && fromInvite && !running.Running && len(manifest.Files) == 0 && status.NeedsGame {
		return m.startPackSync(manifest.Digest)
	}
	return nil
}

func readLobbyManifest(ctx context.Context, peer packsync.Fetcher) (packsync.Manifest, error) {
	var err error
	for attempt := range 2 {
		read, cancel := context.WithTimeout(ctx, 7*time.Second)
		var manifest packsync.Manifest
		manifest, err = packsync.ReadManifest(read, peer)
		cancel()
		if err == nil {
			return manifest, nil
		}
		var status packsync.ManifestHTTPError
		var network net.Error
		retry := errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.ErrUnexpectedEOF)
		if errors.As(err, &status) {
			retry = status == http.StatusTooManyRequests || status >= 500
		}
		if attempt == 1 || !retry || ctx.Err() != nil {
			break
		}
		select {
		case <-ctx.Done():
			return packsync.Manifest{}, ctx.Err()
		case <-time.After(600 * time.Millisecond):
		}
	}
	return packsync.Manifest{}, err
}

func lobbyManifestError(err error) error {
	var status packsync.ManifestHTTPError
	if errors.As(err, &status) {
		switch status {
		case http.StatusServiceUnavailable, http.StatusForbidden:
			return errors.New("лидеру нужно восстановить сборку лобби или создать комнату заново")
		case http.StatusNotFound:
			return errors.New("лаунчер лидера не отдаёт сборку. Проверьте версии Sintet")
		default:
			return errors.New("сборка временно недоступна. Повтори проверку")
		}
	}
	var network net.Error
	if errors.As(err, &network) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, net.ErrClosed) {
		return errors.New("канал сборки не ответил. Повтори проверку")
	}
	return errors.New("не удалось прочитать описание сборки. Попроси лидера обновить файлы лобби")
}

func (m *socialManager) retryGuestPack(ctx context.Context) error {
	m.mu.Lock()
	room, peer, epoch, name := m.state.Room, m.peers[m.guest], m.epoch, m.state.HostName
	canRetry := room != nil && room.Host != m.account && peer != nil && peer.Connected() && m.packCancel == nil
	m.mu.Unlock()
	if !canRetry {
		return errors.New("сначала восстанови соединение с лобби")
	}
	return m.prepareGuestPack(ctx, peer, epoch, socialAccount{ID: room.Host, Username: name}, false)
}
func (m *socialManager) packFailure(epoch uint64, err error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.guest == "" || m.peers[m.guest] == nil {
		return
	}
	if m.state.Pack == nil {
		m.state.Pack = &packStatus{}
	}
	m.state.Pack.Phase = "error"
	m.state.Pack.Error = err.Error()
	if launcher.IsDiskFull(err) {
		m.state.Pack.Error = lobbyDiskFullMessage
	}
}
func (m *socialManager) updatePack(epoch uint64, phase, current string, done, total int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.state.Pack == nil {
		return
	}
	m.state.Pack.Phase = phase
	m.state.Pack.Current = current
	m.state.Pack.Done = done
	m.state.Pack.Total = total
	m.state.Pack.Error = ""
}
func (m *socialManager) connectPreparedPack(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if m.app.forceQuit.Load() {
		return context.Canceled
	}
	m.mu.Lock()
	currentPlan := m.guestPack
	m.mu.Unlock()
	if currentPlan != nil {
		digest, err := m.exchangeLobby(ctx, currentPlan.peer, currentPlan.epoch)
		if err != nil {
			return err
		}
		if digest != "" && digest != currentPlan.manifest.Digest {
			_ = m.prepareGuestPack(ctx, currentPlan.peer, currentPlan.epoch, socialAccount{ID: currentPlan.host}, false)
			return errors.New("сборка обновилась; проверь подготовку файлов")
		}
	}
	m.mu.Lock()
	plan := m.guestPack
	status := m.state.Pack
	if plan == nil || status == nil || status.Phase != "ready" || plan.epoch != m.epoch {
		m.mu.Unlock()
		return errors.New("сначала синхронизируйте сборку хоста")
	}
	profile := plan.profile
	port := m.state.LocalPort
	restart := status.Restart
	m.mu.Unlock()
	if !plan.peer.Connected() || port <= 0 {
		return errors.New("ждём, пока лидер откроет мир для сети")
	}
	if m.app.State().Running {
		if !restart && m.app.State().Profile == profile {
			return m.connectGame()
		}
		if err := m.closeLobbyGame(ctx); err != nil {
			return err
		}
	}
	if !m.app.service.Installed(profile) {
		m.updatePack(plan.epoch, "installing", "Устанавливаем Minecraft и загрузчик", 0, 0)
		if err := m.app.service.StartInstallAs(profile, plan.manifest.Game, plan.manifest.Loader, plan.manifest.LoaderVersion, ""); err != nil {
			return err
		}
		done := make(chan struct{})
		go func() { m.app.service.WaitInstall(); close(done) }()
		select {
		case <-ctx.Done():
			m.app.service.Cancel()
			<-done
			return ctx.Err()
		case <-done:
		}
		if !m.app.service.Installed(profile) {
			return errors.New("не удалось установить Minecraft; открой журнал игры")
		}
	}
	m.updatePack(plan.epoch, "launching", "Запускаем Minecraft", 0, 0)
	if err := m.app.launchInstance(profile, "Player", port, plan.fromInvite, func() { m.queueColdLobbyJoin(plan, port) }); err != nil {
		m.packFailure(plan.epoch, err)
		return err
	}
	go m.finishPackLaunch(plan, profile)
	return nil
}
func (m *socialManager) finishPackLaunch(plan *guestPack, profile string) {
	timer := time.NewTimer(3 * time.Minute)
	defer timer.Stop()
	tick := time.NewTicker(500 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case <-m.ctx.Done():
			return
		case <-plan.peer.Closed():
			return
		case <-timer.C:
			m.packFailure(plan.epoch, errors.New("Minecraft запускается дольше обычного; откройте журнал игры"))
			return
		case <-tick.C:
			state := m.app.State()
			if state.Profile != profile {
				return
			}
			if state.Running {
				m.mu.Lock()
				if m.epoch == plan.epoch && m.state.Pack != nil {
					m.state.Pack.Phase = "ready"
					m.state.Pack.Profile = profile
					m.state.Pack.Restart = false
				}
				m.mu.Unlock()
				return
			}
			if !state.Busy {
				message := state.Error
				if message == "" {
					message = "Minecraft не запустился; откройте журнал игры"
				}
				m.packFailure(plan.epoch, errors.New(message))
				return
			}
		}
	}
}
func (m *socialManager) startPackSync(digest string) error {
	m.mu.Lock()
	plan := m.guestPack
	if plan == nil || m.state.Pack == nil || plan.epoch != m.epoch || digest != plan.manifest.Digest {
		m.mu.Unlock()
		return errors.New("сборка хоста изменилась; подключитесь к лобби заново")
	}
	if m.packCancel != nil {
		m.mu.Unlock()
		return errors.New("сборка уже синхронизируется")
	}
	if m.state.Pack.Phase != "required" && m.state.Pack.Phase != "error" {
		m.mu.Unlock()
		return errors.New("синхронизация сейчас не требуется")
	}
	if !m.app.remotePackBusy.CompareAndSwap(false, true) {
		m.mu.Unlock()
		return errors.New("дождитесь текущей операции")
	}
	ctx, cancel := context.WithCancel(m.ctx)
	m.packCancel = cancel
	m.packConsent = digest
	m.state.Pack.Phase = "downloading"
	m.state.Pack.Error = ""
	m.mu.Unlock()
	if m.app.ctx != nil {
		m.app.ShowLauncher()
	}
	go func() {
		defer cancel()
		defer m.app.remotePackBusy.Store(false)
		done := make(chan struct{})
		go func() {
			select {
			case <-plan.peer.Closed():
				cancel()
			case <-done:
			case <-ctx.Done():
			}
		}()
		defer close(done)
		err := m.syncPack(ctx, plan)
		m.mu.Lock()
		if m.epoch == plan.epoch && m.guestPack == plan {
			m.packCancel = nil
			if err == nil || launcher.IsDiskFull(err) {
				m.packConsent = ""
			}
		}
		m.mu.Unlock()
		if err != nil {
			if errors.Is(err, context.Canceled) {
				err = errors.New("синхронизация отменена; исходная сборка сохранена")
			}
			m.packFailure(plan.epoch, err)
		}
	}()
	return nil
}
func (m *socialManager) syncPack(ctx context.Context, plan *guestPack) error {
	options := packsync.DownloadOptions{}
	if m.app.workshop != nil {
		client := m.app.workshop.Client
		options.Resolvers = []packsync.Resolver{packsync.ModrinthResolver{HTTP: client.HTTP, BaseURL: client.Modrinth}, packsync.CurseForgeResolver{HTTP: client.HTTP, BaseURL: client.CurseForge, Key: client.Key}}
	}
	var fetcher packsync.Fetcher = plan.peer
	if plan.peer.Relayed() {
		fetcher = nil
	}
	if err := packsync.DownloadWithOptions(ctx, plan.manifest, m.packCache(), fetcher, plan.local, func(name string, done, total int64) { m.updatePack(plan.epoch, "downloading", name, done, total) }, options); err != nil {
		if plan.peer.Relayed() && errors.Is(err, packsync.ErrPeerFilesUnavailable) {
			return errors.New("Файлы хоста доступны только при прямом соединении")
		}
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	service := m.app.service
	if state := service.State(); state.Busy {
		return errors.New("дождитесь текущего запуска или установки Minecraft")
	}
	m.updatePack(plan.epoch, "downloading", "Подготавливаем отдельную сборку", 0, 0)
	m.app.contentMu.Lock()
	profile, err := m.materializePack(ctx, plan)
	m.app.contentMu.Unlock()
	if err != nil {
		return err
	}
	m.mu.Lock()
	if m.epoch != plan.epoch || m.state.Pack == nil {
		m.mu.Unlock()
		return context.Canceled
	}
	plan.profile = profile
	m.mu.Unlock()
	// Preparation never closes a running world. Restart only on a join action.
	if service.State().Running && !service.Installed(profile) {
		m.updatePack(plan.epoch, "ready", "Закрой текущую игру перед установкой Minecraft", 0, 0)
		m.mu.Lock()
		if m.epoch != plan.epoch || m.state.Pack == nil {
			m.mu.Unlock()
			return context.Canceled
		}
		m.state.Pack.Profile = profile
		m.state.Pack.Restart = true
		m.mu.Unlock()
		return nil
	}
	if !service.Installed(profile) {
		m.updatePack(plan.epoch, "installing", "Устанавливаем Minecraft и загрузчик", 0, 0)
		if err := service.StartInstallAs(profile, plan.manifest.Game, plan.manifest.Loader, plan.manifest.LoaderVersion, ""); err != nil {
			return err
		}
		done := make(chan struct{})
		go func() { service.WaitInstall(); close(done) }()
		ticker := time.NewTicker(500 * time.Millisecond)
		waiting := true
		for waiting {
			select {
			case <-ctx.Done():
				service.Cancel()
				<-done
				ticker.Stop()
				return ctx.Err()
			case <-done:
				waiting = false
			case <-ticker.C:
				state := service.State()
				m.updatePack(plan.epoch, "installing", state.Stage, int64(state.Percent), 100)
			}
		}
		ticker.Stop()
		if !service.Installed(profile) {
			return fmt.Errorf("не удалось установить Minecraft: %s", service.State().Error)
		}
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	if m.epoch != plan.epoch || !plan.peer.Connected() {
		m.mu.Unlock()
		return errors.New("хост закрыл лобби")
	}
	m.state.Pack.Phase = "ready"
	m.state.Pack.Profile = profile
	m.state.Pack.Restart = service.State().Running && service.State().Profile != profile
	m.state.Pack.Missing, m.state.Pack.Bytes, m.state.Pack.NeedsGame = 0, 0, false
	m.mu.Unlock()
	return nil
}

func (m *socialManager) closeLobbyGame(ctx context.Context) error {
	m.app.gameHidWindow.Store(false)
	closeCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	err := m.app.service.CloseGame(closeCtx)
	if errors.Is(err, context.DeadlineExceeded) && ctx.Err() == nil {
		return errors.New("Minecraft не закрылся; сохрани мир и закрой игру")
	}
	return err
}
func (m *socialManager) materializePack(ctx context.Context, plan *guestPack) (string, error) {
	root := m.app.service.Root
	if plan.profile != "" {
		dir, err := m.app.service.ProfileDirectory(plan.profile, m.app.settings.InstanceMode)
		if err == nil {
			info, e := m.app.service.ProfileIdentity(plan.profile)
			if e == nil {
				local, e := packsync.ScanMods(ctx, dir, packMeta(info, ""))
				if e == nil && packsync.Compatible(local.Manifest, plan.manifest) {
					return plan.profile, nil
				}
			}
		}
	}
	raw := make([]byte, 5)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	id := "build-p2p-" + hex.EncodeToString(raw)
	parent := filepath.Join(root, "instances")
	if err := os.MkdirAll(parent, 0700); err != nil {
		return "", err
	}
	directory := filepath.Join(parent, id)
	if err := packsync.Populate(ctx, plan.manifest, m.packCache(), directory); err != nil {
		return "", err
	}
	registered := false
	defer func() {
		if !registered {
			packsync.RemoveUnpublished(directory, plan.manifest)
		}
	}()
	for _, folder := range []string{"mods", "config", "resourcepacks", "saves"} {
		if err := os.MkdirAll(filepath.Join(directory, folder), 0700); err != nil {
			return "", err
		}
	}
	name := []rune(plan.manifest.Name)
	if len(name) > 30 {
		name = name[:30]
	}
	label := "P2P · " + string(name)
	if strings.TrimSpace(string(name)) == "" {
		label = "P2P · Minecraft " + plan.manifest.Game
	}
	build := WorkshopBuild{ID: id, Name: label, Icon: "grass", Game: plan.manifest.Game, Loader: plan.manifest.Loader, LoaderVersion: plan.manifest.LoaderVersion, UseGlobalSettings: true, MinMemoryMB: 512, MemoryMB: 4096, Width: 1280, Height: 720}
	store, err := m.app.readWorkshopBuilds()
	if err != nil {
		return "", err
	}
	if len(store.Builds) >= 500 {
		return "", errors.New("слишком много сборок; удалите ненужные")
	}
	store.Builds = append(store.Builds, build)
	if err = m.app.writeWorkshopBuildManifest(root, build); err != nil {
		return "", err
	}
	data, _ := json.Marshal(plan.manifest)
	if err = accountfiles.Write(filepath.Join(directory, ".sintet-p2p.json"), data); err != nil {
		return "", err
	}
	if err = m.app.saveWorkshopBuilds(store); err != nil {
		return "", err
	}
	registered = true
	return id, nil
}
