package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"sintet/internal/identity"
)

func TestPresenceSendsChangesAndKeepsOnlineHeartbeat(t *testing.T) {
	const accountID = "ba815a63-4176-45db-8e70-12e3801c1207"
	var requests atomic.Int32
	var reject atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/launcher/token":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"account": identity.Account{ID: accountID, Username: "Fixture"},
				"token":   strings.Repeat("t", 43), "expires": time.Now().Add(time.Hour),
			})
		case "/api/social":
			requests.Add(1)
			if reject.Load() {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusNoContent)
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
	m := newSocialManager(&App{}, context.Background())
	m.client, m.account = client, accountID

	publish := func(activity socialActivity, wantRequests int32) {
		t.Helper()
		if err := m.publishPresence(activity, activity.legacyStatus(), activity.Version); err != nil {
			t.Fatal(err)
		}
		if got := requests.Load(); got != wantRequests {
			t.Fatalf("requests after %s = %d, want %d", activity.Mode, got, wantRequests)
		}
	}

	idle := socialActivity{Mode: "launcher"}
	publish(idle, 1)
	publish(idle, 1)
	m.lastPresence.sentAt = time.Now().Add(-presenceHeartbeatInterval)
	publish(idle, 2)

	playing := socialActivity{Mode: "singleplayer", Version: "1.20.1"}
	reject.Store(true)
	if err := m.publishPresence(playing, playing.legacyStatus(), playing.Version); err == nil {
		t.Fatal("expected status update to fail")
	}
	reject.Store(false)
	publish(playing, 4)
	publish(playing, 4)

	m.state.Room = &socialRoom{Code: "123456"}
	publish(socialActivity{Mode: "lobby", Version: "1.20.1"}, 5)
	m.state.Privacy.Invisible = true
	publish(playing, 6)
	publish(playing, 6)
	m.epoch++
	publish(playing, 7)
}

func TestRichPresenceDoesNotShareLocalAddresses(t *testing.T) {
	for _, address := range []string{"127.0.0.1:25565", "192.168.1.8", "[fd00::4]:25565", "minecraft.local:25565"} {
		if label := publicServerLabel("Private Server", address); label != "" {
			t.Fatalf("published a local server label for %q", address)
		}
	}
	if label := publicServerLabel("Minecraft Server", "Play.Minecraft.net:25565"); label != "play.minecraft.net" {
		t.Fatalf("unexpected public hostname %q", label)
	}
	if label := publicServerLabel("§aCastle\x00", "play.minecraft.net"); label != "Castle" {
		t.Fatalf("unexpected named server %q", label)
	}
}

func TestRichPresenceClearsServerOnReturnToMenu(t *testing.T) {
	m := &socialManager{}
	headers := http.Header{}
	headers.Set("X-Sintet-World", "1")
	headers.Set("X-Sintet-Activity", "multiplayer")
	headers.Set("X-Sintet-Server-Address", base64.RawURLEncoding.EncodeToString([]byte("play.minecraft.net:25565")))
	m.observeBridgeActivity(headers)
	if m.bridgeMode != "multiplayer" || m.bridgeServer != "play.minecraft.net" {
		t.Fatalf("invalid bridge activity: %q, %q", m.bridgeMode, m.bridgeServer)
	}
	headers.Set("X-Sintet-World", "0")
	m.observeBridgeActivity(headers)
	if m.bridgeMode != "menu" || m.bridgeServer != "" || m.bridgeWorld {
		t.Fatal("server activity was retained in the main menu")
	}
}

func TestRichPresenceLogFallbackRequiresConnection(t *testing.T) {
	m := &socialManager{}
	m.observeGameLog([]byte("[Render thread/INFO]: [CHAT] Connecting to fake.minecraft.net, 25565\n"))
	if !m.connectingAt.IsZero() {
		t.Fatal("chat was treated as a connection event")
	}
	m.observeGameLog([]byte("[Render thread/INFO] [minecraft/ConnectScreen]: Connecting to play.minecraft.net, 25565\n"))
	if m.loggedRemote {
		t.Fatal("pending connection was treated as a game session")
	}
	m.observeGameLog([]byte("[Render thread/INFO]: Loaded 2 advancements\n"))
	if !m.loggedRemote || m.loggedServer != "play.minecraft.net" {
		t.Fatal("successful connection was not reflected in the log fallback")
	}
	m.observeGameLog([]byte("[Render thread/WARN]: Client disconnected with reason: connection closed\n"))
	if m.loggedRemote || m.loggedServer != "" {
		t.Fatal("server activity remained after disconnect")
	}
}
