//go:build windows

package main

import (
	"encoding/json"
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
		case "/state":
			connection := ""
			if received.Load() {
				connection = "invite-received"
			}
			json.NewEncoder(w).Encode(map[string]any{"enabled": true, "connection": connection, "friends": []any{map[string]any{"account": socialAccount{ID: "test-account", Username: "Adolo"}, "status": "accepted", "presence": map[string]string{"status": "menu"}}}})
		case "/action":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			action <- body["action"] == "invite" && body["target"] == "test-account"
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
