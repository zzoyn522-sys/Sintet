package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sintet/internal/launcher"
	p2p "sintet/internal/social"
)

func lobbyFixture(t *testing.T) *App {
	t.Helper()
	root := t.TempDir()
	app := &App{profiles: &profileRoster{root: root}, service: launcher.New(filepath.Join(root, "Minecraft")), settings: launcher.DefaultSettings(filepath.Join(root, "Minecraft"))}
	java := filepath.Join(root, "fixture-java.exe")
	if err := os.WriteFile(java, []byte("fixture only, never execute"), 0600); err != nil {
		t.Fatal(err)
	}
	receipt := launcher.Receipt{ID: "1.21.1", Base: "1.21.1", Loader: "vanilla", Java: java}
	path := filepath.Join(app.service.Root, ".sintet", "installed", "1.21.1.json")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(receipt)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	return app
}

func TestStableReleaseSupportsPreGameHosting(t *testing.T) {
	previous := launcherChannel
	launcherChannel = ""
	defer func() { launcherChannel = previous }()
	m := newSocialManager(lobbyFixture(t), context.Background())
	if !m.snapshot().PreGameLobby {
		t.Fatal("stable did not advertise pre-game lobby")
	}
	if profile, err := m.lobbyHostProfile("1.21.1"); err != nil || profile != "1.21.1" {
		t.Fatal("stable failed to select a pack before launching the game", err)
	}
}

func TestHostPackCanBePreparedWithoutMinecraftAndRoomSurvivesWorldClose(t *testing.T) {
	previous := launcherChannel
	launcherChannel = "test"
	defer func() { launcherChannel = previous }()
	app := lobbyFixture(t)
	m := newSocialManager(app, context.Background())
	profile, err := m.lobbyHostProfile("1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := m.captureHostPack(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Manifest.Game != "1.21.1" || app.State().Running {
		t.Fatal("host preparation requires a running game")
	}
	m.account = "host"
	m.hostProfile = profile
	m.hostPack = &snapshot
	m.state.Room = &socialRoom{Code: "123456", Host: "host"}
	m.state.Peers = []socialPeer{{ID: "peer", Guest: "guest"}}
	m.state.Members = []socialMember{{ID: "host", Leader: true}, {ID: "guest"}}
	m.state.WorldReady = true
	m.updateHostedWorld(0)
	if m.state.Room == nil || m.hostPack == nil || m.state.WorldReady {
		t.Fatal("closing the world removed the lobby")
	}
	handler := m.hostLobbyHandler("guest")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/lobby", strings.NewReader(`{"phase":"downloading","percent":42,"digest":"`+snapshot.Manifest.Digest+`"}`)))
	var state lobbyExchange
	if err := json.Unmarshal(w.Body.Bytes(), &state); err != nil || w.Code != 200 || state.WorldReady || len(state.Members) != 1 || state.Members[0].Percent != 42 {
		t.Fatal("lobby state did not retain preparation", w.Code, w.Body.String(), err)
	}
	w = httptest.NewRecorder()
	m.hostLobbyHandler("outsider").ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/lobby", strings.NewReader(`{"phase":"ready"}`)))
	if w.Code != 403 {
		t.Fatal("outsider can access lobby metadata")
	}
	w = httptest.NewRecorder()
	handler.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/lobby", strings.NewReader(`{"phase":"ready","percent":101}`)))
	if w.Code != 400 {
		t.Fatal("invalid progress accepted")
	}
}

func TestPreparedGuestWaitsForHostInsteadOfLaunchingGame(t *testing.T) {
	previous := launcherChannel
	launcherChannel = "test"
	defer func() { launcherChannel = previous }()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	hostManager := newSocialManager(lobbyFixture(t), ctx)
	snapshot, err := hostManager.captureHostPack(ctx, "1.21.1")
	if err != nil {
		t.Fatal(err)
	}
	hostManager.account = "host"
	hostManager.hostProfile = "1.21.1"
	hostManager.hostPack = &snapshot
	hostManager.state.Room = &socialRoom{Code: "123456", Host: "host"}
	hostManager.state.Peers = []socialPeer{{ID: "peer", Guest: "guest"}}
	host, err := p2p.NewPeer(-1, false)
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	host.SetPackHandler(hostManager.hostLobbyHandler("guest"))
	peer, err := p2p.NewPeer(0, false)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	offer, err := peer.Offer(ctx)
	if err != nil {
		t.Fatal(err)
	}
	answer, err := host.Answer(ctx, *offer)
	if err != nil {
		t.Fatal(err)
	}
	if err = peer.SetAnswer(*answer); err != nil {
		t.Fatal(err)
	}
	port, err := peer.Listen(ctx)
	if err != nil {
		t.Fatal(err)
	}
	guest := newSocialManager(lobbyFixture(t), ctx)
	guest.epoch = 1
	guest.guest = "peer"
	guest.guestPort = port
	guest.peers["peer"] = peer
	guest.state.Room = hostManager.state.Room
	if _, err = guest.exchangeLobby(ctx, peer, 1); err != nil {
		t.Fatal(err)
	}
	if err = guest.prepareGuestPack(ctx, peer, 1, socialAccount{ID: "host"}, true); err != nil {
		t.Fatal(err)
	}
	state := guest.snapshot()
	if state.Pack == nil || state.Pack.Phase != "ready" || state.WorldReady || state.LocalPort != 0 || guest.app.State().Running || guest.app.State().Busy {
		t.Fatalf("prepared guest started Minecraft before world: %+v", state)
	}
	if err = guest.connectPreparedPack(ctx); err == nil || !strings.Contains(err.Error(), "лидер") {
		t.Fatal("join allowed before LAN world", err)
	}
	hostManager.mu.Lock()
	hostManager.state.WorldReady = true
	hostManager.mu.Unlock()
	if _, err = guest.exchangeLobby(ctx, peer, 1); err != nil {
		t.Fatal(err)
	}
	if !guest.snapshot().WorldReady || guest.snapshot().LocalPort != port || guest.app.State().Running {
		t.Fatal("ready world should expose join without automatic launch")
	}
	guest.setGameCommandLocked(port, true, time.Minute)
	guest.state.Status = "Ждём главное меню Minecraft для подключения"
	if _, err = guest.exchangeLobby(ctx, peer, 1); err != nil {
		t.Fatal(err)
	}
	if state := guest.snapshot(); !state.GameJoining || state.Status != "Ждём главное меню Minecraft для подключения" {
		t.Fatalf("lobby polling erased the Minecraft menu stage: %+v", state)
	}
	guest.gameCommandPort = 0
	guest.gameJoinUntil = time.Now().Add(time.Minute)
	if _, err = guest.exchangeLobby(ctx, peer, 1); err != nil {
		t.Fatal(err)
	}
	if state := guest.snapshot(); !state.GameJoining || state.Status != "Minecraft подключается к миру" {
		t.Fatalf("lobby polling erased the game join stage: %+v", state)
	}
	hostManager.mu.Lock()
	hostManager.state.WorldReady = false
	hostManager.mu.Unlock()
	if _, err = guest.exchangeLobby(ctx, peer, 1); err != nil {
		t.Fatal(err)
	}
	if guest.snapshot().Room == nil || guest.snapshot().Pack.Phase != "ready" || guest.snapshot().LocalPort != 0 || guest.snapshot().GameJoining {
		t.Fatal("world closing lost prepared guest")
	}
	hostManager.mu.Lock()
	next := snapshot
	next.Manifest.Digest = strings.Repeat("b", 64)
	hostManager.hostPack = &next
	hostManager.state.WorldReady = true
	hostManager.mu.Unlock()
	if digest, err := guest.exchangeLobby(ctx, peer, 1); err != nil || digest != next.Manifest.Digest {
		t.Fatal("guest did not see the host's new pack", err)
	}
	if state := guest.snapshot(); !state.LobbyConnected || state.LocalPort != 0 {
		t.Fatal("guest should retain membership and wait for the new pack")
	}
}
