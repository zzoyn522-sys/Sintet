package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pion/webrtc/v4"
	"sintet/internal/identity"
	p2p "sintet/internal/social"
)

func TestTURNLimitClosesRelayAndStopsLobbyRecovery(t *testing.T) {
	account := identity.Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "Fixture"}
	token := strings.Repeat("t", 43)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/launcher/token":
			_ = json.NewEncoder(w).Encode(map[string]any{"account": account, "token": token, "expires": time.Now().Add(time.Hour)})
		case "/api/social/turn-usage":
			if r.Header.Get("Authorization") != "Bearer "+token {
				http.Error(w, "Missing authentication", http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(turnUsageView{Used: 5_000_000_000, Limit: 5_000_000_000, PeriodStart: "2026-11-01"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client := identity.New(server.URL, filepath.Join(t.TempDir(), "session.dpapi"))
	address, err := client.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, strings.Repeat("c", 43))
	peer, err := p2p.NewMeteredRelayPeer(0, []webrtc.ICEServer{{
		URLs: []string{"turn:127.0.0.1:3478"}, Username: "fixture", Credential: "fixture",
	}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer peer.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	social := newSocialManager(nil, ctx)
	social.account = account.ID
	social.peers["guest"] = peer
	social.guest = "guest"
	social.guestPort = 25565
	social.state.LocalPort = social.guestPort
	social.state.Room = &socialRoom{Code: "123456", Host: "host"}
	social.state.ResumeLobby = true
	social.state.AutoConnect = true

	go social.watchTURNPeer(client, account.ID, peer)
	select {
	case <-peer.Closed():
	case <-ctx.Done():
		t.Fatal("relay remained open after quota was reached")
	}
	if _, reconnect := social.finishGuest(0, "guest", peer); reconnect {
		t.Fatal("launcher tried to recover an exhausted TURN session")
	}
	state := social.snapshot()
	if state.TURNNotice == 0 || !state.TURNUsage.exhausted() || state.LocalPort != 0 || state.AutoConnect || state.Status != turnLimitMessage {
		t.Fatalf("unexpected lobby state after quota was reached: %+v", state)
	}
}
