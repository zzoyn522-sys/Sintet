package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// Opt-in: talks to the local Next.js server and the configured real database.
// Creates a QA account, revokes its test sessions, never prints credentials.
func TestLocalWebsiteHandoff(t *testing.T) {
	if os.Getenv("SINTET_AUTH_INTEGRATION") != "1" {
		t.Skip("requires configured local website")
	}
	base := "http://127.0.0.1:3033"
	browser := &http.Client{Timeout: 25 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	call := func(method, path string, body any, cookie *http.Cookie, origin string) (*http.Response, []byte) {
		t.Helper()
		var input io.Reader
		if body != nil {
			data, _ := json.Marshal(body)
			input = bytes.NewReader(data)
		}
		r, _ := http.NewRequest(method, base+path, input)
		r.Header.Set("Content-Type", "application/json")
		if origin != "" {
			r.Header.Set("Origin", origin)
		}
		if cookie != nil {
			r.AddCookie(cookie)
		}
		response, e := browser.Do(r)
		if e != nil {
			t.Fatal("website request failed", e)
		}
		data, _ := io.ReadAll(io.LimitReader(response.Body, 1000000))
		response.Body.Close()
		return response, data
	}
	nick := "QA" + strconv.FormatInt(time.Now().UnixMilli(), 36)
	password, _ := randomString()
	response, data := call("POST", "/api/auth/register", map[string]string{"username": nick, "password": password}, nil, base)
	if response.StatusCode != 200 {
		t.Fatalf("register returned %d (check local server configuration)", response.StatusCode)
	}
	var created struct {
		Account Account `json:"account"`
	}
	if json.Unmarshal(data, &created) != nil || !validAccount(created.Account) {
		t.Fatal("registration did not return valid profile")
	}
	var cookie *http.Cookie
	for _, c := range response.Cookies() {
		if c.Name == "sintet_session" {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("missing browser session cookie")
	}
	defer call("POST", "/api/auth/logout", nil, cookie, base)
	client := New(base, filepath.Join(t.TempDir(), "identity.dpapi"))
	address, e := client.Start()
	if e != nil {
		t.Fatal(e)
	}
	defer client.Cancel()
	authorize, _ := url.Parse(address)
	params := authorize.Query()
	response, _ = call("GET", authorize.RequestURI(), nil, nil, "")
	if response.StatusCode != 307 || !strings.HasPrefix(response.Header.Get("Location"), "/login?next=") {
		t.Fatal("unauthenticated request did not preserve login return")
	}
	response, _ = call("GET", "/login?next="+url.QueryEscape(authorize.RequestURI()), nil, cookie, "")
	if response.StatusCode != 307 || response.Header.Get("Location") != authorize.RequestURI() {
		t.Fatal("existing browser login did not resume consent")
	}
	response, data = call("GET", authorize.RequestURI(), nil, cookie, "")
	if response.StatusCode != 200 || !strings.Contains(string(data), nick) || !strings.Contains(string(data), "Разрешить лаунчеру") {
		t.Fatal("consent does not identify real browser account")
	}
	input := map[string]string{"redirect_uri": params.Get("redirect_uri"), "state": params.Get("state"), "code_challenge": params.Get("code_challenge")}
	response, _ = call("POST", "/api/launcher/authorize", input, cookie, "https://wrong.example")
	if response.StatusCode != 403 {
		t.Fatal("cross-origin consent accepted")
	}
	response, _ = call("POST", "/api/launcher/authorize", input, nil, base)
	if response.StatusCode != 401 {
		t.Fatal("consent without cookie accepted")
	}
	response, data = call("POST", "/api/launcher/authorize", input, cookie, base)
	if response.StatusCode != 200 {
		t.Fatalf("authorize returned %d", response.StatusCode)
	}
	var approval struct {
		Callback string `json:"callback"`
	}
	json.Unmarshal(data, &approval)
	callback, _ := url.Parse(approval.Callback)
	if callback.Host == "" {
		t.Fatal("missing callback")
	}
	r, e := browser.Get(approval.Callback)
	if e != nil {
		t.Fatal(e)
	}
	io.Copy(io.Discard, r.Body)
	r.Body.Close()
	if r.StatusCode != 200 || client.State().Pending || client.State().Account == nil || client.State().Account.ID != created.Account.ID {
		t.Fatal("website-to-Go handoff failed", r.StatusCode)
	}
	restored := New(base, client.path)
	restored.Restore()
	if restored.State().Account == nil || restored.State().Account.ID != created.Account.ID {
		t.Fatal("real session did not restore")
	}
	if e = restored.Logout(); e != nil {
		t.Fatal(e)
	}
	if _, e = client.Verify(context.Background()); e == nil {
		t.Fatal("revoked real session accepted")
	}
	response, _ = call("GET", "/account", nil, cookie, "")
	if response.StatusCode != 200 {
		t.Fatal("launcher logout also ended browser session")
	}
	// Independently exercise wrong-verifier rejection and one-use redemption.
	verifier, _ := randomString()
	input["code_challenge"] = challenge(verifier)
	response, data = call("POST", "/api/launcher/authorize", input, cookie, base)
	if response.StatusCode != 200 {
		t.Fatal("second consent failed")
	}
	json.Unmarshal(data, &approval)
	callback, _ = url.Parse(approval.Callback)
	code := callback.Query().Get("code")
	response, _ = call("POST", "/api/launcher/token", map[string]string{"code": code, "code_verifier": strings.Repeat("x", 43)}, nil, "")
	if response.StatusCode != 401 {
		t.Fatal("wrong verifier accepted")
	}
	response, data = call("POST", "/api/launcher/token", map[string]string{"code": code, "code_verifier": verifier}, nil, "")
	if response.StatusCode != 200 {
		t.Fatal("correct verifier rejected")
	}
	var token credential
	json.Unmarshal(data, &token)
	response, _ = call("POST", "/api/launcher/token", map[string]string{"code": code, "code_verifier": verifier}, nil, "")
	if response.StatusCode != 401 {
		t.Fatal("authorization code replay accepted")
	}
	if e = client.request(context.Background(), "DELETE", "/api/launcher/session", token.Token, nil, nil); e != nil {
		t.Fatal(e)
	}
	t.Log("Real local registration, cookie consent, PKCE handoff, encrypted restore, replay rejection and logout passed. QA account:", nick)
}
