package main

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/pion/webrtc/v4"
	"sintet/internal/identity"
	"sintet/internal/launcher"
	"sintet/internal/packsync"
	p2p "sintet/internal/social"
)

type socialAccount struct {
	AvatarDigest string `json:"avatarDigest,omitempty"`
	AvatarURL    string `json:"avatarUrl,omitempty"`
	ID           string `json:"id"`
	Username     string `json:"username"`
	Handle       string `json:"handle,omitempty"`
}
type socialPresence struct {
	Status   string          `json:"status"`
	Version  string          `json:"version"`
	Activity *socialActivity `json:"activity,omitempty"`
}
type socialFriend struct {
	ID       string                `json:"id"`
	Account  *socialAccount        `json:"account"`
	Status   string                `json:"status"`
	Incoming bool                  `json:"incoming"`
	Presence *socialPresence       `json:"presence"`
	Totals   *friendActivityTotals `json:"totals,omitempty"`
}
type socialInvite struct {
	ID      string         `json:"id"`
	Code    string         `json:"room_code"`
	Game    string         `json:"game,omitempty"`
	Build   string         `json:"build,omitempty"`
	Loader  string         `json:"loader,omitempty"`
	Account *socialAccount `json:"account"`
}
type socialRoom struct {
	Code     string `json:"code"`
	Host     string `json:"host"`
	Version  string `json:"version"`
	PackName string `json:"pack_name,omitempty"`
	Loader   string `json:"loader,omitempty"`
}
type socialPeer struct {
	ID     string                     `json:"id"`
	Guest  string                     `json:"guest"`
	Offer  *webrtc.SessionDescription `json:"offer"`
	Answer *webrtc.SessionDescription `json:"answer"`
}
type socialMember struct {
	AvatarDigest string `json:"avatarDigest,omitempty"`
	AvatarURL    string `json:"avatarUrl,omitempty"`
	ID           string `json:"id"`
	Username     string `json:"username"`
	Handle       string `json:"handle,omitempty"`
	Leader       bool   `json:"leader"`
	Preparation  string `json:"preparation,omitempty"`
	Percent      int    `json:"percent,omitempty"`
}
type socialRemote struct {
	ResumeLobby  bool            `json:"resumeLobby,omitempty"`
	RelayEnabled bool            `json:"relayEnabled,omitempty"`
	Privacy      presencePrivacy `json:"privacy"`
	Members      []socialMember  `json:"members"`
	Friends      []socialFriend  `json:"friends"`
	Invites      []socialInvite  `json:"invites"`
	Room         *socialRoom     `json:"room"`
	Peers        []socialPeer    `json:"peers"`
}
type socialState struct {
	PreGameLobby bool             `json:"preGameLobby"`
	Pack         *packStatus      `json:"pack,omitempty"`
	Quality      *p2p.LinkQuality `json:"quality,omitempty"`
	NetworkPeers []socialLink     `json:"networkPeers,omitempty"`
	Leader       bool             `json:"leader"`
	Offline      bool             `json:"offline"`
	Reconnecting bool             `json:"reconnecting"`
	socialRemote
	Enabled        bool   `json:"enabled"`
	Busy           bool   `json:"busy"`
	JoinAttempt    int    `json:"joinAttempt,omitempty"`
	LobbyConnected bool   `json:"lobbyConnected"`
	HostNeedsPack  bool   `json:"hostNeedsPack,omitempty"`
	HostProfile    string `json:"hostProfile,omitempty"`
	Status         string `json:"connection"`
	Error          string `json:"error"`
	LANPort        int    `json:"lanPort"`
	LocalPort      int    `json:"localPort"`
	Ping           int64  `json:"ping"`
	HostName       string `json:"hostName"`
	Connected      int    `json:"connected"`
	GameReady      bool   `json:"gameReady"`
	GameJoining    bool   `json:"gameJoining"`
	GameRunning    bool   `json:"gameRunning"`
	WorldReady     bool   `json:"worldReady"`
	AutoConnect    bool   `json:"autoConnect"`
	InWorld        bool   `json:"inWorld"`
}

type socialLink struct {
	Name string `json:"name"`
	p2p.LinkQuality
}
type socialManager struct {
	hostPack          *packsync.Snapshot
	hostProfile       string
	guestPort         int
	guestWorldDigest  string
	memberPreparation map[string]lobbyPreparation
	lobbyConnectBusy  bool
	guestPack         *guestPack
	packCancel        context.CancelFunc
	packConsent       string
	lastGuestRecovery time.Time
	app               *App
	ctx               context.Context
	mu                sync.Mutex
	identityMu        sync.Mutex
	refreshMu         sync.Mutex
	packChecking      bool
	gameMu            sync.Mutex
	observedPort      int
	observedWorld     bool
	loggedRemote      bool
	loggedServer      string
	connectingAt      time.Time
	bridgeWorld       bool
	bridgeMode        string
	bridgeServer      string
	bridgeWorldSeen   time.Time
	logTail           string
	actions           sync.Mutex
	state             socialState
	client            *identity.Client
	account           string
	peers             map[string]*p2p.Peer
	epoch             uint64
	guest             string
	roomCancel        context.CancelFunc
	address, secret   string
	bridgeSeen        time.Time
	gameCommand       uint64
	gameCommandPort   int
	gameCommandUntil  time.Time
	gameCommandEpoch  uint64
	gameCommandMenu   bool
	gameJoinUntil     time.Time
	server            *http.Server
	syncFailures      int
	sharedMusicCover  string
	privacyRevision   uint64
}

func newSocialManager(app *App, ctx context.Context) *socialManager {
	m := &socialManager{app: app, ctx: ctx, peers: make(map[string]*p2p.Peer)}
	m.state.Friends = []socialFriend{}
	m.state.Invites = []socialInvite{}
	return m
}
func (m *socialManager) snapshot() socialState {
	m.mu.Lock()
	defer m.mu.Unlock()
	state := m.state
	state.PreGameLobby = true
	state.Members = append([]socialMember(nil), state.Members...)
	if state.Pack != nil {
		copy := *state.Pack
		state.Pack = &copy
	}
	if m.app != nil {
		state.Offline = !m.app.InternetAvailable()
	}
	state.Leader = state.Room != nil && state.Room.Host == m.account
	state.HostNeedsPack = state.Leader && m.hostPack == nil
	if state.Leader {
		state.HostProfile = m.hostProfile
	}
	state.LobbyConnected = state.Room != nil && (state.Leader || m.guestPort != 0 && m.peers[m.guest] != nil && m.peers[m.guest].Connected())
	for i := range state.Members {
		if report, ok := m.memberPreparation[state.Members[i].ID]; ok {
			state.Members[i].Preparation, state.Members[i].Percent = report.Phase, report.Percent
		}
	}
	if state.Leader {
		for _, signal := range state.Peers {
			peer := m.peers[signal.ID]
			if peer != nil && peer.Connected() {
				continue
			}
			for i := range state.Members {
				if state.Members[i].ID == signal.Guest {
					state.Members[i].Preparation = "connecting"
				}
			}
		}
	}
	state.GameReady = time.Since(m.bridgeSeen) <= 3*time.Second
	state.GameJoining = !state.InWorld && m.gameJoinPendingLocked()
	state.Connected = 0
	for id, peer := range m.peers {
		if peer == nil || !peer.Connected() {
			continue
		}
		if id != m.guest {
			state.Connected++
		}
		if state.Room != nil && (state.Leader || id == m.guest) {
			quality := peer.Quality()
			name, memberID := state.HostName, state.Room.Host
			if state.Leader {
				name = "Гость"
				for _, signal := range state.Peers {
					if signal.ID == id {
						memberID = signal.Guest
						break
					}
				}
			}
			for _, member := range state.Members {
				if member.ID == memberID && (!state.Leader || !member.Leader) {
					name = member.Username
					break
				}
			}
			if name == "" {
				name = "Хост"
			}
			state.NetworkPeers = append(state.NetworkPeers, socialLink{Name: name, LinkQuality: quality})
			if state.Quality == nil {
				state.Quality = &quality
			} else {
				state.Quality.KBPerSec += quality.KBPerSec
				if quality.PingMS != nil && (state.Quality.PingMS == nil || *quality.PingMS > *state.Quality.PingMS) {
					state.Quality.PingMS = quality.PingMS
				}
				if quality.Loss != nil && (state.Quality.Loss == nil || *quality.Loss > *state.Quality.Loss) {
					state.Quality.Loss = quality.Loss
				}
				if linkQualityRank[quality.Level] > linkQualityRank[state.Quality.Level] {
					state.Quality.Level = quality.Level
				}
			}
		}
	}
	sort.Slice(state.NetworkPeers, func(i, j int) bool { return state.NetworkPeers[i].Name < state.NetworkPeers[j].Name })
	if state.Quality != nil && state.Quality.PingMS != nil {
		state.Ping = *state.Quality.PingMS
	}
	if state.HostNeedsPack {
		state.Status = "Восстанови сборку лобби"
	} else if !state.Leader && state.Pack != nil && state.Pack.Phase == "error" && state.Pack.Digest == "" {
		state.Status = ""
	} else if state.LobbyConnected && !state.Busy {
		if !state.WorldReady {
			if state.Leader {
				state.Status = "Запусти сборку и открой мир для сети"
			} else {
				state.Status = "Ждём, пока лидер откроет мир для сети"
			}
		} else if state.Leader {
			state.Status = "Мир открыт для участников"
		}
	}
	return state
}

var linkQualityRank = map[string]int{"unknown": 0, "good": 1, "unstable": 2, "poor": 3}

func (m *socialManager) logError(err error) {
	if err == nil || m.app == nil || m.app.profiles == nil {
		return
	}
	path := filepath.Join(m.app.profiles.root, "p2p.log")
	file, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		return
	}
	defer file.Close()
	fmt.Fprintf(file, "%s %s\n", time.Now().Format(time.RFC3339), identity.ServiceDiagnostic(err))
}
func (m *socialManager) error(err error) {
	m.logError(err)
	m.mu.Lock()
	if err != nil {
		m.state.Error = playerFacingSocialError(err)
	} else {
		m.state.Error = ""
	}
	m.mu.Unlock()
}
func (m *socialManager) errorForEpoch(epoch uint64, err error) {
	m.logError(err)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch == epoch && err != nil {
		m.state.Error = playerFacingSocialError(err)
	}
}

func (m *socialManager) syncFailure(epoch uint64, err error) {
	if m.ctx != nil && m.ctx.Err() != nil {
		return
	}
	if !identity.TemporaryServiceError(err) {
		m.errorForEpoch(epoch, err)
		return
	}
	m.mu.Lock()
	if m.epoch != epoch {
		m.mu.Unlock()
		return
	}
	m.syncFailures++
	count := m.syncFailures
	m.state.Reconnecting = true
	if count >= 3 {
		m.state.Error = "Связь с Sintet ID прервана. Повторяем подключение…"
	}
	m.mu.Unlock()
	if count == 1 || count%6 == 0 {
		m.logError(err)
	}
}

func playerFacingSocialError(err error) string {
	if err == nil {
		return ""
	}
	if launcher.IsDiskFull(err) {
		return lobbyDiskFullMessage
	}
	text := err.Error()
	if errors.Is(err, errDirectTunnel) || strings.Contains(text, "WebRTC закрыт") || strings.Contains(text, "P2P") || strings.Contains(text, "соединиться напрямую") {
		return "Не удалось подключиться. Попробуйте ещё раз."
	}
	return text
}

var diagnosticStates = regexp.MustCompile(`ICE=([a-z-]+), (?:connection|DTLS)=([a-z-]+)`)

func (m *socialManager) sendDiagnostic(event string, err error) {
	if event == "error" && err == nil {
		return
	}
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return
	}
	code, ice, connection := "none", "", ""
	detail := ""
	if err != nil {
		text := err.Error()
		if _, tail, ok := strings.Cut(text, " | trace="); ok {
			detail = tail
			if len(detail) > 3500 {
				detail = detail[:3500]
			}
		}
		code = "other"
		switch {
		case strings.Contains(text, "WebRTC"):
			code = "webrtc_closed"
		case strings.Contains(text, "closed network"):
			code = "network_closed"
		case strings.Contains(text, "deadline"):
			code = "timeout"
		case strings.Contains(text, "сервис"):
			code = "service_unavailable"
		}
		if match := diagnosticStates.FindStringSubmatch(text); len(match) == 3 {
			ice, connection = match[1], match[2]
		}
	}
	body := map[string]any{"action": "diagnostic", "id": uuid.NewString(), "event": event, "error_code": code, "ice_state": ice, "connection_state": connection, "launcher_version": launcherVersion, "detail": detail}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		_ = client.SocialRequest(ctx, "POST", "/api/social", body, nil)
	}()
}
func (m *socialManager) request(ctx context.Context, method, path string, body, out any) error {
	m.mu.Lock()
	client := m.client
	m.mu.Unlock()
	if client == nil {
		return errors.New("войдите через Sintet ID, чтобы добавить друзей")
	}
	call, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return client.SocialRequest(call, method, path, body, out)
}
func (m *socialManager) post(ctx context.Context, body, out any) error {
	return m.request(ctx, "POST", "/api/social", body, out)
}
func (m *socialManager) identity() {
	m.identityMu.Lock()
	defer m.identityMu.Unlock()
	m.app.authMu.Lock()
	var client *identity.Client
	var id string
	if slot := m.app.profiles.find(m.app.profiles.active); slot != nil && slot.Provider == "sintet" {
		client = slot.sintet
		if account := client.SavedAccount(); account != nil {
			id = account.ID
		}
	}
	m.app.authMu.Unlock()
	m.mu.Lock()
	changed := m.account != id
	m.mu.Unlock()
	if changed {
		m.disconnect()
		m.mu.Lock()
		m.state = socialState{socialRemote: socialRemote{Friends: []socialFriend{}, Invites: []socialInvite{}}}
		m.account = id
		m.syncFailures = 0
		m.mu.Unlock()
	}
	m.mu.Lock()
	m.client = client
	m.state.Enabled = id != ""
	m.mu.Unlock()
}

const lanPortEvent = `(?:Started serving on|Local game hosted on port)\s+(?:\[(\d{1,5})\]|(\d{1,5})\b)`

var lanPortPattern = regexp.MustCompile(`(?i)` + lanPortEvent)

func detectedLANPort(log string) int {
	indices := lanPortPattern.FindAllStringSubmatchIndex(log, -1)
	if len(indices) == 0 {
		return 0
	}
	last := indices[len(indices)-1]
	if strings.LastIndex(log, "Stopping server") > last[0] {
		return 0
	}
	start, end := last[2], last[3]
	if start < 0 {
		start, end = last[4], last[5]
	}
	port, _ := strconv.Atoi(log[start:end])
	if port < 1 || port > 65535 {
		return 0
	}
	return port
}

var gameEventPattern = regexp.MustCompile(`(?i)` + lanPortEvent + `|Stopping server|Starting integrated minecraft server`)

func (m *socialManager) resetGameLog() {
	m.gameMu.Lock()
	m.observedPort = 0
	m.observedWorld = false
	m.loggedRemote = false
	m.loggedServer = ""
	m.connectingAt = time.Time{}
	m.bridgeWorld = false
	m.bridgeMode = ""
	m.bridgeServer = ""
	m.bridgeWorldSeen = time.Time{}
	m.logTail = ""
	m.gameMu.Unlock()
	m.mu.Lock()
	m.bridgeSeen = time.Time{}
	m.gameCommandPort = 0
	m.gameJoinUntil = time.Time{}
	m.mu.Unlock()
}
func (m *socialManager) observeGameLog(data []byte) {
	m.gameMu.Lock()
	defer m.gameMu.Unlock()
	text := m.logTail + string(data)
	end := strings.LastIndexByte(text, '\n')
	if end < 0 {
		if len(text) > 4096 {
			text = text[len(text)-4096:]
		}
		m.logTail = text
		return
	}
	m.logTail = text[end+1:]
	if len(m.logTail) > 4096 {
		m.logTail = m.logTail[len(m.logTail)-4096:]
	}
	for _, match := range gameEventPattern.FindAllStringSubmatch(text[:end], -1) {
		switch {
		case strings.EqualFold(match[0], "Stopping server"):
			m.observedPort = 0
			m.observedWorld = false
		case strings.EqualFold(match[0], "Starting integrated minecraft server"):
			m.observedPort = 0
			m.observedWorld = true
		default:
			value := match[1]
			if value == "" {
				value = match[2]
			}
			port, _ := strconv.Atoi(value)
			if port > 0 && port <= 65535 {
				m.observedPort = port
				m.observedWorld = true
			}
		}
	}
	m.observeRemoteGameLines(text[:end])
}
func (m *socialManager) game() (string, string, int) {
	activity, profile, port := m.gameActivity()
	return activity.legacyStatus(), profile, port
}
func (m *socialManager) run() {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	defer m.disconnect()
	go m.pollSocial()
	for {
		m.identity()
		if m.snapshot().Enabled {
			activity, version, port := m.gameActivity()
			before := m.snapshot()
			if before.Room != nil && before.Leader {
				m.updateHostedWorld(port)
			}
			m.mu.Lock()
			m.state.LANPort = port
			if activity.Mode != "launcher" && activity.accountID != m.account {
				activity, version = socialActivity{Mode: "launcher"}, ""
			}
			status := activity.legacyStatus()
			if m.state.Room != nil {
				activity.Mode, activity.Server = "lobby", ""
			}
			pollEpoch := m.epoch
			m.mu.Unlock()
			if err := m.publishPresence(activity, status, version); err != nil {
				m.syncFailure(pollEpoch, err)
			}
		} else {
			m.app.presenceMedia(false)
		}
		select {
		case <-m.ctx.Done():
			return
		case <-tick.C:
		}
	}
}

// Lobby signaling must keep moving while presence and its optional music
// upload wait for the service.
func (m *socialManager) pollSocial() {
	for m.ctx.Err() == nil {
		m.mu.Lock()
		enabled := m.state.Enabled
		hosting := m.state.Room != nil && m.state.Room.Host == m.account
		m.mu.Unlock()
		if enabled {
			m.refresh()
		}
		delay := 5 * time.Second
		if hosting {
			delay = 2 * time.Second
		}
		select {
		case <-m.ctx.Done():
			return
		case <-time.After(delay):
		}
	}
}

func (m *socialManager) refresh() {
	m.refreshMu.Lock()
	defer m.refreshMu.Unlock()
	m.mu.Lock()
	account := m.account
	epoch := m.epoch
	privacyRevision := m.privacyRevision
	guestAtRequest, portAtRequest := m.guest, m.guestPort
	m.mu.Unlock()
	var remote socialRemote
	ctx, cancel := context.WithTimeout(m.ctx, 6*time.Second)
	defer cancel()
	if err := m.request(ctx, "GET", "/api/social", nil, &remote); err != nil {
		m.syncFailure(epoch, err)
		return
	}
	if remote.Friends == nil {
		remote.Friends = []socialFriend{}
	}
	if remote.Invites == nil {
		remote.Invites = []socialInvite{}
	}
	m.mu.Lock()
	if m.account != account || m.epoch != epoch {
		m.mu.Unlock()
		return
	}
	for i := range remote.Friends {
		if person := remote.Friends[i].Account; person != nil {
			person.AvatarURL = m.avatarURL(person.ID, person.AvatarDigest)
		}
	}
	for i := range remote.Members {
		member := &remote.Members[i]
		member.AvatarURL = m.avatarURL(member.ID, member.AvatarDigest)
	}
	for i := range remote.Invites {
		invite := &remote.Invites[i]
		if person := invite.Account; person != nil {
			person.AvatarURL = m.avatarURL(person.ID, person.AvatarDigest)
			for _, friend := range remote.Friends {
				if friend.Account != nil && friend.Account.ID == person.ID && friend.Presence != nil && friend.Presence.Activity != nil {
					activity := friend.Presence.Activity
					if invite.Game == "" || invite.Game == activity.Version {
						if invite.Game == "" {
							invite.Game = activity.Version
						}
						if invite.Build == "" {
							invite.Build = activity.Build
						}
						if invite.Loader == "" {
							invite.Loader = activity.Loader
						}
					}
					break
				}
			}
		}
	}
	previousInvites := m.state.Invites
	if privacyRevision != m.privacyRevision {
		remote.Privacy = m.state.Privacy
	}
	if remote.Privacy != m.state.Privacy {
		m.sharedMusicCover = ""
	}
	m.state.socialRemote = remote
	memberIDs := make(map[string]bool, len(remote.Members))
	for _, member := range remote.Members {
		memberIDs[member.ID] = true
	}
	for id := range m.memberPreparation {
		if !memberIDs[id] {
			delete(m.memberPreparation, id)
		}
	}
	if m.syncFailures >= 3 {
		m.state.Error = ""
	}
	m.state.Reconnecting = false
	m.syncFailures = 0
	port := m.state.LANPort
	known := make(map[string]bool)
	for _, peer := range remote.Peers {
		known[peer.ID] = true
	}
	var removed []*p2p.Peer
	for id, peer := range m.peers {
		if !known[id] && (id != m.guest || id == guestAtRequest && portAtRequest != 0) {
			if id != m.guest {
				delete(m.peers, id)
			}
			if peer != nil {
				removed = append(removed, peer)
			}
		}
	}
	var incoming []socialPeer
	if remote.Room != nil && remote.Room.Host == account {
		for _, peer := range remote.Peers {
			if peer.Offer != nil {
				if _, exists := m.peers[peer.ID]; !exists {
					m.peers[peer.ID] = nil
					incoming = append(incoming, peer)
				}
			}
		}
	}
	m.mu.Unlock()
	m.notifyNewInvites(previousInvites, remote.Invites)
	for _, peer := range removed {
		peer.Close()
	}
	for _, peer := range incoming {
		go m.answer(peer, port, epoch)
	}
}
func (m *socialManager) answer(remote socialPeer, port int, epoch uint64) {
	ctx, cancel := context.WithTimeout(m.ctx, 45*time.Second)
	defer cancel()
	trace, finishTrace := m.startICETrace("host")
	trace.Stage("offer_received")
	var peer *p2p.Peer
	var err error
	if offerUsesRelay(remote.Offer) {
		trace.Stage("relay")
		peer, err = m.newRelayPeer(ctx, remote.ID, -1, trace)
	} else {
		peer, err = p2p.NewPeerWithICETrace(-1, true, trace)
	}
	if err != nil {
		finishTrace(remote.ID, nil, "peer_init_failed")
		m.logError(err)
		return
	}
	peer.SetPackHandler(m.hostLobbyHandler(remote.Guest))
	m.mu.Lock()
	if _, exists := m.peers[remote.ID]; !exists || m.epoch != epoch {
		m.mu.Unlock()
		finishTrace(remote.ID, peer, "superseded")
		peer.Close()
		return
	}
	m.peers[remote.ID] = peer
	if m.state.WorldReady {
		peer.SetHostPort(m.state.LANPort)
	}
	m.mu.Unlock()
	traceResult := "answer_failed"
	answer, err := peer.Answer(ctx, *remote.Offer)
	if err == nil {
		trace.Stage("answer_created")
		traceResult = "signal_failed"
		err = m.post(ctx, map[string]any{"action": "signal", "peer": remote.ID, "description": answer}, nil)
	}
	if err == nil {
		trace.Stage("answer_sent")
		traceResult = "tunnel_failed"
		err = peer.Wait(ctx)
	}
	if err == nil {
		traceResult = "ready"
	}
	finishTrace(remote.ID, peer, traceResult)
	if err != nil {
		peer.Close()
		m.mu.Lock()
		if m.epoch == epoch && m.peers[remote.ID] == peer {
			// A guest retry gets a new ID. Do not answer an expired attempt again.
			m.peers[remote.ID] = nil
		}
		m.mu.Unlock()
		m.logError(fmt.Errorf("P2P хост: %w", err))
		return
	}
	m.mu.Lock()
	if m.epoch != epoch || m.peers[remote.ID] != peer {
		m.mu.Unlock()
		peer.Close()
		return
	}
	m.mu.Unlock()
	<-peer.Closed()
	m.mu.Lock()
	if m.epoch == epoch && m.peers[remote.ID] == peer {
		m.peers[remote.ID] = nil
		delete(m.memberPreparation, remote.Guest)
	}
	m.mu.Unlock()
}
func (m *socialManager) disconnect() uint64 {
	m.mu.Lock()
	m.epoch++
	epoch := m.epoch
	peers := m.peers
	m.peers = make(map[string]*p2p.Peer)
	cancel := m.roomCancel
	packCancel := m.packCancel
	m.packCancel = nil
	m.packConsent = ""
	m.packChecking = false
	m.hostPack = nil
	m.hostProfile = ""
	m.guestPort = 0
	m.guestWorldDigest = ""
	m.memberPreparation = nil
	m.state.WorldReady = false
	m.state.AutoConnect = false
	m.state.InWorld = false
	m.gameCommandPort = 0
	m.gameJoinUntil = time.Time{}
	m.lobbyConnectBusy = false
	m.guestPack = nil
	m.state.Pack = nil
	m.roomCancel = nil
	m.guest = ""
	m.state.LocalPort = 0
	m.state.Ping = 0
	m.state.HostName = ""
	m.state.Status = ""
	m.state.Connected = 0
	m.state.Room = nil
	m.state.Members = nil
	m.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if packCancel != nil {
		packCancel()
	}
	for _, peer := range peers {
		if peer != nil {
			peer.Close()
		}
	}
	return epoch
}

func (m *socialManager) host(ctx context.Context, profiles ...string) error {
	state := m.snapshot()
	if state.Leader && !state.HostNeedsPack {
		return nil
	}
	profile := ""
	if len(profiles) > 0 {
		profile = profiles[0]
	}
	profile, err := m.lobbyHostProfile(profile)
	if err != nil {
		return err
	}
	snapshot, err := m.captureHostPack(ctx, profile)
	if err != nil {
		return fmt.Errorf("не удалось подготовить сборку: %w", err)
	}
	version := snapshot.Manifest.Game
	if state.HostNeedsPack &&
		(state.Room.Version != version || state.Room.Loader != "" && state.Room.Loader != snapshot.Manifest.Loader ||
			state.Room.PackName != "" && state.Room.PackName != cleanActivityLabel(snapshot.Manifest.Name)) {
		return errors.New("выбери исходную сборку лобби или закрой комнату и создай новую")
	}
	m.mu.Lock()
	guest := m.guest
	m.mu.Unlock()
	if guest != "" {
		_ = m.post(ctx, map[string]any{"action": "peer-leave", "peer": guest}, nil)
		m.disconnect()
	}
	var result struct {
		Room *socialRoom `json:"room"`
	}
	if err := m.post(ctx, map[string]any{"action": "room-create", "version": version,
		"pack_name": cleanActivityLabel(snapshot.Manifest.Name), "loader": snapshot.Manifest.Loader}, &result); err != nil {
		return err
	}
	m.mu.Lock()
	m.state.Room = result.Room
	m.hostPack = &snapshot
	m.hostProfile = profile
	m.memberPreparation = make(map[string]lobbyPreparation)
	m.state.Pack = &packStatus{Phase: "hosting", Name: snapshot.Manifest.Name, Game: version, Loader: snapshot.Manifest.Loader, Digest: snapshot.Manifest.Digest, Missing: len(snapshot.Manifest.Files)}
	m.state.Status = "Лобби создано"
	m.mu.Unlock()
	_, _, port := m.game()
	m.updateHostedWorld(port)
	return nil
}

var errDirectTunnel = errors.New("прямое соединение не установлено")

func (m *socialManager) join(ctx context.Context, code string) error {
	return m.joinRoom(ctx, code, false)
}

func (m *socialManager) joinRoom(ctx context.Context, code string, fromInvite bool) error {
	attempts := [...]struct{ signaling, tunnel time.Duration }{
		{9 * time.Second, 8 * time.Second},
		{12 * time.Second, 13 * time.Second},
	}
	defer func() {
		m.mu.Lock()
		m.state.JoinAttempt = 0
		m.mu.Unlock()
	}()
	var err error
	for index, budget := range attempts {
		attempt := index + 1
		m.mu.Lock()
		m.state.JoinAttempt = attempt
		m.state.Error = ""
		m.state.Status = "Входим в лобби"
		m.mu.Unlock()
		err = m.joinOnce(ctx, code, "", attempt > 1, fromInvite, false, budget.signaling, budget.tunnel)
		if attempt == len(attempts) || !errors.Is(err, errDirectTunnel) || ctx.Err() != nil {
			break
		}
		m.logError(fmt.Errorf("попытка подключения %d: %w", attempt, err))
	}
	if errors.Is(err, errDirectTunnel) && ctx.Err() == nil {
		m.mu.Lock()
		relayEnabled := m.state.RelayEnabled
		m.mu.Unlock()
		if relayEnabled {
			err = m.joinOnce(ctx, code, "", true, fromInvite, true, 18*time.Second, 15*time.Second)
		}
	}
	return err
}

func (m *socialManager) joinOnce(ctx context.Context, code, resumePeer string, retry, fromInvite, relay bool, signalBudget, tunnelBudget time.Duration) error {
	var result struct {
		Room socialRoom    `json:"room"`
		Peer socialPeer    `json:"peer"`
		Host socialAccount `json:"host"`
	}
	m.mu.Lock()
	previousGuest := m.guest
	previousRoom := m.state.Room
	account := m.account
	m.mu.Unlock()
	connect, cancel := context.WithTimeout(ctx, signalBudget)
	defer cancel()
	trace, finishTrace := m.startICETrace("guest")
	if retry {
		trace.Stage("retry")
	}
	if fromInvite {
		trace.Stage("invite")
	}
	var peer *p2p.Peer
	var err error
	var waitOffer func(context.Context) (*webrtc.SessionDescription, error)
	if relay {
		trace.Stage("relay")
		m.mu.Lock()
		m.state.JoinAttempt = 3
		m.state.Status = "Подключаемся через ретранслятор"
		m.mu.Unlock()
	}
	var epoch uint64
	var registered, success bool
	traceResult := "offer_failed"
	defer func() {
		finishTrace(result.Peer.ID, peer, traceResult)
		if !success {
			if peer != nil {
				peer.Close()
			}
			if registered {
				// Retain membership for an explicit retry or leave.
				m.finishGuest(epoch, result.Peer.ID, peer)
			}
		}
	}()
	if !relay {
		peer, err = p2p.NewPeerWithICETrace(0, true, trace)
		if err != nil {
			return err
		}
		trace.Stage("offer_gather_started")
		waitOffer, err = peer.StartOffer()
		if err != nil {
			return fmt.Errorf("%w: подготовка WebRTC: %w", errDirectTunnel, err)
		}
	}
	traceResult = "signal_failed"
	join := map[string]any{"action": "room-join", "code": code}
	if resumePeer != "" {
		join["action"] = "room-reconnect"
		join["peer"] = resumePeer
	}
	if err := m.post(connect, join, &result); err != nil {
		if connect.Err() != nil || identity.TemporaryServiceError(err) {
			return fmt.Errorf("%w: вход в лобби: %w", errDirectTunnel, err)
		}
		return err
	}
	trace.Stage("room_joined")
	// room-join already replaces the guest's previous peer in the same room.
	if previousGuest != "" && previousGuest != result.Peer.ID && (previousRoom == nil || previousRoom.Code != code) {
		leave, cancelLeave := context.WithTimeout(connect, 2*time.Second)
		_ = m.post(leave, map[string]any{"action": "peer-leave", "peer": previousGuest}, nil)
		cancelLeave()
	}
	if room := m.snapshot().Room; room != nil && room.Host == account {
		if err := m.post(connect, map[string]any{"action": "room-close"}, nil); err != nil {
			return err
		}
	}
	epoch = m.disconnect()
	m.mu.Lock()
	if m.epoch != epoch || m.account != account {
		m.mu.Unlock()
		traceResult = "superseded"
		return context.Canceled
	}
	m.peers[result.Peer.ID] = peer
	registered = true
	m.guest = result.Peer.ID
	m.state.Status = "Установка P2P соединения"
	if relay {
		m.state.Status = "Подключаемся через ретранслятор"
	} else if retry {
		m.state.Status = "Повторная попытка прямого подключения"
	}
	m.state.HostName = result.Host.Username
	m.state.Room = &result.Room
	m.mu.Unlock()
	traceResult = "offer_failed"
	if relay {
		relayPeer, relayErr := m.newRelayPeer(connect, result.Peer.ID, 0, trace)
		if relayErr != nil {
			return fmt.Errorf("%w: ретранслятор: %w", errDirectTunnel, relayErr)
		}
		m.mu.Lock()
		if m.epoch != epoch || m.guest != result.Peer.ID {
			m.mu.Unlock()
			relayPeer.Close()
			return context.Canceled
		}
		peer = relayPeer
		m.peers[result.Peer.ID] = peer
		m.mu.Unlock()
		trace.Stage("offer_gather_started")
		waitOffer, err = peer.StartOffer()
		if err != nil {
			return fmt.Errorf("%w: подготовка WebRTC: %w", errDirectTunnel, err)
		}
	}
	offer, err := waitOffer(connect)
	if err != nil {
		return fmt.Errorf("%w: подготовка WebRTC: %w", errDirectTunnel, err)
	}
	if relay && !offerUsesRelay(offer) {
		return fmt.Errorf("%w: TURN не выдал ICE-кандидата", errDirectTunnel)
	}
	trace.Stage("offer_created")
	traceResult = "signal_failed"
	if err = m.post(connect, map[string]any{"action": "signal", "peer": result.Peer.ID, "description": offer}, nil); err != nil {
		if connect.Err() != nil || identity.TemporaryServiceError(err) {
			return fmt.Errorf("%w: отправка предложения WebRTC: %w", errDirectTunnel, err)
		}
		return err
	}
	trace.Stage("offer_sent")
	traceResult = "answer_wait_failed"
	m.mu.Lock()
	if m.epoch == epoch && !relay {
		m.state.Status = "Ждём ответа лидера"
	}
	m.mu.Unlock()
	for {
		var reply struct {
			Peer socialPeer `json:"peer"`
		}
		if err = m.post(connect, map[string]any{"action": "peer-state", "peer": result.Peer.ID}, &reply); err != nil {
			if connect.Err() != nil || identity.TemporaryServiceError(err) {
				return fmt.Errorf("%w: ожидание ответа хоста: %w", errDirectTunnel, err)
			}
			return err
		}
		if reply.Peer.Answer != nil {
			trace.Stage("answer_received")
			traceResult = "answer_failed"
			if err = peer.SetAnswer(*reply.Peer.Answer); err != nil {
				return err
			}
			trace.Stage("answer_applied")
			break
		}
		select {
		case <-connect.Done():
			return fmt.Errorf("%w: ожидание ответа хоста: %w", errDirectTunnel, connect.Err())
		case <-time.After(600 * time.Millisecond):
		}
	}
	cancel()
	m.mu.Lock()
	if m.epoch == epoch && !relay {
		m.state.Status = "Открываем P2P-туннель"
	}
	m.mu.Unlock()
	link, cancelLink := context.WithTimeout(ctx, tunnelBudget)
	defer cancelLink()
	traceResult = "tunnel_failed"
	port, err := peer.Listen(link)
	if err != nil {
		return fmt.Errorf("%w: открытие P2P-туннеля: %w", errDirectTunnel, err)
	}
	traceResult = "ready"
	finishTrace(result.Peer.ID, peer, traceResult)
	announce, cancelAnnounce := context.WithCancel(m.ctx)
	m.mu.Lock()
	if m.epoch != epoch || m.guest != result.Peer.ID {
		m.mu.Unlock()
		cancelAnnounce()
		return context.Canceled
	}
	m.roomCancel = cancelAnnounce
	m.state.Room = &result.Room
	m.guestPort = port
	m.state.Status = "Вы в лобби"
	m.mu.Unlock()
	go func() {
		defer cancelAnnounce()
		<-peer.Closed()
		if recovery, ok := m.finishGuest(epoch, result.Peer.ID, peer); ok {
			m.recoverGuestLobby(recovery)
		}
	}()
	success = true
	exchange, cancelExchange := context.WithTimeout(ctx, 3*time.Second)
	_, _ = m.exchangeLobby(exchange, peer, epoch)
	cancelExchange()
	go m.watchGuestLobby(announce, peer, epoch, result.Host)
	return m.prepareGuestPack(ctx, peer, epoch, result.Host, fromInvite)
}

// A previous session may finish after a new join. Only its own generation may
// release the advertised endpoint or change the currently visible room status.
func (m *socialManager) finishGuest(epoch uint64, id string, peer *p2p.Peer) (guestLobbyRecovery, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.epoch != epoch || m.guest != id || m.peers[id] != peer {
		return guestLobbyRecovery{}, false
	}
	recovery := m.guestRecoveryLocked()
	reconnect := m.guestPort != 0 && recovery.guarded && recovery.code != "" &&
		time.Since(m.lastGuestRecovery) >= 45*time.Second
	if reconnect {
		m.lastGuestRecovery = time.Now()
	}
	delete(m.peers, id)
	m.state.LocalPort = 0
	m.guestPort = 0
	m.guestWorldDigest = ""
	m.state.WorldReady = false
	m.state.InWorld = false
	m.state.AutoConnect = recovery.autoConnect
	m.gameCommandPort = 0
	m.gameJoinUntil = time.Time{}
	m.state.Ping = 0
	if m.state.Room == nil {
		m.guest = ""
		m.state.HostName = ""
	}
	m.state.Status = "Связь прервалась. Подключись повторно"
	if m.packCancel != nil {
		m.packCancel()
		m.packCancel = nil
	}
	m.guestPack = nil
	m.state.Pack = nil
	return recovery, reconnect
}

func (m *socialManager) action(ctx context.Context, action, target string) error {
	m.actions.Lock()
	defer m.actions.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	m.identity()
	if !m.snapshot().Enabled {
		return errors.New("войдите через Sintet ID")
	}
	m.mu.Lock()
	syncing := m.packCancel != nil
	m.mu.Unlock()
	if syncing && action != "close" && action != "cancel-pack" && action != "auto-connect" {
		return errors.New("дождитесь синхронизации сборки или отмените её")
	}
	m.mu.Lock()
	m.state.Busy = true
	m.mu.Unlock()
	defer func() { m.mu.Lock(); m.state.Busy = false; m.mu.Unlock() }()
	var err error
	switch action {
	case "auto-connect":
		m.mu.Lock()
		m.state.AutoConnect = target == "true"
		m.mu.Unlock()
	case "reconnect-lobby":
		m.mu.Lock()
		recovery := m.guestRecoveryLocked()
		m.mu.Unlock()
		if recovery.code == "" {
			err = errors.New("сначала войди в лобби")
		} else {
			err = m.resumeGuestLobby(ctx, recovery)
		}
	case "launch-host":
		err = m.launchLobbyHost(ctx)
	case "refresh-pack":
		err = m.refreshLobbyPack(ctx)
	case "change-lobby-pack":
		err = m.changeLobbyPack(ctx, target)
	case "retry-pack":
		err = m.retryGuestPack(ctx)
	case "connect-game":
		err = m.connectPreparedPack(ctx)
	case "sync-pack":
		err = m.startPackSync(target)
	case "cancel-pack":
		m.mu.Lock()
		m.packConsent = ""
		cancel := m.packCancel
		m.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	case "host":
		err = m.host(ctx, target)
	case "join":
		err = m.join(ctx, target)
	case "close":
		m.mu.Lock()
		guest := m.guest
		m.mu.Unlock()
		m.disconnect()
		if guest != "" {
			err = m.post(ctx, map[string]any{"action": "peer-leave", "peer": guest}, nil)
		} else {
			err = m.post(ctx, map[string]any{"action": "room-close"}, nil)
		}
	case "kick":
		state := m.snapshot()
		if !state.Leader || target == state.Room.Host {
			err = errors.New("исключать участников может только лидер лобби")
			break
		}
		err = m.post(ctx, map[string]any{"action": "kick", "target": target, "code": state.Room.Code}, nil)
		if err == nil {
			m.mu.Lock()
			var kicked *p2p.Peer
			for _, entry := range state.Peers {
				if entry.Guest == target {
					kicked = m.peers[entry.ID]
					delete(m.peers, entry.ID)
					break
				}
			}
			m.mu.Unlock()
			if kicked != nil {
				kicked.Close()
			}
		}
	case "invite":
		for _, member := range m.snapshot().Members {
			if member.ID == target {
				return errors.New("друг уже в лобби")
			}
		}
		if m.snapshot().Room == nil {
			err = m.host(ctx)
		}
		if err == nil {
			err = m.post(ctx, map[string]any{"action": "invite", "target": target}, nil)
		}
	case "invite-accept", "invite-decline":
		if action == "invite-accept" && m.app.State().Busy {
			return errors.New("дождитесь завершения текущего запуска или установки")
		}
		var reply struct {
			Code string `json:"code"`
		}
		err = m.post(ctx, map[string]any{"action": action, "invite": target}, &reply)
		if err == nil && action == "invite-accept" {
			err = m.joinRoom(ctx, reply.Code, true)
		}
	case "request", "friend-accept", "friend-remove":
		err = m.post(ctx, map[string]any{"action": action, "target": target}, nil)
	default:
		err = errors.New("неизвестное действие")
	}
	if err != nil {
		m.mu.Lock()
		if (action == "join" || action == "invite-accept" || action == "reconnect-lobby") && m.guestPort == 0 {
			m.state.Status = "Не удалось подключиться"
		}
		m.mu.Unlock()
	}
	m.error(err)
	if err == nil {
		if action == "host" || action == "join" || action == "close" {
			m.sendDiagnostic(action, nil)
		}
		if action == "invite-accept" {
			m.sendDiagnostic("connected", nil)
		}
	}
	if err == nil {
		if action == "join" || action == "invite-accept" || action == "reconnect-lobby" {
			go m.refresh()
		} else {
			m.refresh()
		}
		return nil
	}
	return errors.New(playerFacingSocialError(err))
}
func (a *App) FriendsState() socialState {
	if a.social == nil {
		return socialState{}
	}
	state := a.social.snapshot()
	state.GameRunning = a.State().Running
	return state
}
func (a *App) FriendsAction(action, target string) error {
	if a.social == nil {
		return errors.New("сервис запускается")
	}
	return a.social.action(a.social.ctx, action, target)
}
func (a *App) FriendsSearch(query string) ([]socialAccount, error) {
	if a.social == nil {
		return nil, errors.New("сервис запускается")
	}
	var reply struct {
		Accounts []socialAccount `json:"accounts"`
	}
	err := a.social.request(a.social.ctx, "GET", "/api/social/search?q="+url.QueryEscape(query), nil, &reply)
	if err == nil {
		for i := range reply.Accounts {
			reply.Accounts[i].AvatarURL = a.social.avatarURL(reply.Accounts[i].ID, reply.Accounts[i].AvatarDigest)
		}
	}
	return reply.Accounts, err
}
func (m *socialManager) startIPC() error {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return err
	}
	var secret [32]byte
	if _, err = rand.Read(secret[:]); err != nil {
		listener.Close()
		return err
	}
	m.address = listener.Addr().String()
	m.secret = hex.EncodeToString(secret[:])
	m.server = &http.Server{ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 3 * time.Second, WriteTimeout: 110 * time.Second, IdleTimeout: 10 * time.Second, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Host != m.address || r.Header.Get("Origin") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+m.secret)) != 1 {
			http.Error(w, "Forbidden", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		if r.Method == "GET" && r.URL.Path == "/avatar" {
			id := r.URL.Query().Get("id")
			address := ""
			state := m.snapshot()
			for _, f := range state.Friends {
				if f.Account != nil && f.Account.ID == id {
					address = f.Account.AvatarURL
					break
				}
			}
			for _, member := range state.Members {
				if member.ID == id {
					address = member.AvatarURL
					break
				}
			}
			if address == "" {
				http.NotFound(w, r)
				return
			}
			data, _, err := m.app.avatarBytes(address)
			if err != nil {
				http.Error(w, "Avatar unavailable", 503)
				return
			}
			pixels, err := overlayAvatarPNG(data, false)
			if err != nil {
				http.Error(w, "Avatar unavailable", 503)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"pixels": pixels})
			return
		}
		if r.Method == "GET" && (r.URL.Path == "/game/command" || r.URL.Path == "/game/presence") {
			m.observeBridgeActivity(r.Header)
			if r.URL.Path == "/game/presence" {
				fmt.Fprint(w, "ok")
				return
			}
			id, port := m.pollGameCommand(r.Header.Get("X-Sintet-Join-Ready") == "1")
			w.Header().Set("Content-Type", "text/plain")
			fmt.Fprintf(w, "%d %d", id, port)
			return
		}
		if r.Method == "POST" && r.URL.Path == "/game/result" {
			var id uint64
			var result string
			_, err := fmt.Fscan(http.MaxBytesReader(w, r.Body, 128), &id, &result)
			m.mu.Lock()
			if err == nil && id == m.gameCommand && m.gameCommandEpoch == m.epoch && m.gameCommandPort == m.state.LocalPort && m.gameCommandPort != 0 {
				m.gameCommandPort = 0
				if result == "ok" {
					m.gameJoinUntil = time.Now().Add(50 * time.Second)
					m.state.Status = "Minecraft подключается к миру"
					m.state.Error = ""
				} else {
					m.state.Error = "Minecraft не выполнил вход. Выйдите в главное меню и повторите; подробности в журнале игры."
				}
			}
			m.mu.Unlock()
			fmt.Fprint(w, "ok")
			return
		}
		if r.Method == "GET" && r.URL.Path == "/state" {
			json.NewEncoder(w).Encode(m.snapshot())
			return
		}
		if r.Method == "POST" && r.URL.Path == "/action" {
			var body struct {
				Action string `json:"action"`
				Target string `json:"target"`
			}
			if json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&body) != nil {
				http.Error(w, "Invalid request", 400)
				return
			}
			if body.Action == "request" || body.Action == "friend-accept" {
				json.NewEncoder(w).Encode(map[string]string{"error": "Добавляйте друзей в лаунчере"})
				return
			}
			err := m.action(m.ctx, body.Action, body.Target)
			if err != nil {
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"ok": true})
			return
		}
		http.NotFound(w, r)
	})}
	go m.server.Serve(listener)
	go func() { <-m.ctx.Done(); m.server.Close() }()
	return nil
}

// Only the currently established local P2P tunnel can become a game command.
func (m *socialManager) connectGame() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.state.LocalPort <= 0 || m.state.LocalPort > 65535 {
		return errors.New("сначала подключитесь к лобби и дождитесь P2P-соединения")
	}
	if time.Since(m.bridgeSeen) > 3*time.Second {
		return errors.New("игровой мост недоступен: запустите Minecraft из обновлённого Sintet")
	}
	if m.gameJoinPendingLocked() {
		return nil
	}
	m.setGameCommandLocked(m.state.LocalPort, false, 15*time.Second)
	m.state.Status = "Передаём подключение в Minecraft"
	return nil
}

func (m *socialManager) gameJoinPendingLocked() bool {
	if m.gameCommandEpoch != m.epoch || m.state.LocalPort == 0 {
		return false
	}
	now := time.Now()
	return m.gameCommandPort == m.state.LocalPort && now.Before(m.gameCommandUntil) || now.Before(m.gameJoinUntil)
}

func (m *socialManager) setGameCommandLocked(port int, waitForMenu bool, timeout time.Duration) uint64 {
	m.gameCommand++
	m.gameCommandPort = port
	m.gameCommandEpoch = m.epoch
	m.gameJoinUntil = time.Time{}
	m.gameCommandMenu = waitForMenu
	m.gameCommandUntil = time.Now().Add(timeout)
	return m.gameCommand
}

func (m *socialManager) pollGameCommand(menuReady bool) (uint64, int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.bridgeSeen = time.Now()
	id, port := m.gameCommand, m.gameCommandPort
	if m.gameCommandEpoch != m.epoch || port != m.state.LocalPort ||
		!m.bridgeSeen.Before(m.gameCommandUntil) || m.gameCommandMenu && !menuReady {
		port = 0
	}
	return id, port
}

func (m *socialManager) queueColdLobbyJoin(plan *guestPack, port int) {
	const timeout = 4 * time.Minute
	m.mu.Lock()
	if m.guestPack != plan || m.epoch != plan.epoch || m.state.LocalPort != port || !plan.peer.Connected() {
		m.mu.Unlock()
		return
	}
	id := m.setGameCommandLocked(port, true, timeout)
	m.state.Status = "Ждём главное меню Minecraft для подключения"
	m.mu.Unlock()

	time.AfterFunc(timeout, func() {
		running := m.app.State().Running
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.epoch != plan.epoch || m.gameCommand != id || m.gameCommandPort == 0 {
			return
		}
		m.gameCommandPort = 0
		if running && m.state.LocalPort == port {
			m.state.Error = "Minecraft не подтвердил готовность. Повтори вход из оверлея."
		}
	})
}

func (m *socialManager) avatarURL(id, digest string) string {
	if m.app.avatarDB == nil || !avatarAccountID.MatchString(id) || !avatarDigest.MatchString(digest) {
		return ""
	}
	return strings.TrimRight(m.app.avatarDB.ImageOrigin, "/") + "/avatars/" + id + "/" + digest + ".png"
}
