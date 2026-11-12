package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"sintet/internal/identity"
	"sintet/internal/launcher"
)

func authorizeFixture(t *testing.T, address, code string) {
	t.Helper()
	page, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	callback, err := url.Parse(page.Query().Get("redirect_uri"))
	if err != nil {
		t.Fatal(err)
	}
	callback.RawQuery = url.Values{"state": {page.Query().Get("state")}, "code": {code}}.Encode()
	response, err := http.Get(callback.String())
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("callback status %d", response.StatusCode)
	}
}

func TestAccountRosterKeepsIndependentSessionsAndRestoresSelection(t *testing.T) {
	first := identity.Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "FirstPlayer"}
	second := identity.Account{ID: "f5ca677c-926e-429c-9edb-82e9da112032", Username: "SecondPlayer"}
	firstToken, secondToken := strings.Repeat("a", 43), strings.Repeat("b", 43)
	duplicateToken := strings.Repeat("d", 43)
	revoked := make(chan string, 4)
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/launcher/token":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			account, token := first, firstToken
			if body["code"] == secondToken {
				account, token = second, secondToken
			}
			if body["code"] == duplicateToken {
				token = duplicateToken
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"account": account, "token": token, "expires": time.Now().Add(time.Hour)})
		case r.URL.Path == "/api/launcher/session":
			token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
			if r.Method == "DELETE" {
				revoked <- token
				w.WriteHeader(204)
				return
			}
			account := first
			if token == secondToken {
				account = second
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"account": account})
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	root := t.TempDir()
	old := identity.New(site.URL, filepath.Join(root, "identity.dpapi"))
	address, err := old.Start()
	if err != nil {
		t.Fatal(err)
	}
	authorizeFixture(t, address, firstToken)
	app := &App{profiles: newProfileRoster(root, site.URL), service: launcher.New(root)}
	if state := app.IdentityState("MyLocal"); state.Active != "legacy-sintet" || len(state.Profiles) != 2 {
		t.Fatal("legacy profile not restored", state)
	}
	address, err = app.beginSintetLogin()
	if err == nil {
		app.profiles.pending.sintet.ServiceBase = site.URL
	}
	if err != nil {
		t.Fatal(err)
	}
	if state := app.IdentityState("MyLocal"); !state.Pending || state.Account.ID != first.ID {
		t.Fatal("sign-in replaced the active profile before approval")
	}
	authorizeFixture(t, address, secondToken)
	state := app.IdentityState("MyLocal")
	if state.Pending || state.Account.ID != second.ID || len(state.Profiles) != 3 {
		t.Fatal("second account not added", state)
	}
	secondKey := state.Active
	address, err = app.beginSintetLogin()
	if err == nil {
		app.profiles.pending.sintet.ServiceBase = site.URL
	}
	if err != nil {
		t.Fatal(err)
	}
	authorizeURL, err := url.Parse(address)
	if err != nil {
		t.Fatal(err)
	}
	excluded := strings.Split(authorizeURL.Query().Get("excluded"), ",")
	if len(excluded) != 2 || excluded[0] != first.ID || excluded[1] != second.ID {
		t.Fatal("saved account exclusions missing", excluded)
	}
	authorizeFixture(t, address, duplicateToken)
	if got := app.IdentityState("MyLocal"); got.LoginError == "" || got.Active != secondKey || len(got.Profiles) != 3 {
		t.Fatal("duplicate account replaced an existing slot", got)
	}
	select {
	case token := <-revoked:
		if token != duplicateToken {
			t.Fatal("revoked existing account")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("duplicate token not revoked")
	}
	if err = app.SelectAccount("legacy-sintet"); err != nil {
		t.Fatal(err)
	}
	if err = app.AddLocalAccount("AnotherLocal", "MyLocal"); err != nil {
		t.Fatal(err)
	}
	local := app.IdentityState("MyLocal")
	if local.Provider != "local" || local.Account.ID != launcher.OfflineProfileID("AnotherLocal") || len(local.Profiles) != 4 {
		t.Fatal("local profile mismatch", local)
	}
	if err = app.AddLocalAccount("AnotherLocal", "MyLocal"); err != nil {
		t.Fatal(err)
	}
	if len(app.IdentityState("MyLocal").Profiles) != 4 {
		t.Fatal("duplicate local profile")
	}
	if err = app.SelectAccount(secondKey); err != nil {
		t.Fatal(err)
	}
	restored := &App{profiles: newProfileRoster(root, site.URL), service: launcher.New(root)}
	if got := restored.IdentityState("MyLocal"); got.Active != secondKey || len(got.Profiles) != 4 {
		t.Fatal("selection not persisted", got)
	}
	backupPath := filepath.Join(root, "accounts.json.bak")
	stale := []byte(`{"active":"local-default","profiles":[]}`)
	if err := os.WriteFile(backupPath, stale, 0600); err != nil {
		t.Fatal(err)
	}
	newProfileRoster(root, site.URL)
	_, updatedBackup, err := readProfileIndex(backupPath)
	if err != nil || string(updatedBackup) == string(stale) {
		t.Fatal("startup did not refresh stale backup", err)
	}
	indexPath := filepath.Join(root, "accounts.json")
	if err := os.WriteFile(indexPath, make([]byte, 220), 0600); err != nil {
		t.Fatal(err)
	}
	backupRestore := newProfileRoster(root, site.URL).selection("MyLocal")
	if backupRestore.Active != secondKey || len(backupRestore.Profiles) != 4 || backupRestore.Error != "" {
		t.Fatal("backup did not restore selection and local profiles", backupRestore)
	}
	evidence, _ := filepath.Glob(filepath.Join(root, "accounts.corrupt-*.json"))
	if len(evidence) != 1 {
		t.Fatal("damaged index was not preserved")
	}
	recoveryRoot := t.TempDir()
	if err := os.MkdirAll(filepath.Join(recoveryRoot, "accounts"), 0700); err != nil {
		t.Fatal(err)
	}
	for _, relative := range []string{"identity.dpapi", filepath.Join("accounts", secondKey+".dpapi")} {
		raw, err := os.ReadFile(filepath.Join(root, relative))
		if err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(recoveryRoot, relative), raw, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(recoveryRoot, "accounts.json"), make([]byte, 220), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recoveryRoot, "accounts.json.bak"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(recoveryRoot, ".account-interrupted.tmp"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	fromSessions := newProfileRoster(recoveryRoot, site.URL).selection("MyLocal")
	if len(fromSessions.Profiles) != 3 || fromSessions.Provider != "sintet" || fromSessions.Error != "" {
		t.Fatal("encrypted sessions not recovered", fromSessions)
	}
	for key, expected := range map[string]string{"legacy-sintet": firstToken, secondKey: secondToken} {
		slot := restored.profiles.find(key)
		slot.sintet.ServiceBase = site.URL
		_, token, err := slot.sintet.GameIdentity(context.Background())
		if err != nil || token != expected {
			t.Fatal("slot did not use its own session", err)
		}
	}
	if err = restored.LogoutAccount(); err != nil {
		t.Fatal(err)
	}
	select {
	case token := <-revoked:
		if token != secondToken {
			t.Fatal("revoked wrong account")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("session not revoked")
	}
	got := restored.IdentityState("MyLocal")
	if len(got.Profiles) != 3 || restored.profiles.find("legacy-sintet") == nil {
		t.Fatal("logout affected other profiles")
	}
	index, _ := os.ReadFile(filepath.Join(root, "accounts.json"))
	if strings.Contains(string(index), firstToken) || strings.Contains(string(index), secondToken) {
		t.Fatal("plaintext credential in index")
	}
}

func TestFiveProfilesAndRemovingInactiveAccount(t *testing.T) {
	root := t.TempDir()
	app := &App{profiles: newProfileRoster(root, "http://127.0.0.1:3033"), service: launcher.New(root)}
	for _, name := range []string{"Local1", "Local2", "Local3", "Local4"} {
		if err := app.AddLocalAccount(name, "Player"); err != nil {
			t.Fatal(err)
		}
	}
	initial := app.IdentityState("Player")
	if len(initial.Profiles) != 5 || initial.CanAdd {
		t.Fatal("incorrect capacity")
	}
	if err := app.AddLocalAccount("Local5", "Player"); err == nil {
		t.Fatal("accepted a sixth profile")
	}
	if err := app.RemoveAccount(defaultProfile); err != nil {
		t.Fatal(err)
	}
	if state := app.IdentityState("Player"); len(state.Profiles) != 4 || state.Active != initial.Active || !state.CanAdd {
		t.Fatal("removing inactive local profile changed active profile")
	}
	if err := app.AddLocalAccount("Local5", "Player"); err != nil {
		t.Fatal(err)
	}
	restored := newProfileRoster(root, "http://127.0.0.1:3033").selection("Player")
	if len(restored.Profiles) != 5 || restored.CanAdd || restored.Account.Username != "Local5" {
		t.Fatal("capacity or selection changed on restore")
	}
	if err := app.RenameLocalAccount(restored.Active, "Local1", "Player"); err == nil {
		t.Fatal("accepted duplicate nickname")
	}
	if err := app.RenameLocalAccount(restored.Active, "RenamedLocal", "Player"); err != nil {
		t.Fatal(err)
	}
	renamed := newProfileRoster(root, "http://127.0.0.1:3033").selection("Player")
	if renamed.Active != restored.Active || renamed.Account.Username != "RenamedLocal" || renamed.Account.ID != launcher.OfflineProfileID("RenamedLocal") {
		t.Fatal("renamed profile not restored")
	}
	for _, profile := range restored.Profiles {
		if err := app.RemoveAccount(profile.Key); err != nil {
			t.Fatal(err)
		}
	}
	if state := app.IdentityState("Player"); state.Active != defaultProfile || len(state.Profiles) != 1 || !state.CanAdd {
		t.Fatal("default fallback was not restored")
	}
}
