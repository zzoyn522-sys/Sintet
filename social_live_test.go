package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"sintet/internal/identity"
)

// Invoked by the portal's live-test script while its two disposable accounts exist.
// No real player's credentials, saved profiles, or game installation are involved.
func TestLiveSocialWorkerToWebRTCTunnel(t *testing.T) {
	var fixtures []struct {
		ID    string `json:"id"`
		Name  string `json:"username"`
		Token string `json:"token"`
	}
	if os.Getenv("SINTET_SOCIAL_FIXTURES") == "" {
		t.Skip("live disposable fixtures not supplied")
	}
	if err := json.Unmarshal([]byte(os.Getenv("SINTET_SOCIAL_FIXTURES")), &fixtures); err != nil || len(fixtures) != 2 {
		t.Fatal("invalid fixtures")
	}
	previousChannel := launcherChannel
	launcherChannel = "test"
	defer func() { launcherChannel = previousChannel }()
	ctx, cancel := context.WithTimeout(context.Background(), 65*time.Second)
	defer cancel()
	managers := make([]*socialManager, 2)
	for i, fixture := range fixtures {
		portal := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/api/launcher/token" {
				http.NotFound(w, r)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"account": identity.Account{ID: fixture.ID, Username: fixture.Name}, "token": fixture.Token, "expires": time.Now().Add(time.Hour)})
		}))
		root := t.TempDir()
		client := identity.New(portal.URL, filepath.Join(root, "fixture.dpapi"))
		address, err := client.Start()
		if err != nil {
			t.Fatal(err)
		}
		authorizeFixture(t, address, strings.Repeat("t", 43))
		portal.Close()
		client.ServiceBase = "https://sintet-identity.zzoyn522.workers.dev"
		app := lobbyFixture(t)
		app.profiles = newProfileRoster(root, portal.URL)
		slot := &profileSlot{profileRecord: profileRecord{Key: "fixture", Provider: "sintet"}, sintet: client}
		app.profiles.slots = []*profileSlot{slot}
		app.profiles.active = "fixture"
		managers[i] = newSocialManager(app, ctx)
		managers[i].identity()
		defer managers[i].disconnect()
	}
	host, guest := managers[0], managers[1]
	target, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer target.Close()
	go func() {
		for {
			c, e := target.Accept()
			if e != nil {
				return
			}
			go func() { defer c.Close(); io.Copy(c, c) }()
		}
	}()
	var created struct {
		Room socialRoom `json:"room"`
	}
	if err = host.post(ctx, map[string]any{"action": "room-create", "version": "1.21.1"}, &created); err != nil {
		t.Fatal(err)
	}
	defer host.post(context.Background(), map[string]any{"action": "room-close"}, nil)
	host.mu.Lock()
	host.state.Room = &created.Room
	host.state.LANPort = target.Addr().(*net.TCPAddr).Port
	host.mu.Unlock()
	go host.pollSocial()
	for attempt := 0; attempt < 2; attempt++ {
		start := time.Now()
		if err = guest.joinRoom(ctx, created.Room.Code, attempt == 0); err != nil {
			t.Fatal(err)
		}
		if attempt == 0 {
			// A restarted leader can still own the room on the server while its
			// in-memory pack is absent. The tunnel itself must remain usable.
			state := guest.snapshot()
			if !host.snapshot().HostNeedsPack || !state.LobbyConnected || state.WorldReady || state.Pack == nil || state.Pack.Phase != "error" {
				t.Fatal("missing host pack was not reported separately from tunnel connectivity")
			}
			if err = host.host(ctx, "1.21.1"); err != nil {
				t.Fatal(err)
			}
			host.mu.Lock()
			host.state.WorldReady = true
			host.state.LANPort = target.Addr().(*net.TCPAddr).Port
			for _, peer := range host.peers {
				if peer != nil {
					peer.SetHostPort(host.state.LANPort)
				}
			}
			host.mu.Unlock()
			waitUntil := time.Now().Add(10 * time.Second)
			for time.Now().Before(waitUntil) {
				state = guest.snapshot()
				if state.WorldReady && state.Pack != nil && state.Pack.Phase == "ready" {
					break
				}
				time.Sleep(100 * time.Millisecond)
			}
			if state.Pack == nil || state.Pack.Phase != "ready" {
				t.Fatal("guest did not recover the pack over the same connection")
			}
		}
		state := guest.snapshot()
		if state.LocalPort == 0 || !state.LobbyConnected || !state.WorldReady {
			t.Fatalf("room did not become connected: %q", state.Status)
		}
		t.Logf("Tunnel %d ready in %s", attempt+1, time.Since(start).Round(time.Millisecond))
		if count := host.snapshot().Connected; count != 1 {
			t.Fatalf("host reports %d peers after join %d", count, attempt)
		}
		conn, err := net.DialTimeout("tcp4", net.JoinHostPort("127.0.0.1", strconv.Itoa(state.LocalPort)), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		conn.SetDeadline(time.Now().Add(10 * time.Second))
		payload := make([]byte, 1<<20)
		rand.Read(payload)
		writeErr := make(chan error, 1)
		go func() { _, err := conn.Write(payload); writeErr <- err }()
		got := make([]byte, len(payload))
		if _, err = io.ReadFull(conn, got); err != nil {
			t.Fatal(err)
		}
		if err = <-writeErr; err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(payload, got) {
			t.Fatal("data corruption across the hosted SDP exchange")
		}
		conn.Close()
		if attempt == 0 {
			guest.disconnect()
		}
	}
	t.Log("PASS: independent hosted identity/signaling -> Pion/STUN -> local Minecraft TCP endpoint, 1 MiB intact before and after reconnect")
}
