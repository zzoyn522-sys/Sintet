package main

import (
	"context"
	"errors"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"

	p2p "sintet/internal/social"
)

type lobbyGameProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

type lobbyGameAdmission struct {
	remote socialPeer
	peer   *p2p.Peer
	epoch  uint64
	port   int
	until  time.Time
}

var lobbyGameName = regexp.MustCompile(`^[A-Za-z0-9_]{1,16}$`)
var lobbyGameUUID = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)

func lobbyProvider(provider string) string {
	if provider == "" {
		return "sintet"
	}
	return provider
}

func (m *socialManager) mixedPeerLocked(peer socialPeer) bool {
	if m.state.Room == nil || !m.state.Room.MixedLogin {
		return false
	}
	for _, member := range m.state.Members {
		if member.ID == peer.Guest {
			return lobbyProvider(member.Provider) != lobbyProvider(m.state.LobbyProvider)
		}
	}
	// A missing directory entry cannot establish that native login is compatible.
	return true
}

func (m *socialManager) mixedWorldReadyLocked() bool {
	return m.state.WorldReady && m.state.LANPort > 0 && time.Since(m.bridgeMixedSeen) < 4*time.Second
}

func (m *socialManager) validMixedAdmissionLocked(admission *lobbyGameAdmission) bool {
	room := m.state.Room
	if room == nil || room.Host != m.account || !room.MixedLogin ||
		m.epoch != admission.epoch || m.peers[admission.remote.ID] != admission.peer ||
		m.state.LANPort != admission.port || time.Now().After(admission.until) || !m.mixedWorldReadyLocked() {
		return false
	}
	for _, current := range m.state.Peers {
		if current.ID == admission.remote.ID && current.Guest == admission.remote.Guest &&
			current.GameProfile != nil && *current.GameProfile == *admission.remote.GameProfile {
			return true
		}
	}
	return false
}

func (m *socialManager) dialMixedLobby(ctx context.Context, remote socialPeer, peer *p2p.Peer, epoch uint64, port int) (net.Conn, func(), error) {
	if remote.GameProfile == nil || !lobbyGameUUID.MatchString(remote.GameProfile.ID) || !lobbyGameName.MatchString(remote.GameProfile.Name) {
		return nil, nil, errors.New("lobby member has no verified game identity")
	}
	admission := &lobbyGameAdmission{remote: remote, peer: peer, epoch: epoch, port: port, until: time.Now().Add(20 * time.Second)}
	m.mu.Lock()
	valid := m.validMixedAdmissionLocked(admission)
	m.mu.Unlock()
	if !valid {
		return nil, nil, errors.New("mixed lobby is unavailable")
	}
	dialer := net.Dialer{Timeout: 5 * time.Second, LocalAddr: &net.TCPAddr{IP: net.IPv4(127, 0, 0, 2)}}
	target, err := dialer.DialContext(ctx, "tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(port)))
	if err != nil {
		return nil, nil, err
	}
	source := target.LocalAddr().(*net.TCPAddr).Port
	m.mu.Lock()
	defer m.mu.Unlock()
	if !m.validMixedAdmissionLocked(admission) || m.lobbyAdmissions[source] != nil {
		target.Close()
		return nil, nil, errors.New("mixed lobby admission expired")
	}
	if m.lobbyAdmissions == nil {
		m.lobbyAdmissions = make(map[int]*lobbyGameAdmission)
	}
	m.lobbyAdmissions[source] = admission
	release := func() {
		m.mu.Lock()
		defer m.mu.Unlock()
		if m.lobbyAdmissions[source] == admission {
			delete(m.lobbyAdmissions, source)
		}
	}
	return target, release, nil
}

func (m *socialManager) consumeLobbyAdmission(port int, name string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	admission := m.lobbyAdmissions[port]
	if admission == nil || !m.validMixedAdmissionLocked(admission) {
		return ""
	}
	delete(m.lobbyAdmissions, port)
	profile := admission.remote.GameProfile
	if !strings.EqualFold(name, profile.Name) {
		return ""
	}
	return profile.ID
}
