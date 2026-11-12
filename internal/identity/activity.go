package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
)

type ActivityTotals struct {
	LauncherMS  int64 `json:"launcherMs"`
	MinecraftMS int64 `json:"minecraftMs"`
}

func (c *Client) PublishActivity(ctx context.Context, accountID, databaseURL, publicKey, device string, totals ActivityTotals) (ActivityTotals, error) {
	c.mu.Lock()
	if c.credential == nil || c.credential.Account.ID != accountID {
		c.mu.Unlock()
		return ActivityTotals{}, errors.New("аккаунт изменился")
	}
	token := c.credential.Token
	c.mu.Unlock()

	body, err := json.Marshal(struct {
		Token       string `json:"p_token"`
		Device      string `json:"p_device"`
		LauncherMS  int64  `json:"p_launcher_ms"`
		MinecraftMS int64  `json:"p_minecraft_ms"`
	}{token, device, totals.LauncherMS, totals.MinecraftMS})
	if err != nil {
		return ActivityTotals{}, err
	}
	address := strings.TrimRight(databaseURL, "/") + "/rest/v1/rpc/sintet_sync_activity"
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, address, bytes.NewReader(body))
	if err != nil {
		return ActivityTotals{}, err
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("apikey", publicKey)
	response, err := c.HTTP.Do(request)
	if err != nil {
		return ActivityTotals{}, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return ActivityTotals{}, errors.New("не удалось сохранить статистику Sintet")
	}
	var other ActivityTotals
	err = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&other)
	return other, err
}
