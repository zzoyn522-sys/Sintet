package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"

	"sintet/internal/sldcfg"
)

var defaultThemePortalMode string

func ThemePortalIsLocal() bool {
	return os.Getenv("SINTET_LOCAL_THEME_SYNC") == "1" || defaultThemePortalMode == "local"
}

func themePortalOrigin() string {
	if ThemePortalIsLocal() {
		return "http://127.0.0.1:3033"
	}
	return "https://sintet.xyz"
}

func (c *Client) themeLibrary(ctx context.Context, accountID, method, id string, data []byte, draft, catalog bool) ([]byte, error) {
	path := ""
	limit := int64(16 << 10)
	switch {
	case catalog:
		path = "?catalog=1"
	case draft:
		path = "/drafts?export=1"
		limit = sldcfg.MaxFile
	case id != "":
		if !themeID.MatchString(id) {
			return nil, errors.New("Оформление не найдено.")
		}
		path = "?id=" + id
		limit = sldcfg.MaxFile
	}
	return c.themeLibraryRequest(ctx, accountID, method, path, data, limit)
}

var themeID = regexp.MustCompile(`^[a-f0-9]{64}$`)

type SavedTheme struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Accent string `json:"accent"`
}

// ThemeLibrary uses the hosted portal, or the explicitly enabled local portal. The credential stays
// in Go and is never sent to a URL supplied by a theme or by the WebView.
func (c *Client) ThemeLibrary(ctx context.Context, accountID, method, id string, data []byte, draft bool) ([]byte, error) {
	return c.themeLibrary(ctx, accountID, method, id, data, draft, false)
}

func (c *Client) UploadThemePackage(ctx context.Context, accountID, id string, data []byte) error {
	body, err := c.ThemeLibrary(ctx, accountID, http.MethodPost, "", data, false)
	if err != nil {
		return err
	}
	var receipt SavedTheme
	if json.Unmarshal(body, &receipt) != nil || receipt.ID != id || receipt.Name == "" {
		return errors.New("Кабинет не подтвердил сохранение оформления.")
	}
	return nil
}

func (c *Client) UploadThemePreviews(ctx context.Context, accountID, id, version string, data []byte) error {
	if !themeID.MatchString(id) {
		return errors.New("Оформление не найдено.")
	}
	body, err := c.themeLibraryRequest(ctx, accountID, http.MethodPost, "/previews?id="+id+"&version="+url.QueryEscape(version), data, 16<<10)
	if err != nil {
		return err
	}
	var receipt struct {
		ID      string
		Screens int
	}
	if json.Unmarshal(body, &receipt) != nil || receipt.ID != id || receipt.Screens != 3 {
		return errors.New("Кабинет не подтвердил сохранение трёх снимков.")
	}
	return nil
}

func (c *Client) themeLibraryRequest(ctx context.Context, accountID, method, path string, data []byte, limit int64) ([]byte, error) {
	c.mu.Lock()
	if c.credential == nil || c.credential.Account.ID != accountID {
		c.mu.Unlock()
		return nil, errors.New("Войди в Sintet ID для синхронизации.")
	}
	token := c.credential.Token
	c.mu.Unlock()
	endpoint := themePortalOrigin() + "/api/account/theme-library" + path
	r, err := http.NewRequestWithContext(ctx, method, endpoint, bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	r.Header.Set("Authorization", "Bearer "+token)
	r.Header.Set("X-Sintet-Account", accountID)
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/octet-stream")
	}
	res, err := c.HTTP.Do(r)
	if err != nil {
		return nil, errors.New("Сервис оформлений Sintet временно недоступен.")
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusCreated {
		var problem struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&problem) == nil && problem.Error != "" {
			return nil, errors.New(problem.Error)
		}
		return nil, errors.New("Не удалось связаться с библиотекой кабинета.")
	}
	if res.ContentLength > limit {
		return nil, errors.New("Файл оформления слишком велик.")
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if int64(len(raw)) > limit {
		return nil, errors.New("Файл оформления слишком велик.")
	}
	return raw, err
}
