package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	p2p "sintet/internal/social"
	"syscall"
	"testing"
	"time"
)

func TestColdLobbyCommandWaitsForMenuAndCurrentTunnel(t *testing.T) {
	const port = 31234
	m := &socialManager{epoch: 4, state: socialState{LocalPort: port}}
	id := m.setGameCommandLocked(port, true, time.Minute)
	if _, delivered := m.pollGameCommand(false); delivered != 0 {
		t.Fatal("cold connection was sent before the main menu")
	}
	if gotID, delivered := m.pollGameCommand(true); gotID != id || delivered != port {
		t.Fatalf("ready game received command %d on port %d", gotID, delivered)
	}
	m.state.LocalPort++
	if _, delivered := m.pollGameCommand(true); delivered != 0 {
		t.Fatal("command escaped its original tunnel")
	}
	m.state.LocalPort = port
	m.epoch++
	if _, delivered := m.pollGameCommand(true); delivered != 0 {
		t.Fatal("command escaped its original lobby")
	}
	m.setGameCommandLocked(port, false, time.Minute)
	if _, delivered := m.pollGameCommand(false); delivered != port {
		t.Fatal("manual overlay join became restricted to the title screen")
	}
}

func TestClosedGuestCannotEraseNextConnection(t *testing.T) {
	m := &socialManager{epoch: 2, guest: "current", peers: map[string]*p2p.Peer{"current": nil}, state: socialState{LocalPort: 54321, Ping: 30, Status: "Подключено", HostName: "Host"}}
	m.finishGuest(1, "previous", nil)
	m.errorForEpoch(1, context.Canceled)
	if m.state.LocalPort != 54321 || m.state.Status != "Подключено" || m.state.Error != "" {
		t.Fatal("old room overwrote a new connection")
	}
	m.finishGuest(2, "current", nil)
	if m.state.LocalPort != 0 || m.state.Ping != 0 || m.state.HostName != "" || m.guest != "" || len(m.peers) != 0 {
		t.Fatal("current connection was not cleaned up")
	}
}

func TestGuestRecoveryRetainsConsentAndOnlyRunsOnceAfterDrop(t *testing.T) {
	m := &socialManager{epoch: 2, guest: "peer", account: "guest", guestPort: 54321, packConsent: "pack-digest",
		peers: map[string]*p2p.Peer{"peer": nil},
		state: socialState{AutoConnect: true, socialRemote: socialRemote{ResumeLobby: true, Room: &socialRoom{Host: "host", Code: "123456"}}}}
	recovery, reconnect := m.finishGuest(2, "peer", nil)
	if !reconnect || recovery.code != "123456" || recovery.digest != "pack-digest" || !recovery.autoConnect || m.guestPort != 0 {
		t.Fatalf("failed to retain lobby intent after a drop: %+v", recovery)
	}
	if _, reconnect := m.finishGuest(2, "peer", nil); reconnect {
		t.Fatal("failed connection immediately began another recovery")
	}
	m.disconnect()
	if m.packConsent != "" || m.state.AutoConnect {
		t.Fatal("intent carried into another lobby")
	}
}

func TestSocialIPCRejectsBrowserAndWrongSecrets(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	m := newSocialManager(&App{}, ctx)
	if err := m.startIPC(); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		auth, origin string
		status       int
	}{
		{"", "", 403}, {"Bearer wrong", "", 403}, {"Bearer " + m.secret, "https://untrusted.example", 403}, {"Bearer " + m.secret, "", 200},
	} {
		r, _ := http.NewRequest("GET", "http://"+m.address+"/state", nil)
		r.Header.Set("Authorization", test.auth)
		r.Header.Set("Origin", test.origin)
		res, err := http.DefaultClient.Do(r)
		if err != nil {
			t.Fatal(err)
		}
		if res.StatusCode != test.status {
			t.Errorf("got %d want %d", res.StatusCode, test.status)
		}
		if test.status == 200 {
			var value map[string]any
			if err = json.NewDecoder(res.Body).Decode(&value); err != nil {
				t.Fatal(err)
			}
			if _, ok := value["friends"]; !ok {
				t.Fatal("friend state missing from IPC")
			}
			if _, ok := value["enabled"]; !ok {
				t.Fatal("account state missing")
			}
		}
		res.Body.Close()
	}
}
func TestLobbyDiskFullIsNotReportedAsNetworkFailure(t *testing.T) {
	err := fmt.Errorf("не удалось подготовить сборку: %w", &os.PathError{Op: "write", Path: "P2P/Temp/objects/pack.tmp", Err: syscall.ENOSPC})
	if got := playerFacingSocialError(err); got != lobbyDiskFullMessage {
		t.Fatalf("got %q, want %q", got, lobbyDiskFullMessage)
	}
}

func TestLANPortFollowsPublishedWorldLifecycle(t *testing.T) {
	for _, test := range []struct {
		log  string
		port int
	}{
		{"[Server thread/INFO]: Started serving on 54321", 54321},
		{"Local game hosted on port 49200", 49200},
		{"[Render thread/INFO]: [System] [CHAT] Local game hosted on port [49910]", 49910},
		{"Local game hosted on port [59294]\nStopping server", 0},
		{"Started serving on 543210", 0},
		{"Started serving on 54321\nStopping server", 0},
		{"Started serving on 54321\nStopping server\nStarted serving on 33333", 33333},
		{"Started serving on 99999", 0},
	} {
		if port := detectedLANPort(test.log); port != test.port {
			t.Errorf("port=%d want %d for %q", port, test.port, test.log)
		}
	}
}

func TestGameLogObserverSurvivesSplitWritesAndLargeLogs(t *testing.T) {
	m := &socialManager{}
	m.observeGameLog([]byte("Starting integrated minecraft server version 1.21.1\nStarted serving on 54"))
	if !m.observedWorld || m.observedPort != 0 {
		t.Fatal("partial LAN port interpreted as complete")
	}
	m.observeGameLog([]byte("321\n"))
	if m.observedPort != 54321 {
		t.Fatal("split log write lost LAN port")
	}
	for i := 0; i < 1000; i++ {
		m.observeGameLog([]byte("Unrelated game output keeps growing well past the log preview limit\n"))
	}
	if m.observedPort != 54321 {
		t.Fatal("long-running game forgot its LAN port")
	}
	m.observeGameLog([]byte("Local game hosted on port [592"))
	m.observeGameLog([]byte("94]\r\n"))
	if m.observedPort != 59294 {
		t.Fatal("bracketed Forge LAN port was not captured")
	}
	m.observeGameLog([]byte("Stopping server\n"))
	if m.observedPort != 0 || m.observedWorld {
		t.Fatal("closed world still advertised")
	}
	m.resetGameLog()
	if m.logTail != "" {
		t.Fatal("new game kept old log data")
	}
}
