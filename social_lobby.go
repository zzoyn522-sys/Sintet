package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"

	"sintet/internal/packsync"
	p2p "sintet/internal/social"
)

type lobbyPreparation struct {
	Phase   string `json:"phase"`
	Percent int    `json:"percent"`
	Digest  string `json:"digest"`
}
type lobbyMemberState struct {
	ID string `json:"id"`
	lobbyPreparation
}
type lobbyExchange struct {
	WorldReady    bool               `json:"worldReady"`
	NoPack        bool               `json:"noPack,omitempty"`
	Digest        string             `json:"digest"`
	Members       []lobbyMemberState `json:"members"`
	RosterVersion uint64             `json:"rosterVersion,omitempty"`
}
type lobbyReport struct {
	lobbyPreparation
	RosterVersion uint64 `json:"rosterVersion,omitempty"`
}

type lobbyHostSelection struct {
	profile, version, loader, name string
	pack                           *packsync.Snapshot
}

func (m *socialManager) resizeLobby(ctx context.Context, value string, disconnect bool) error {
	capacity, err := strconv.Atoi(value)
	if err != nil || capacity < 4 || capacity > maximumLobbyCapacity {
		return errors.New("размер лобби должен быть от 4 до 150")
	}
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	state := m.snapshot()
	if !state.Leader || state.Room == nil {
		return errors.New("менять размер лобби может только лидер")
	}
	if state.Room.Capacity == 0 {
		return errors.New("Этот сервер пока поддерживает только 4 места.")
	}
	var result struct {
		Room         *socialRoom  `json:"room"`
		Disconnected []socialPeer `json:"disconnected"`
	}
	if err := m.post(ctx, map[string]any{"action": "room-capacity", "code": state.Room.Code, "capacity": capacity, "disconnect": disconnect}, &result); err != nil {
		return err
	}
	if result.Room == nil || result.Room.Code != state.Room.Code || result.Room.Capacity != capacity {
		return errors.New("сервис не подтвердил новый размер лобби")
	}
	var closed []*p2p.Peer
	m.mu.Lock()
	if m.state.Room != nil && m.state.Room.Code == state.Room.Code && m.account == result.Room.Host {
		m.state.Room = result.Room
		for _, removed := range result.Disconnected {
			if peer := m.peers[removed.ID]; peer != nil {
				closed = append(closed, peer)
			}
			delete(m.peers, removed.ID)
			delete(m.memberPreparation, removed.Guest)
		}
	}
	m.mu.Unlock()
	for _, peer := range closed {
		peer.Close()
	}
	return nil
}

func (m *socialManager) prepareLobbyHost(ctx context.Context, profile string) (lobbyHostSelection, error) {
	if profile == manualLobbyProfile {
		// The room service preserves the version string and validates the loader on changes.
		return lobbyHostSelection{profile: profile, version: manualLobbyProfile, loader: "vanilla"}, nil
	}
	pack, err := m.captureHostPack(ctx, profile)
	if err != nil {
		return lobbyHostSelection{}, fmt.Errorf("не удалось подготовить сборку: %w", err)
	}
	manifest := pack.Manifest
	return lobbyHostSelection{profile: profile, version: manifest.Game, loader: manifest.Loader, name: cleanActivityLabel(manifest.Name), pack: &pack}, nil
}

func (m *socialManager) applyLobbyHostLocked(selection lobbyHostSelection) {
	m.hostProfile = selection.profile
	m.hostPack = selection.pack
	m.state.NoPack = selection.pack == nil
	m.state.Pack = nil
	if selection.pack != nil {
		manifest := selection.pack.Manifest
		m.state.Pack = &packStatus{Phase: "hosting", Name: manifest.Name, Game: manifest.Game, Loader: manifest.Loader, Digest: manifest.Digest, Missing: len(manifest.Files)}
	}
}

func (m *socialManager) lobbyHostProfile(requested string) (string, error) {
	state := m.app.service.State()
	if state.Running {
		if requested != "" && requested != state.Profile {
			return "", errors.New("для запущенной игры выбери её сборку")
		}
		return state.Profile, nil
	}
	if requested != "" && m.app.service.Installed(requested) {
		return requested, nil
	}
	if requested == "" {
		profiles := withoutSharedInstallations(m.app.service.Installations())
		if len(profiles) > 0 {
			return profiles[0].ID, nil
		}
	}
	return "", errors.New("сначала установи выбранную версию или сборку Minecraft")
}

// Room membership and the encrypted file channel outlive the Minecraft socket.
func (m *socialManager) updateHostedWorld(port int) {
	game := m.app.service.State()
	m.mu.Lock()
	profile, epoch := m.hostProfile, m.epoch
	manual := m.state.Room.manual()
	previous := m.state.WorldReady
	m.mu.Unlock()
	if !game.Running || (!manual && game.Profile != profile) {
		port = 0
	}
	if port > 0 && !previous && !manual {
		ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
		err := m.refreshLobbyPack(ctx)
		cancel()
		if err != nil {
			m.logError(err)
			port = 0
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.hostProfile != profile || m.state.Room.manual() != manual {
		return
	}
	m.state.WorldReady = port > 0
	for _, peer := range m.peers {
		if peer != nil {
			peer.SetHostPort(port)
		}
	}
}

// Changing a pack updates the existing room and retains its peer channels.
func (m *socialManager) changeLobbyPack(ctx context.Context, profile string) error {
	m.mu.Lock()
	room, account, epoch := m.state.Room, m.account, m.epoch
	m.mu.Unlock()
	if room == nil || room.Host != account {
		return errors.New("сборку может сменить только лидер лобби")
	}
	if profile != manualLobbyProfile && (profile == "" || !m.app.service.Installed(profile)) {
		return errors.New("сначала установи выбранную сборку")
	}
	selection, err := m.prepareLobbyHost(ctx, profile)
	if err != nil {
		return err
	}
	var reply struct {
		Room *socialRoom `json:"room"`
	}
	if err := m.post(ctx, map[string]any{"action": "room-pack", "code": room.Code,
		"version": selection.version, "loader": selection.loader, "pack_name": selection.name}, &reply); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.account != account || m.state.Room == nil || m.state.Room.Code != room.Code {
		return context.Canceled
	}
	if reply.Room != nil {
		m.state.Room = reply.Room
	}
	if m.hostProfile != profile || (selection.pack != nil && (m.hostPack == nil || m.hostPack.Manifest.Digest != selection.pack.Manifest.Digest)) {
		m.state.WorldReady = false
		m.memberPreparation = make(map[string]lobbyPreparation)
		for _, peer := range m.peers {
			if peer != nil {
				peer.SetHostPort(0)
			}
		}
	}
	m.applyLobbyHostLocked(selection)
	return nil
}

func (m *socialManager) refreshLobbyPack(ctx context.Context) error {
	m.mu.Lock()
	profile, epoch, account := m.hostProfile, m.epoch, m.account
	room := m.state.Room
	m.mu.Unlock()
	if profile == "" || room == nil || room.Host != account {
		return errors.New("сначала создай лобби")
	}
	snapshot, err := m.captureHostPack(ctx, profile)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.hostProfile != profile {
		return context.Canceled
	}
	m.hostPack = &snapshot
	m.state.Pack = &packStatus{Phase: "hosting", Name: snapshot.Manifest.Name, Game: snapshot.Manifest.Game, Loader: snapshot.Manifest.Loader, Digest: snapshot.Manifest.Digest}
	return nil
}

func (m *socialManager) launchLobbyHost(ctx context.Context) error {
	if err := m.refreshLobbyPack(ctx); err != nil {
		return err
	}
	m.mu.Lock()
	profile := m.hostProfile
	m.mu.Unlock()
	if m.app.State().Running {
		return errors.New("Minecraft уже запущен; открой мир для сети")
	}
	return m.app.Launch(profile, "Player")
}

func allowedLobbyPhase(phase string) bool {
	switch phase {
	case "checking", "required", "downloading", "installing", "stopping", "launching", "ready", "error", "world":
		return true
	}
	return false
}

func (m *socialManager) hostLobbyHandler(peerID, guest string) http.Handler {
	var revisionMu sync.Mutex
	var revision *packsync.Snapshot
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		snapshot, room, account := m.hostPack, m.state.Room, m.account
		authorized := false
		for _, peer := range m.state.Peers {
			if peer.ID == peerID && peer.Guest == guest {
				authorized = true
				break
			}
		}
		m.mu.Unlock()
		if room == nil || room.Host != account || !authorized {
			http.Error(w, "Lobby unavailable", 403)
			return
		}
		if snapshot == nil && !room.manual() {
			http.Error(w, "Host pack not prepared", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/lobby" {
			if room.manual() {
				http.NotFound(w, r)
				return
			}
			revisionMu.Lock()
			if revision == nil || r.URL.Path == "/manifest" {
				revision = snapshot
			}
			selected := revision
			revisionMu.Unlock()
			selected.Handler().ServeHTTP(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "Method not allowed", 405)
			return
		}
		var report lobbyReport
		decoder := json.NewDecoder(io.LimitReader(r.Body, 2049))
		if err := decoder.Decode(&report); err != nil || !allowedLobbyPhase(report.Phase) || report.Percent < 0 || report.Percent > 100 || len(report.Digest) > 64 {
			http.Error(w, "Invalid lobby state", 400)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.state.Room == nil || m.state.Room.Code != room.Code {
			http.Error(w, "Lobby closed", 410)
			return
		}
		if m.memberPreparation == nil {
			m.memberPreparation = make(map[string]lobbyPreparation)
		}
		manual := m.state.Room.manual()
		digest := ""
		if !manual && m.hostPack == nil {
			http.Error(w, "Host pack not prepared", http.StatusServiceUnavailable)
			return
		}
		if !manual {
			digest = m.hostPack.Manifest.Digest
		}
		preparation := report.lobbyPreparation
		if manual && report.Phase != "world" {
			preparation = lobbyPreparation{Phase: "ready"}
		} else if report.Digest != digest && !manual {
			preparation = lobbyPreparation{Phase: "checking"}
		}
		if m.memberPreparation[guest] != preparation || m.rosterVersion == 0 {
			m.memberPreparation[guest] = preparation
			m.rosterVersion++
		}
		result := lobbyExchange{WorldReady: m.state.WorldReady, NoPack: manual, Digest: digest, RosterVersion: m.rosterVersion}
		if report.RosterVersion != result.RosterVersion {
			for _, member := range m.state.Members {
				if member.Leader {
					continue
				}
				result.Members = append(result.Members, lobbyMemberState{ID: member.ID, lobbyPreparation: m.memberPreparation[member.ID]})
			}
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(result)
	})
}

func (m *socialManager) exchangeLobby(ctx context.Context, peer *p2p.Peer, epoch uint64) (string, error) {
	m.mu.Lock()
	if m.epoch != epoch {
		m.mu.Unlock()
		return "", context.Canceled
	}
	report := lobbyReport{lobbyPreparation: lobbyPreparation{Phase: "checking"}, RosterVersion: m.guestRosterVersion}
	if pack := m.state.Pack; pack != nil {
		report.Phase, report.Digest = pack.Phase, pack.Digest
		if pack.Total > 0 {
			report.Percent = int(min(int64(100), pack.Done*100/pack.Total))
		}
	}
	m.mu.Unlock()
	mode, _, _ := m.game()
	if peer.GameConnections() > 0 && mode == "world" {
		report.Phase = "world"
	}
	data, _ := json.Marshal(report)
	response, err := peer.LobbyExchange(ctx, data)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result lobbyExchange
	// Older hosts only share a manifest and already require an open LAN world.
	if response.StatusCode == http.StatusNotFound {
		result.WorldReady = true
	} else if response.StatusCode != http.StatusOK {
		return "", errors.New("не удалось получить состояние лобби")
	} else if err = json.NewDecoder(io.LimitReader(response.Body, 64<<10)).Decode(&result); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch {
		return "", context.Canceled
	}
	m.state.WorldReady = result.WorldReady
	m.guestRosterVersion = result.RosterVersion
	m.state.NoPack = result.NoPack
	if result.NoPack {
		if m.packCancel != nil {
			m.packCancel()
		}
		m.guestPack = nil
		m.packConsent = ""
		m.state.Pack = nil
	}
	if m.guestWorldDigest != result.Digest {
		m.resetAutoLobbyRetryLocked()
	}
	m.guestWorldDigest = result.Digest
	m.state.InWorld = report.Phase == "world"
	m.state.LocalPort = 0
	if result.WorldReady && (result.Digest == "" || m.guestPack != nil && m.guestPack.manifest.Digest == result.Digest) {
		m.state.LocalPort = m.guestPort
	}
	if m.memberPreparation == nil {
		m.memberPreparation = make(map[string]lobbyPreparation)
	}
	for _, member := range result.Members {
		if allowedLobbyPhase(member.Phase) {
			m.memberPreparation[member.ID] = member.lobbyPreparation
		}
	}
	now := time.Now()
	switch {
	case m.state.InWorld:
		m.state.AutoConnect = false
		m.resetAutoLobbyRetryLocked()
		m.gameCommandPort = 0
		m.gameJoinUntil = time.Time{}
		m.state.Status = "В мире"
	case !result.WorldReady:
		m.resetAutoLobbyRetryLocked()
		m.gameCommandPort = 0
		m.gameJoinUntil = time.Time{}
		m.state.Status = "Ждём, пока лидер откроет мир для сети"
	case m.gameCommandEpoch == epoch && m.gameCommandPort != 0 && m.gameCommandPort == m.state.LocalPort && now.Before(m.gameCommandUntil):
		// Preserve the menu/handoff stage while the Java bridge is still waiting.
	case m.gameCommandEpoch == epoch && m.state.LocalPort != 0 && now.Before(m.gameJoinUntil):
		m.state.Status = "Minecraft подключается к миру"
	default:
		if m.state.LocalPort != 0 && (!m.gameJoinUntil.IsZero() || m.gameCommandPort == m.state.LocalPort) {
			m.state.Error = "Не удалось подключиться"
		}
		m.gameCommandPort = 0
		m.gameJoinUntil = time.Time{}
		m.state.Status = "Мир доступен"
	}
	return result.Digest, nil
}

func (m *socialManager) watchGuestLobby(ctx context.Context, peer *p2p.Peer, epoch uint64, host socialAccount) {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	var announceCancel context.CancelFunc
	var retriedDigest string
	defer func() {
		if announceCancel != nil {
			announceCancel()
		}
	}()
	for {
		select {
		case <-ctx.Done():
			return
		case <-peer.Closed():
			return
		case <-tick.C:
		}
		poll, cancel := context.WithTimeout(ctx, 5*time.Second)
		digest, err := m.exchangeLobby(poll, peer, epoch)
		cancel()
		if err != nil {
			continue
		}
		if digest == "" {
			retriedDigest = ""
		}
		m.mu.Lock()
		plan := m.guestPack
		pack := m.state.Pack
		cancelSync := m.packCancel
		changed := plan != nil && digest != "" && digest != plan.manifest.Digest
		recovered := plan == nil && digest != "" && digest != retriedDigest && (pack == nil || pack.Phase == "error" && pack.Digest == "")
		port := m.state.LocalPort
		m.mu.Unlock()
		if changed && cancelSync != nil {
			cancelSync()
		} else if (changed || recovered) && cancelSync == nil {
			retriedDigest = digest
			_ = m.prepareGuestPack(ctx, peer, epoch, host, false)
		}
		if port > 0 && announceCancel == nil {
			var announce context.Context
			announce, announceCancel = context.WithCancel(ctx)
			go func() { _ = p2p.Announce(announce, port, host.Username) }()
		} else if port == 0 && announceCancel != nil {
			announceCancel()
			announceCancel = nil
		}
		m.tryAutoLobbyConnect(ctx, epoch)
	}
}

// A relay-only offer contains at least one candidate and no direct candidates.
func offerUsesRelay(offer *webrtc.SessionDescription) bool {
	if offer == nil {
		return false
	}
	found := false
	for _, line := range strings.Split(offer.SDP, "\n") {
		if !strings.HasPrefix(line, "a=candidate:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 || fields[6] != "typ" || fields[7] != "relay" {
			return false
		}
		found = true
	}
	return found
}

func (m *socialManager) newRelayPeer(ctx context.Context, peerID string, port int, trace *p2p.ICETrace) (*p2p.Peer, error) {
	var credentials struct {
		Servers []webrtc.ICEServer `json:"iceServers"`
		Metered bool               `json:"metered"`
	}
	m.mu.Lock()
	client, account := m.client, m.account
	roomCode := ""
	if m.state.Room != nil {
		roomCode = m.state.Room.Code
	}
	m.mu.Unlock()
	if client == nil || account == "" {
		return nil, errors.New("войдите через Sintet ID")
	}
	if err := client.SocialRequest(ctx, "POST", "/api/social", map[string]any{"action": "relay-credentials", "peer": peerID}, &credentials); err != nil {
		return nil, normalizeTURNError(err)
	}
	if !credentials.Metered {
		return p2p.NewRelayPeer(port, credentials.Servers, trace)
	}
	peer, err := p2p.NewMeteredRelayPeer(port, credentials.Servers, trace)
	if err == nil {
		go m.watchTURNPeer(client, account, peer, roomCode)
	}
	return peer, err
}

type guestLobbyRecovery struct {
	epoch                uint64
	peer, code, digest   string
	guarded, autoConnect bool
}

func (m *socialManager) guestRecoveryLocked() guestLobbyRecovery {
	recovery := guestLobbyRecovery{epoch: m.epoch, peer: m.guest, guarded: m.state.ResumeLobby}
	if m.state.Room != nil && m.state.Room.Host != m.account && m.guest != "" {
		recovery.code = m.state.Room.Code
		recovery.digest = m.packConsent
		recovery.autoConnect = !m.state.InWorld && (m.state.AutoConnect ||
			m.gameCommandEpoch == m.epoch && (m.gameCommandPort != 0 || time.Now().Before(m.gameJoinUntil)))
	}
	return recovery
}

func (m *socialManager) recoverGuestLobby(recovery guestLobbyRecovery) {
	if m.ctx == nil || m.ctx.Err() != nil {
		return
	}
	m.actions.Lock()
	defer m.actions.Unlock()
	m.mu.Lock()
	current := m.guestRecoveryLocked()
	if current.epoch != recovery.epoch || current.peer != recovery.peer ||
		current.code != recovery.code || m.guestPort != 0 {
		m.mu.Unlock()
		return
	}
	m.state.Busy = true
	m.state.Status = "Восстанавливаем соединение"
	m.state.Error = ""
	m.mu.Unlock()

	err := m.resumeGuestLobby(m.ctx, current)
	m.mu.Lock()
	m.state.Busy = false
	if err != nil && m.state.Room != nil && m.state.Room.Code == current.code && m.guestPort == 0 {
		m.state.Status = "Не удалось подключиться"
		m.state.Error = playerFacingSocialError(err)
	}
	m.mu.Unlock()
	if err != nil {
		m.logError(err)
	}
	go m.refresh()
}

func (m *socialManager) resumeGuestLobby(ctx context.Context, recovery guestLobbyRecovery) error {
	var err error
	if recovery.guarded {
		err = m.joinOnce(ctx, recovery.code, recovery.peer, true, false, false, 12*time.Second, 13*time.Second)
		if errors.Is(err, errDirectTunnel) && ctx.Err() == nil {
			m.mu.Lock()
			current := m.guestRecoveryLocked()
			relay := m.state.RelayEnabled && current.code == recovery.code
			m.mu.Unlock()
			if relay {
				err = m.joinOnce(ctx, recovery.code, current.peer, true, false, true, 40*time.Second, 20*time.Second)
			}
		}
	} else {
		err = m.join(ctx, recovery.code)
	}
	if err != nil {
		return err
	}
	m.mu.Lock()
	plan := m.guestPack
	room := m.state.Room
	if room == nil || room.Code != recovery.code {
		m.mu.Unlock()
		return nil
	}
	m.state.AutoConnect = recovery.autoConnect
	if plan == nil || plan.epoch != m.epoch || !plan.peer.Connected() {
		m.mu.Unlock()
		return nil
	}
	resumePack := m.state.Pack != nil && m.state.Pack.Phase == "required" && plan.manifest.Digest == recovery.digest
	if resumePack {
		m.packConsent = recovery.digest
	}
	m.mu.Unlock()
	if !resumePack {
		return nil
	}
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for m.app.remotePackBusy.Load() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-deadline.C:
			return nil
		case <-tick.C:
		}
	}
	return m.startPackSync(recovery.digest)
}

func (m *socialManager) resetAutoLobbyRetryLocked() {
	m.autoJoinAttempts = 0
	m.autoJoinRetryAt = time.Time{}
}

func (m *socialManager) setAutoLobbyConnect(enabled bool) error {
	m.mu.Lock()
	if !m.state.Enabled || m.state.Room == nil || m.state.Room.Host == m.account || m.guest == "" {
		m.mu.Unlock()
		return errors.New("сначала войди в лобби")
	}
	m.state.AutoConnect = enabled && !m.state.InWorld
	m.resetAutoLobbyRetryLocked()
	epoch := m.epoch
	m.mu.Unlock()
	if enabled {
		m.tryAutoLobbyConnect(m.ctx, epoch)
	}
	return nil
}

func (m *socialManager) autoLobbyReadyLocked(epoch uint64, now time.Time) bool {
	return m.epoch == epoch && m.state.AutoConnect && m.state.WorldReady && m.state.LocalPort != 0 &&
		!m.state.InWorld && !m.state.Busy && m.packCancel == nil && !m.lobbyConnectBusy &&
		!m.gameJoinPendingLocked() && m.autoJoinAttempts < 2 && !now.Before(m.autoJoinRetryAt) &&
		(m.state.NoPack && now.Sub(m.bridgeSeen) <= 3*time.Second || m.state.Pack != nil && m.state.Pack.Phase == "ready")
}

func (m *socialManager) tryAutoLobbyConnect(ctx context.Context, epoch uint64) {
	game := m.app.State()
	if game.Busy {
		return
	}
	m.mu.Lock()
	now := time.Now()
	ready := m.autoLobbyReadyLocked(epoch, now)
	if ready && game.Running && m.guestPack != nil && game.Profile == m.guestPack.profile &&
		now.Sub(m.bridgeSeen) > 3*time.Second && game.GameStarted > 0 && now.Sub(time.Unix(game.GameStarted, 0)) < 2*time.Minute {
		// Give a still-loading game time to start reporting through the bridge.
		ready = false
	}
	if ready {
		m.lobbyConnectBusy = true
		m.autoJoinAttempts++
		m.autoJoinRetryAt = now.Add(5 * time.Second)
	}
	m.mu.Unlock()
	if !ready {
		return
	}
	go func() {
		err := m.action(ctx, "auto-join", "")
		m.mu.Lock()
		if m.epoch == epoch {
			m.lobbyConnectBusy = false
		}
		m.mu.Unlock()
		if err != nil {
			m.errorForEpoch(epoch, err)
		}
	}()
}
