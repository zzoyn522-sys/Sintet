package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"testing"
	"time"

	p2p "sintet/internal/social"
)

func TestMixedLobbyAdmissionIsBoundToTheGameSocket(t *testing.T) {
	game, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	defer game.Close()
	port := game.Addr().(*net.TCPAddr).Port
	manager := newSocialManager(nil, t.Context())
	manager.account = "host"
	manager.state.Room = &socialRoom{Host: "host", MixedLogin: true}
	manager.state.LANPort, manager.state.WorldReady = port, true
	manager.bridgeMixedSeen = time.Now()
	peer := &p2p.Peer{}
	guest := socialPeer{ID: "peer", Guest: "guest", GameProfile: &lobbyGameProfile{
		ID: "18d87fe6-ff41-807f-8c3c-d8d9c197ab7e", Name: "ElyPlayer",
	}}
	manager.state.Peers = []socialPeer{guest}
	manager.peers[guest.ID] = peer
	if err := manager.startIPC(); err != nil {
		t.Fatal(err)
	}
	open := func() int {
		t.Helper()
		connection, release, err := manager.dialMixedLobby(t.Context(), guest, peer, manager.epoch, port)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { connection.Close(); release() })
		if err := game.SetDeadline(time.Now().Add(time.Second)); err != nil {
			t.Fatal(err)
		}
		got, err := game.AcceptTCP()
		if err != nil {
			t.Fatal(err)
		}
		defer got.Close()
		remote := got.RemoteAddr().(*net.TCPAddr)
		if !remote.IP.Equal(net.IPv4(127, 0, 0, 2)) || remote.Port != connection.LocalAddr().(*net.TCPAddr).Port {
			t.Fatalf("listener got unexpected remote endpoint %v", remote)
		}
		return remote.Port
	}
	check := func(port int, name, secret string, wantStatus int, wantBody string) {
		t.Helper()
		address := fmt.Sprintf("http://%s/game/lobby-login?port=%d&name=%s", manager.address, port, name)
		request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, address, nil)
		if err != nil {
			t.Fatal(err)
		}
		request.Header.Set("Authorization", "Bearer "+secret)
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != wantStatus || wantBody != "" && string(body) != wantBody {
			t.Fatalf("login status %d, body %q, err %v", response.StatusCode, body, err)
		}
	}
	source := open()
	check(source, "ElyPlayer", "", http.StatusForbidden, "")
	check(source, "ElyPlayer", manager.secret, http.StatusOK, guest.GameProfile.ID)
	check(source, "ElyPlayer", manager.secret, http.StatusForbidden, "")
	source = open()
	check(source, "OtherPlayer", manager.secret, http.StatusForbidden, "")
	check(source, "ElyPlayer", manager.secret, http.StatusForbidden, "")
	source = open()
	manager.mu.Lock()
	delete(manager.peers, guest.ID)
	manager.mu.Unlock()
	check(source, "ElyPlayer", manager.secret, http.StatusForbidden, "")
}
