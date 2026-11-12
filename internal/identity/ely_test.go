package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestElyTwoFactorEncryptedSessionRefreshAndLogout(t *testing.T) {
	var clientToken string
	var invalidated atomic.Bool
	var refreshes atomic.Int32
	profile := ElyProfile{ID: "ba815a63417645db8e7012e3801c1207", Name: "ElyPlayer"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if json.NewDecoder(r.Body).Decode(&body) != nil {
			t.Error("invalid request")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/auth/authenticate":
			if body["username"] != "qa@example.test" || body["requestUser"] != true {
				t.Error("invalid authenticate fields")
			}
			if body["password"] == "temporary-password" {
				w.WriteHeader(401)
				io.WriteString(w, `{"errorMessage":"Account protected with two factor auth."}`)
				return
			}
			if body["password"] != "temporary-password:123456" {
				t.Error("missing TOTP")
			}
			clientToken, _ = body["clientToken"].(string)
			json.NewEncoder(w).Encode(elySession{AccessToken: strings.Repeat("a", 80), ClientToken: clientToken, Selected: profile})
		case "/auth/refresh":
			if body["clientToken"] != clientToken || body["requestUser"] != true || body["password"] != nil {
				t.Error("invalid refresh")
			}
			n := refreshes.Add(1)
			p := profile
			p.Name = "RenamedPlayer"
			json.NewEncoder(w).Encode(elySession{AccessToken: strings.Repeat(string(rune('b'+n)), 80), ClientToken: clientToken, Selected: p})
		case "/auth/invalidate":
			invalidated.Store(true)
			w.WriteHeader(204)
		case "/auth/signout":
			t.Error("must not sign out other launchers")
		default:
			t.Error("unexpected endpoint")
		}
	}))
	defer server.Close()
	path := filepath.Join(t.TempDir(), "ely.dpapi")
	client := NewEly(path)
	client.base = server.URL
	if e := client.Login("qa@example.test", "temporary-password", ""); e == nil || e.Error() != "ELY_TWO_FACTOR_REQUIRED" {
		t.Fatal("missing 2FA request")
	}
	if client.HasSession() {
		t.Fatal("2FA failure saved session")
	}
	if e := client.Login("qa@example.test", "temporary-password", "123456"); e != nil {
		t.Fatal(e)
	}
	s := client.State()
	if s.Provider != "ely" || s.Account == nil || s.Account.Username != "ElyPlayer" || !s.SessionSaved {
		t.Fatal("missing Ely identity")
	}
	raw, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(raw), "temporary-password") || strings.Contains(string(raw), strings.Repeat("a", 80)) {
		t.Fatal("plaintext credentials stored")
	}
	plain, e := unprotect(raw)
	if e != nil {
		t.Fatal(e)
	}
	if strings.Contains(string(plain), "password") || strings.Contains(string(plain), "qa@example.test") {
		t.Fatal("password or email persisted")
	}
	account, token, e := client.Refresh(context.Background())
	if e != nil || account.Username != "RenamedPlayer" || account.ID != s.Account.ID || token == "" {
		t.Fatal("profile refresh failed", e)
	}
	restored := NewEly(path)
	restored.base = server.URL
	restored.Restore()
	if restored.State().Account == nil || restored.State().Account.Username != "RenamedPlayer" {
		t.Fatal("Ely session did not restore")
	}
	if e = restored.Logout(); e != nil {
		t.Fatal(e)
	}
	if restored.HasSession() || !invalidated.Load() {
		t.Fatal("logout did not revoke session")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("session retained on disk")
	}
}
func TestElyDoesNotReflectPasswordInErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(401)
		io.WriteString(w, `{"errorMessage":"temporary-password should never be shown"}`)
	}))
	defer server.Close()
	client := NewEly(filepath.Join(t.TempDir(), "ely.dpapi"))
	client.base = server.URL
	e := client.Login("player", "temporary-password", "")
	if e == nil || strings.Contains(e.Error(), "temporary-password") || client.State().Account != nil {
		t.Fatal("unsafe error or invalid login")
	}
}
