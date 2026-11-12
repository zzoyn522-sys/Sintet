package main

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"sintet/internal/identity"
)

// This configuration contains an intentionally public Supabase key, never a server key.
//
//go:embed avatar-db-public.json
var publicAvatarConfig []byte

var avatarAccountID = regexp.MustCompile(`^[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}$`)
var avatarDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)
var directAvatarPath = regexp.MustCompile(`^/avatars/[a-f0-9]{8}(?:-[a-f0-9]{4}){3}-[a-f0-9]{12}/[a-f0-9]{64}\.png$`)

func (db *avatarDatabase) allowedTexture(address string) bool {
	if db == nil {
		return false
	}
	origin, e := url.Parse(db.ImageOrigin)
	if e != nil {
		return false
	}
	target, e := url.Parse(address)
	if e != nil {
		return false
	}
	return origin.Host != "" && strings.EqualFold(origin.Host, target.Host) && origin.Scheme == target.Scheme &&
		(target.Scheme == "https" || target.Scheme == "http" && target.Hostname() == "127.0.0.1") &&
		target.User == nil && target.RawQuery == "" && target.Fragment == "" && directAvatarPath.MatchString(target.EscapedPath())
}

type avatarDatabase struct {
	URL         string       `json:"url"`
	Key         string       `json:"key"`
	ImageOrigin string       `json:"imageOrigin"`
	HTTP        *http.Client `json:"-"`
}

func newAvatarDatabase() *avatarDatabase {
	var config avatarDatabase
	if json.Unmarshal(publicAvatarConfig, &config) != nil || !strings.HasPrefix(config.Key, "sb_publishable_") {
		return nil
	}
	config.HTTP = &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return &config
}

// avatar reads only the public projection. No login token or website request is involved.
func (db *avatarDatabase) avatar(ctx context.Context, id string) (string, error) {
	if db == nil || db.ImageOrigin == "" {
		return "", errors.New("источник аватарок не настроен")
	}
	id = strings.ToLower(id)
	if !avatarAccountID.MatchString(id) {
		return "", errors.New("неверный ID профиля")
	}
	query := url.Values{"account_id": {"eq." + id}, "select": {"account_id,digest"}, "limit": {"1"}}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(db.URL, "/")+"/rest/v1/sintet_id_avatars?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	request.Header.Set("apikey", db.Key)
	request.Header.Set("Cache-Control", "no-cache")
	response, err := db.HTTP.Do(request)
	if err != nil {
		return "", errors.New("база аватарок временно недоступна")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("не удалось прочитать аватарку из БД")
	}
	var rows []struct {
		ID     string `json:"account_id"`
		Digest string `json:"digest"`
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, 4097))
	if err != nil || len(data) > 4096 || json.Unmarshal(data, &rows) != nil {
		return "", errors.New("неверный ответ базы аватарок")
	}
	if len(rows) == 0 {
		return "", nil
	}
	if len(rows) != 1 || rows[0].ID != id || !avatarDigest.MatchString(rows[0].Digest) {
		return "", errors.New("неверная запись аватарки")
	}
	return strings.TrimRight(db.ImageOrigin, "/") + "/avatars/" + id + "/" + rows[0].Digest + ".png", nil
}

func (a *App) syncAvatar(ctx context.Context, client *identity.Client) {
	account := client.SavedAccount()
	if account == nil {
		return
	}
	address, err := a.avatarDB.avatar(ctx, account.ID)
	if err != nil || address == "" {
		return
	} // Keep the last valid image on network/database failures.
	if _, err = a.ResolveAvatar(address); err != nil {
		return
	}
	_ = client.UpdateAvatar(account.ID, address)
}
