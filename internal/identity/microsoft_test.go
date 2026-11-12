package identity

import (
	"context"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

type microsoftMock func(*http.Request) (*http.Response, error)

func (fn microsoftMock) RoundTrip(r *http.Request) (*http.Response, error) { return fn(r) }

func TestMicrosoftExchangeAndRestore(t *testing.T) {
	const appID = "f82dcf80-955c-4fa0-a51d-b72b7043ce34"
	path := filepath.Join(t.TempDir(), "account.dpapi")
	c := NewMicrosoft(path, appID)
	licensed := true
	c.HTTP.Transport = microsoftMock(func(r *http.Request) (*http.Response, error) {
		var body string
		switch r.URL.Host + r.URL.Path {
		case "user.auth.xboxlive.com/user/authenticate":
			body = `{"Token":"xbox"}`
		case "xsts.auth.xboxlive.com/xsts/authorize":
			body = `{"Token":"xsts","DisplayClaims":{"xui":[{"uhs":"user","xid":"123"}]}}`
		case "api.minecraftservices.com/authentication/login_with_xbox":
			raw, err := io.ReadAll(r.Body)
			if err != nil || !strings.Contains(string(raw), "XBL3.0 x=user;xsts") {
				t.Fatalf("invalid Xbox proof")
			}
			body = `{"access_token":"minecraft-token","expires_in":86400}`
		case "api.minecraftservices.com/entitlements/license":
			if r.Header.Get("Authorization") != "Bearer minecraft-token" || !uuidPattern.MatchString(r.URL.Query().Get("requestId")) {
				t.Fatal("invalid license request")
			}
			body = `{"items":[]}`
			if licensed {
				body = `{"items":[{"name":"game_minecraft"}]}`
			}
		case "api.minecraftservices.com/minecraft/profile":
			if !licensed || r.Header.Get("Authorization") != "Bearer minecraft-token" {
				t.Fatal("unexpected profile request")
			}
			body = `{"id":"0199a45bca2742ad80dbd86b6bb6c5cc","name":"SintetPlayer"}`
		default:
			t.Fatalf("unexpected request to %s", r.URL)
		}
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(body))}, nil
	})

	tokens := microsoftTokens{Access: "ms-token", Refresh: "ms-refresh"}
	verified, err := c.exchange(context.Background(), appID, tokens)
	if err != nil {
		t.Fatal(err)
	}
	if err = c.save(verified); err != nil {
		t.Fatal(err)
	}
	restored := NewMicrosoft(path, "")
	if !restored.LoadSaved() {
		t.Fatal("failed to restore encrypted Microsoft session")
	}
	account, token, clientID, xuid, err := restored.GameIdentity(context.Background())
	if err != nil || account.Username != "SintetPlayer" || account.ID != "0199a45b-ca27-42ad-80db-d86b6bb6c5cc" ||
		token != "minecraft-token" || clientID != appID || xuid != "123" {
		t.Fatal("restored incorrect Minecraft identity")
	}
	licensed = false
	if _, err := c.exchange(context.Background(), appID, tokens); err == nil {
		t.Fatal("exchange succeeded without a Java Edition entitlement")
	}
	if err := restored.Forget(); err != nil || NewMicrosoft(path, "").LoadSaved() {
		t.Fatal("Microsoft session remained after account removal")
	}
}
