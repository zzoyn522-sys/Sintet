package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLocalLobbyEnrollmentReusesProtectedDeviceKey(t *testing.T) {
	id := "11111111-1111-4111-8111-111111111111"
	token := "lobby_" + strings.Repeat("t", 43)
	keys := make(chan string, 2)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/social/session":
			var body map[string]string
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body["provider"] != "local" || body["action"] != "session" {
				t.Error("incorrect local session request")
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			keys <- body["key"]
			_ = json.NewEncoder(w).Encode(credential{
				Account: Account{ID: id, Username: body["username"]}, Token: token, Expires: time.Now().Add(time.Hour),
			})
		case "/api/social":
			if r.Header.Get("Authorization") != "Bearer "+token {
				t.Error("signaling must use its own lobby-only session")
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			w.WriteHeader(http.StatusNoContent)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "lobby-key.dpapi")
	for _, name := range []string{"PlayerOne", "PlayerTwo"} {
		client, err := OpenLobbySession(context.Background(), server.URL, server.URL, path, "local", name, nil, nil)
		if err != nil {
			t.Fatal(err)
		}
		if account := client.SavedAccount(); account == nil || account.ID != id || account.Username != name {
			t.Fatal("enrollment did not preserve selected local name")
		}
		if err := client.SocialRequest(context.Background(), http.MethodGet, "/api/social", nil, nil); err != nil {
			t.Fatal(err)
		}
	}
	first, second := <-keys, <-keys
	if !tokenPattern.MatchString(first) || first != second {
		t.Fatal("switching the local nickname must not reset TURN accounting")
	}
}
