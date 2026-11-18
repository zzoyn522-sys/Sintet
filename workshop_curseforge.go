package main

import (
	"errors"
	"net/http"

	"sintet/internal/identity"
)

type curseForgeTransport struct {
	app  *App
	base http.RoundTripper
	key  string
}

func (t curseForgeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.User != nil || request.Method != http.MethodGet {
		return t.base.RoundTrip(request)
	}
	switch request.URL.Host {
	case "api.curseforge.com", "edge.forgecdn.net", "mediafilez.forgecdn.net", "media.forgecdn.net":
	default:
		return t.base.RoundTrip(request)
	}
	if t.key != "" {
		copy := request.Clone(request.Context())
		copy.Header.Set("x-api-key", t.key)
		return t.base.RoundTrip(copy)
	}
	t.app.authMu.Lock()
	slot := t.app.profiles.find(t.app.profiles.active)
	var client *identity.Client
	if slot != nil && slot.Provider == "sintet" {
		client = slot.sintet
	}
	t.app.authMu.Unlock()
	if client == nil {
		return nil, errors.New("войдите через Sintet ID для доступа к CurseForge")
	}
	return client.CurseForgeRequest(request)
}
