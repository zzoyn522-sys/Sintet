//go:build windows

package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
)

func TestNativeOverlaySocialIPC(t *testing.T) {
	if _, err := os.Stat("native/bin/social-test.exe"); err != nil {
		t.Skip("run native/build.bat test to build the native IPC harness")
	}
	secret := strings.Repeat("b", 64)
	action := make(chan bool, 1)
	var received atomic.Bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+secret {
			http.Error(w, "bad secret", 403)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/badges":
			revision, label := strings.Repeat("a", 64), "Developer"
			if received.Load() {
				revision, label = strings.Repeat("b", 64), "Maintainer"
			}
			result := map[string]any{"revision": revision}
			if r.URL.Query().Get("revision") != revision {
				result["icons"] = map[string]overlayBadgeIcon{"developer": {Label: label, Pixels: strings.Repeat("f", 24*24*8)}}
			}
			json.NewEncoder(w).Encode(result)
		case "/state":
			connection := ""
			provider := "local"
			capacity, turnLimit := 150, int64(1_000_000_000)
			if received.Load() {
				connection, provider = "join-received", "sintet"
				capacity, turnLimit = 8, 5_000_000_000
			}
			members := make([]socialMember, capacity)
			for i := range members {
				members[i] = socialMember{ID: fmt.Sprintf("test-%d", i), Username: fmt.Sprintf("Player%d", i),
					Handle: fmt.Sprintf("player%d", i), Provider: provider, Leader: i == 0}
			}
			peerProvider := "microsoft"
			if received.Load() {
				peerProvider = "ely"
			}
			members[len(members)-1].Provider = peerProvider
			members[len(members)-1].Badges = []string{"developer"}
			json.NewEncoder(w).Encode(map[string]any{"enabled": true, "connection": connection, "lobbyProvider": provider,
				"room": socialRoom{Code: "123456", Capacity: capacity}, "members": members, "turnActive": true, "lobbyConnected": true,
				"lobbyProfile":   socialAccount{ID: "self", Username: "Player0", Provider: provider, Badges: []string{"developer"}},
				"lobbyTurnUsage": turnUsageView{Used: 5000, Limit: turnLimit},
				"networkPeers":   []socialLink{{Name: "Player", Provider: peerProvider, Badges: []string{"developer"}}},
				"friends":        []any{map[string]any{"account": socialAccount{ID: "test-account", Username: "Adolo", Provider: "local", Badges: []string{"developer"}}, "status": "accepted", "presence": map[string]string{"status": "menu"}}}})
		case "/action":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			action <- body["action"] == "join" && body["target"] == "123456"
			received.Store(true)
			json.NewEncoder(w).Encode(map[string]bool{"ok": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	command := exec.Command("native/bin/social-test.exe")
	command.Env = append(os.Environ(), "SINTET_OVERLAY_IPC="+strings.TrimPrefix(server.URL, "http://"), "SINTET_OVERLAY_IPC_SECRET="+secret)
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("%v: %s", err, output)
	}
	select {
	case ok := <-action:
		if !ok {
			t.Fatal("native command corrupted")
		}
	default:
		t.Fatal("native action was not sent")
	}
}
