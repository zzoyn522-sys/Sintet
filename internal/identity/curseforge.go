package identity

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// CurseForgeRequest sends only the catalog path to the Sintet ID service.
// The upstream key stays in the service and is never returned to the client.
func (c *Client) CurseForgeRequest(original *http.Request) (*http.Response, error) {
	c.mu.Lock()
	if c.credential == nil {
		c.mu.Unlock()
		return nil, errors.New("войдите через Sintet ID для доступа к CurseForge")
	}
	token := c.credential.Token
	base := strings.TrimRight(c.ServiceBase, "/")
	c.mu.Unlock()
	path := original.URL.EscapedPath()
	if original.URL.Host == "api.curseforge.com" {
		path = strings.TrimPrefix(path, "/v1")
	}
	query := url.Values{"path": {path}, "query": {original.URL.RawQuery}}
	if original.URL.Host != "api.curseforge.com" {
		query.Set("cdn", original.URL.Host)
	}
	request, err := http.NewRequestWithContext(original.Context(), http.MethodGet, base+"/api/curseforge?"+query.Encode(), nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	client := *c.HTTP
	// The originating catalog/download request sets its own deadline.
	client.Timeout = 0
	return client.Do(request)
}
