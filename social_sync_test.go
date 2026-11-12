package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sintet/internal/identity"
	"sintet/internal/launcher"
	p2p "sintet/internal/social"
)

func TestLauncherInvitationRetriesSignalingAndKeepsErrorShort(t *testing.T) {
	const shortError = "Не удалось подключиться. Попробуйте ещё раз."
	const trace = "WebRTC закрыт: ICE=failed, DTLS=failed | trace=checking;failed"
	if message := playerFacingSocialError(fmt.Errorf("%w: %s", errDirectTunnel, trace)); message != shortError {
		t.Fatalf("WebRTC diagnostic shown to the player: %q", message)
	}

	var accepted, joins, leaves atomic.Int32
	account := identity.Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "Fixture"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/launcher/token" {
			json.NewEncoder(w).Encode(map[string]any{"account": account, "token": strings.Repeat("t", 43), "expires": time.Now().Add(time.Hour)})
			return
		}
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		switch body["action"] {
		case "invite-accept":
			accepted.Add(1)
			json.NewEncoder(w).Encode(map[string]string{"code": "123456"})
		case "room-join":
			peer := fmt.Sprint(joins.Add(1))
			json.NewEncoder(w).Encode(map[string]any{"room": socialRoom{Code: "123456", Host: "host"}, "peer": socialPeer{ID: peer}})
		case "signal":
			w.WriteHeader(http.StatusServiceUnavailable)
		case "peer-leave":
			leaves.Add(1)
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	root := t.TempDir()
	client := identity.New(server.URL, filepath.Join(root, "session.dpapi"))
	address, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, strings.Repeat("c", 43))
	app := &App{profiles: newProfileRoster(root, server.URL), service: launcher.New(t.TempDir())}
	app.profiles.slots = []*profileSlot{{profileRecord: profileRecord{Key: "fixture", Provider: "sintet"}, sintet: client}}
	app.profiles.active = "fixture"
	app.social = newSocialManager(app, ctx)
	if err := app.FriendsAction("invite-accept", "invite"); err == nil || err.Error() != shortError {
		t.Fatalf("unexpected invitation error: %v", err)
	}
	if accepted.Load() != 1 || joins.Load() != 2 || leaves.Load() != 0 {
		t.Fatalf("accepts=%d joins=%d leaves=%d", accepted.Load(), joins.Load(), leaves.Load())
	}
	state := app.FriendsState()
	if state.Busy || state.JoinAttempt != 0 || state.Error != shortError || state.LocalPort != 0 {
		t.Fatalf("unexpected state after retry exhaustion: %+v", state)
	}
	log, err := os.ReadFile(filepath.Join(root, "p2p.log"))
	if err != nil || !strings.Contains(string(log), "HTTP 503") {
		t.Fatalf("missing local diagnostic: %q, %v", log, err)
	}
}

func TestSocialNetworkInterruptionPreservesLobbyAndRecovers(t *testing.T) {
	var healthy atomic.Bool
	const id = "ba815a63-4176-45db-8e70-12e3801c1207"
	room := &socialRoom{Code: "123456", Host: "another-account", Version: "1.21.1"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/launcher/token" {
			json.NewEncoder(w).Encode(map[string]any{"account": identity.Account{ID: id, Username: "Fixture"}, "token": strings.Repeat("t", 43), "expires": time.Now().Add(time.Hour)})
			return
		}
		if !healthy.Load() {
			w.WriteHeader(503)
			return
		}
		json.NewEncoder(w).Encode(socialRemote{Room: room, Peers: []socialPeer{{ID: "guest"}}, Friends: []socialFriend{{ID: "friendship", Status: "accepted", Account: &socialAccount{ID: id, Username: "Fixture"}}}})
	}))
	defer server.Close()
	client := identity.New(server.URL, filepath.Join(t.TempDir(), "session.dpapi"))
	address, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, strings.Repeat("c", 43))
	var ignored any
	err = client.SocialRequest(context.Background(), "GET", "/api/social", nil, &ignored)
	if !identity.TemporaryServiceError(err) {
		t.Fatal("expected classified outage", err)
	}
	m := newSocialManager(&App{}, context.Background())
	m.client, m.account, m.epoch, m.guest = client, id, 1, "guest"
	m.peers = map[string]*p2p.Peer{"guest": nil}
	m.state = socialState{Enabled: true, LocalPort: 54321, Status: "Подключено", socialRemote: socialRemote{Room: room, Friends: []socialFriend{{ID: "cached-friend"}}}}
	for i := 0; i < 2; i++ {
		m.syncFailure(1, err)
		if m.state.Error != "" || !m.state.Reconnecting || m.state.Room == nil || m.state.LocalPort != 54321 || len(m.state.Friends) != 1 || len(m.peers) != 1 {
			t.Fatal("short outage erased the active lobby", m.state)
		}
	}
	m.syncFailure(0, err)
	if m.syncFailures != 2 {
		t.Fatal("old room changed current sync health")
	}
	m.syncFailure(1, err)
	if m.state.Error == "" {
		t.Fatal("persistent outage was hidden")
	}
	healthy.Store(true)
	m.refresh()
	if m.state.Reconnecting || m.state.Error != "" || m.syncFailures != 0 || m.state.LocalPort != 54321 || m.state.Room == nil || len(m.peers) != 1 {
		t.Fatal("successful refresh did not recover without dropping P2P", m.state)
	}
}
