package main

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"sintet/internal/identity"
	"sintet/internal/launcher"
)

type elyFixtureTransport func(*http.Request) (*http.Response, error)

func (f elyFixtureTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestExistingElyAndNewLocalProfilesCoexist(t *testing.T) {
	root := t.TempDir()
	ely := identity.NewEly(filepath.Join(root, "ely.dpapi"))
	ely.HTTP.Transport = elyFixtureTransport(func(r *http.Request) (*http.Response, error) {
		var input map[string]any
		if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
			return nil, err
		}
		payload, _ := json.Marshal(map[string]any{"accessToken": strings.Repeat("e", 80), "clientToken": input["clientToken"], "selectedProfile": map[string]string{"id": "ba815a63417645db8e7012e3801c1207", "name": "ElyUser"}})
		return &http.Response{StatusCode: 200, Body: io.NopCloser(bytes.NewReader(payload)), Header: make(http.Header)}, nil
	})
	if err := ely.Login("test@example.test", "test-password", ""); err != nil {
		t.Fatal(err)
	}
	app := &App{profiles: newProfileRoster(root, "http://127.0.0.1:3033"), service: launcher.New(root)}
	if state := app.IdentityState("Player"); state.Provider != "ely" || state.Account.Username != "ElyUser" {
		t.Fatal("Ely session not restored")
	}
	if err := app.AddLocalAccount("LocalTwo", "Player"); err != nil {
		t.Fatal(err)
	}
	if err := app.SelectAccount("legacy-ely"); err != nil {
		t.Fatal(err)
	}
	state := app.IdentityState("Player")
	if state.Provider != "ely" || len(state.Profiles) != 3 {
		t.Fatal("Ely and local profiles did not coexist", state)
	}
}
