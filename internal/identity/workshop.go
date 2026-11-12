package identity

import (
	"context"
	"errors"
	"net/http"
)

// PublishWorkshop keeps the session token in Go; no credential is exposed to the WebView.
func (c *Client) PublishWorkshop(ctx context.Context, accountID string, snapshot any) error {
	c.mu.Lock()
	if c.credential == nil || c.credential.Account.ID != accountID {
		c.mu.Unlock()
		return errors.New("аккаунт изменился")
	}
	token := c.credential.Token
	c.mu.Unlock()
	return c.request(ctx, http.MethodPut, "/api/account/workshop", token, snapshot, nil)
}
