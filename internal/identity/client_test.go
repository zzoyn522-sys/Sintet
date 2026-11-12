package identity

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestBrowserLoginPKCEPersistenceAndRevocation(t *testing.T) {
	var expectedChallenge string
	var exchanges atomic.Int32
	var revoked atomic.Bool
	sessionToken := strings.Repeat("t", 43)
	profile := Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "SintetTest"}
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/launcher/token":
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["code"] != strings.Repeat("c", 43) || challenge(body["code_verifier"]) != expectedChallenge {
				http.Error(w, "bad PKCE", 401)
				return
			}
			exchanges.Add(1)
			json.NewEncoder(w).Encode(credential{Account: profile, Token: sessionToken, Expires: time.Now().Add(time.Hour)})
		case "/api/launcher/session":
			if r.Header.Get("Authorization") != "Bearer "+sessionToken {
				http.Error(w, "missing token", 401)
				return
			}
			if r.Method == "DELETE" {
				revoked.Store(true)
				json.NewEncoder(w).Encode(map[string]bool{"ok": true})
				return
			}
			if revoked.Load() {
				http.Error(w, "revoked", 401)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"account": profile})
		default:
			http.NotFound(w, r)
		}
	}))
	defer site.Close()
	path := filepath.Join(t.TempDir(), "identity.dpapi")
	client := New(site.URL, path)
	address, e := client.Start()
	if e != nil {
		t.Fatal(e)
	}
	defer client.Cancel()
	authorize, _ := url.Parse(address)
	q := authorize.Query()
	expectedChallenge = q.Get("code_challenge")
	if len(expectedChallenge) != 43 || !client.State().Pending {
		t.Fatal("missing PKCE or pending state")
	}
	callback, _ := url.Parse(q.Get("redirect_uri"))
	values := url.Values{"state": {"invalid"}, "code": {strings.Repeat("c", 43)}}
	callback.RawQuery = values.Encode()
	bad, e := http.Get(callback.String())
	if e != nil {
		t.Fatal(e)
	}
	bad.Body.Close()
	if bad.StatusCode != 400 || exchanges.Load() != 0 || !client.State().Pending {
		t.Fatal("invalid state accepted")
	}
	values.Set("state", q.Get("state"))
	callback.RawQuery = values.Encode()
	response, e := http.Get(callback.String())
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, response.Body)
	response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal(response.StatusCode)
	}
	if client.State().Account == nil || client.State().Pending || exchanges.Load() != 1 {
		t.Fatal("login incomplete", client.State())
	}
	stored, e := os.ReadFile(path)
	if e != nil || strings.Contains(string(stored), sessionToken) || strings.Contains(string(stored), profile.Username) {
		t.Fatal("plaintext credential persisted", e)
	}
	// A replay cannot issue another token, even during listener shutdown.
	replay, e := http.Get(callback.String())
	if e == nil {
		replay.Body.Close()
		if replay.StatusCode != 409 {
			t.Fatal("replay accepted")
		}
	}
	if exchanges.Load() != 1 {
		t.Fatal("duplicate exchange")
	}
	restored := New(site.URL, path)
	restored.Restore()
	if restored.State().Account == nil || restored.State().Account.ID != profile.ID {
		t.Fatal("session not restored")
	}
	if e = restored.Logout(); e != nil {
		t.Fatal(e)
	}
	if !revoked.Load() {
		t.Fatal("server session not revoked")
	}
	if _, e = os.Stat(path); !os.IsNotExist(e) {
		t.Fatal("credential file retained")
	}
	if _, e = client.Verify(context.Background()); e == nil || client.State().Account != nil {
		t.Fatal("revoked identity accepted")
	}
}
func TestLoginCancellationAndUnsafeOrigin(t *testing.T) {
	client := New("http://evil.example", filepath.Join(t.TempDir(), "secret"))
	if _, e := client.Start(); e == nil {
		t.Fatal("insecure remote origin accepted")
	}
	client = New("http://127.0.0.1:3033", filepath.Join(t.TempDir(), "secret"))
	address, e := client.Start()
	if e != nil {
		t.Fatal(e)
	}
	u, _ := url.Parse(address)
	callback, _ := url.Parse(u.Query().Get("redirect_uri"))
	callback.RawQuery = url.Values{"state": {u.Query().Get("state")}, "error": {"access_denied"}}.Encode()
	r, e := http.Get(callback.String())
	if e != nil {
		t.Fatal(e)
	}
	r.Body.Close()
	if client.State().Pending || client.State().Account != nil || client.State().Error == "" {
		t.Fatal("denial did not clear waiting state")
	}
	client.Cancel()
}

func TestSessionSurvivesWebsiteShutdown(t *testing.T) {
	profile := Account{ID: "ba815a63-4176-45db-8e70-12e3801c1207", Username: "SintetTest", AvatarURL: "https://avatars.example/avatar.png"}
	token := strings.Repeat("t", 43)
	service := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/launcher/session" || r.Header.Get("Authorization") != "Bearer "+token {
			http.Error(w, "invalid", 401)
			return
		}
		returned := profile
		returned.AvatarURL = ""
		returned.Skin = SkinMetadata{URL: "https://identity.example/skin.png", Model: "slim"}
		json.NewEncoder(w).Encode(map[string]any{"account": returned})
	}))
	defer service.Close()
	site := httptest.NewServer(http.NotFoundHandler())
	siteURL := site.URL
	site.Close()
	file := filepath.Join(t.TempDir(), "session")
	client := New(siteURL, file)
	if err := client.storeCredential(credential{Account: profile, Token: token, Expires: time.Now().Add(time.Hour), Base: siteURL}); err != nil {
		t.Fatal(err)
	}
	restored := New(siteURL, file)
	restored.ServiceBase = service.URL
	if !restored.LoadSaved() {
		t.Fatal("saved account lost during backend migration")
	}
	actual, err := restored.Verify(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if actual.AvatarURL != profile.AvatarURL || actual.Skin.Model != "slim" {
		t.Fatal("profile not synchronized", actual)
	}
	if restored.ServerURL() != service.URL+"/api/minecraft" {
		t.Fatal("game still depends on website")
	}
}

func TestSocialRequestReadsAuthenticatedMusicCover(t *testing.T) {
	const route = "/api/social/music-cover?account=fixture&digest=sample"
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.RequestURI() != route || r.Header.Get("Authorization") != "Bearer fixture-token" {
			http.Error(w, "unexpected request", http.StatusBadRequest)
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"cover": strings.Repeat("a", 30*1024)})
	}))
	defer server.Close()
	client := New(server.URL, "")
	client.credential = &credential{Token: "fixture-token"}
	var result struct{ Cover string }
	if err := client.SocialRequest(context.Background(), http.MethodGet, route, nil, &result); err != nil || len(result.Cover) != 30*1024 {
		t.Fatalf("cover request failed: %v, size %d", err, len(result.Cover))
	}
	if err := client.SocialRequest(context.Background(), http.MethodPost, route, nil, nil); err == nil || requests.Load() != 1 {
		t.Fatal("music cover route must accept reads only")
	}
}
