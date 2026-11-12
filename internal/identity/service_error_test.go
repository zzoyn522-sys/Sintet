package identity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestServiceRetriesReadsAndPresenceButNotInvitations(t *testing.T) {
	for _, action := range []string{"read", "presence", "invite"} {
		t.Run(action, func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if calls.Add(1) == 1 {
					w.WriteHeader(503)
					return
				}
				w.Write([]byte(`{"ok":true}`))
			}))
			defer server.Close()
			client := New(server.URL, filepath.Join(t.TempDir(), "credential"))
			method := http.MethodPost
			var body any = map[string]any{"action": action}
			if action == "read" {
				method, body = http.MethodGet, nil
			}
			var out map[string]bool
			err := client.request(context.Background(), method, "/api/social", "private-token", body, &out)
			if action == "invite" {
				if !TemporaryServiceError(err) || calls.Load() != 1 {
					t.Fatal("non-idempotent action retried", calls.Load(), err)
				}
			} else if err != nil || calls.Load() != 2 || !out["ok"] {
				t.Fatal("safe retry failed", calls.Load(), out, err)
			}
		})
	}
}

func TestServiceDoesNotRetryExpiredSession(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); w.WriteHeader(401) }))
	defer server.Close()
	err := New(server.URL, "").request(context.Background(), "GET", "/api/social", "private-token", nil, nil)
	if err == nil || TemporaryServiceError(err) || calls.Load() != 1 {
		t.Fatal("revoked session treated as a network interruption", err, calls.Load())
	}
}

func TestServiceDiagnosticRemovesURLQueryAndToken(t *testing.T) {
	err := &serviceError{message: "unavailable", method: "GET", route: "/api/social/search?q=private", cause: &url.Error{Op: "Get", URL: "https://example.test/?token=private-token", Err: os.ErrDeadlineExceeded}, temporary: true}
	text := ServiceDiagnostic(err)
	if strings.Contains(text, "private") || !strings.Contains(text, "/api/social/search") || !strings.Contains(text, "timeout") {
		t.Fatal("unsafe or missing diagnostic detail", text)
	}
}

func TestRefreshProfileClearsRecoveredErrorWhenAccountUnchanged(t *testing.T) {
	account := Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "Fixture"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{"account": account})
	}))
	defer server.Close()
	client := New(server.URL, "")
	client.credential = &credential{Account: account, Token: "private-token"}
	client.state.Error = "previous network failure"
	client.RefreshProfile(context.Background())
	if state := client.State(); state.Error != "" || state.Account == nil || *state.Account != account {
		t.Fatal("recovered profile still reports the previous error", state)
	}
}
