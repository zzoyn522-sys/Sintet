package main

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
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
	WorldReady bool               `json:"worldReady"`
	Digest     string             `json:"digest"`
	Members    []lobbyMemberState `json:"members"`
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
		profiles := m.app.service.Installations()
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
	previous := m.state.WorldReady
	m.mu.Unlock()
	if !game.Running || game.Profile != profile {
		port = 0
	}
	if port > 0 && !previous {
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
	if m.epoch != epoch || m.hostProfile != profile {
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
	if profile == "" || !m.app.service.Installed(profile) {
		return errors.New("сначала установи выбранную сборку")
	}
	snapshot, err := m.captureHostPack(ctx, profile)
	if err != nil {
		return err
	}
	manifest := snapshot.Manifest
	var reply struct {
		Room *socialRoom `json:"room"`
	}
	if err := m.post(ctx, map[string]any{"action": "room-pack", "code": room.Code,
		"version": manifest.Game, "loader": manifest.Loader, "pack_name": cleanActivityLabel(manifest.Name)}, &reply); err != nil {
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
	if m.hostPack == nil || m.hostPack.Manifest.Digest != manifest.Digest || m.hostProfile != profile {
		m.state.WorldReady = false
		m.memberPreparation = make(map[string]lobbyPreparation)
		for _, peer := range m.peers {
			if peer != nil {
				peer.SetHostPort(0)
			}
		}
	}
	m.hostProfile = profile
	m.hostPack = &snapshot
	m.state.Pack = &packStatus{Phase: "hosting", Name: manifest.Name, Game: manifest.Game, Loader: manifest.Loader, Digest: manifest.Digest}
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
	return m.app.launchInstance(profile, "Player", 0, false, nil)
}

func allowedLobbyPhase(phase string) bool {
	switch phase {
	case "checking", "required", "downloading", "installing", "stopping", "launching", "ready", "error", "world":
		return true
	}
	return false
}

func (m *socialManager) hostLobbyHandler(guest string) http.Handler {
	var revisionMu sync.Mutex
	var revision *packsync.Snapshot
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		m.mu.Lock()
		snapshot, room, account := m.hostPack, m.state.Room, m.account
		authorized := false
		for _, peer := range m.state.Peers {
			if peer.Guest == guest {
				authorized = true
				break
			}
		}
		m.mu.Unlock()
		if room == nil || room.Host != account || !authorized {
			http.Error(w, "Lobby unavailable", 403)
			return
		}
		if snapshot == nil {
			http.Error(w, "Host pack not prepared", http.StatusServiceUnavailable)
			return
		}
		if r.URL.Path != "/lobby" {
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
		var report lobbyPreparation
		decoder := json.NewDecoder(io.LimitReader(r.Body, 2049))
		if err := decoder.Decode(&report); err != nil || !allowedLobbyPhase(report.Phase) || report.Percent < 0 || report.Percent > 100 || len(report.Digest) > 64 {
			http.Error(w, "Invalid lobby state", 400)
			return
		}
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.hostPack == nil || m.state.Room == nil || m.state.Room.Code != room.Code {
			http.Error(w, "Lobby closed", 410)
			return
		}
		if m.memberPreparation == nil {
			m.memberPreparation = make(map[string]lobbyPreparation)
		}
		if report.Digest != m.hostPack.Manifest.Digest {
			report = lobbyPreparation{Phase: "checking"}
		}
		m.memberPreparation[guest] = report
		result := lobbyExchange{WorldReady: m.state.WorldReady, Digest: m.hostPack.Manifest.Digest}
		for _, member := range m.state.Members {
			if member.Leader {
				continue
			}
			result.Members = append(result.Members, lobbyMemberState{ID: member.ID, lobbyPreparation: m.memberPreparation[member.ID]})
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
	report := lobbyPreparation{Phase: "checking"}
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
	} else if err = json.NewDecoder(io.LimitReader(response.Body, 16<<10)).Decode(&result); err != nil {
		return "", err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch {
		return "", context.Canceled
	}
	m.state.WorldReady = result.WorldReady
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
		m.gameCommandPort = 0
		m.gameJoinUntil = time.Time{}
		m.state.Status = "В мире"
	case !result.WorldReady:
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
		m.mu.Lock()
		plan := m.guestPack
		pack := m.state.Pack
		cancelSync := m.packCancel
		changed := plan != nil && digest != "" && digest != plan.manifest.Digest
		recovered := plan == nil && digest != "" && digest != retriedDigest && pack != nil && pack.Phase == "error" && pack.Digest == ""
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
	}
	if err := m.post(ctx, map[string]any{"action": "relay-credentials", "peer": peerID}, &credentials); err != nil {
		return nil, err
	}
	return p2p.NewRelayPeer(port, credentials.Servers, trace)
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
				err = m.joinOnce(ctx, recovery.code, current.peer, true, false, true, 18*time.Second, 15*time.Second)
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
	if plan == nil || room == nil || room.Code != recovery.code || plan.epoch != m.epoch || !plan.peer.Connected() {
		m.mu.Unlock()
		return nil
	}
	m.state.AutoConnect = recovery.autoConnect
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

func (m *socialManager) tryAutoLobbyConnect(ctx context.Context, epoch uint64) {
	if m.app.State().Busy {
		return
	}
	m.mu.Lock()
	ready := m.epoch == epoch && m.state.AutoConnect && m.state.WorldReady && m.state.LocalPort != 0 && !m.state.InWorld && !m.state.Busy && m.packCancel == nil && !m.lobbyConnectBusy && m.state.Pack != nil && m.state.Pack.Phase == "ready"
	if ready {
		m.lobbyConnectBusy = true
	}
	m.mu.Unlock()
	if !ready {
		return
	}
	go func() {
		err := m.action(ctx, "connect-game", "")
		m.mu.Lock()
		if m.epoch == epoch {
			m.lobbyConnectBusy = false
			m.state.AutoConnect = false
		}
		m.mu.Unlock()
		if err != nil {
			m.errorForEpoch(epoch, err)
		}
	}()
}
