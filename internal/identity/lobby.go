package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"sintet/internal/accountfiles"
)

var lobbyChallenge = regexp.MustCompile(`^[a-f0-9]{40}$`)

func lobbyDeviceKey(path string) (string, error) {
	sealed, err := os.ReadFile(path)
	if err == nil {
		plain, err := unprotect(sealed)
		if err != nil || !tokenPattern.Match(plain) {
			return "", errors.New("не удалось прочитать ключ локального профиля для лобби")
		}
		return string(plain), nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}
	key, err := randomString()
	if err != nil {
		return "", err
	}
	sealed, err = protect([]byte(key))
	if err != nil {
		return "", err
	}
	return key, accountfiles.Write(path, sealed)
}

// OpenLobbySession creates a separate client whose credential is scoped to
// lobby routes. The Minecraft token goes directly to its session provider.
func OpenLobbySession(ctx context.Context, base, service, devicePath, provider, name string, microsoft *MicrosoftClient, ely *ElyClient) (*Client, error) {
	ctx, cancel := context.WithTimeout(ctx, 55*time.Second)
	defer cancel()
	client := New(base, "")
	client.ServiceBase = service
	const path = "/api/social/session"
	body := map[string]any{"action": "session", "provider": provider}
	if provider == "local" {
		key, err := lobbyDeviceKey(devicePath)
		if err != nil {
			return nil, err
		}
		body["key"] = key
	} else {
		var profile Account
		var access, join string
		var err error
		var httpClient *http.Client
		switch provider {
		case "microsoft":
			profile, access, _, _, err = microsoft.GameIdentity(ctx)
			join = "https://sessionserver.mojang.com/session/minecraft/join"
			httpClient = microsoft.HTTP
		case "ely":
			profile, access, err = ely.lobbyIdentity()
			join = ely.base + "/session/join"
			httpClient = ely.HTTP
		default:
			return nil, errors.New("неизвестный тип профиля лобби")
		}
		if err != nil {
			return nil, err
		}
		name = profile.Username
		var response struct {
			Challenge string `json:"challenge"`
		}
		if err := client.requestOnce(ctx, http.MethodPost, path, "", map[string]string{
			"action": "challenge", "provider": provider,
		}, &response); err != nil {
			return nil, err
		}
		if !lobbyChallenge.MatchString(response.Challenge) {
			return nil, errors.New("сервис вернул некорректное подтверждение для лобби")
		}
		// Use the existing HTTP implementation with redirect protection, but do
		// not send any provider credential to the Sintet service.
		proofClient := New(join, "")
		proofClient.HTTP = httpClient
		if err := proofClient.requestOnce(ctx, http.MethodPost, "", "", map[string]string{
			"accessToken": access, "selectedProfile": strings.ReplaceAll(profile.ID, "-", ""), "serverId": response.Challenge,
		}, nil); err != nil {
			return nil, errors.New("не удалось подтвердить игровой профиль для P2P. Проверь вход в аккаунт")
		}
		body["challenge"] = response.Challenge
	}
	body["username"] = name
	var saved credential
	if err := client.requestOnce(ctx, http.MethodPost, path, "", body, &saved); err != nil {
		return nil, err
	}
	if !validAccount(saved.Account) || !strings.HasPrefix(saved.Token, "lobby_") ||
		!tokenPattern.MatchString(strings.TrimPrefix(saved.Token, "lobby_")) || !saved.Expires.After(time.Now()) {
		return nil, errors.New("неверная сессия лобби")
	}
	client.credential = &saved
	return client, nil
}

func (c *ElyClient) lobbyIdentity() (Account, string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.session == nil {
		return Account{}, "", errors.New("войдите через Ely.by")
	}
	account, err := elyAccount(c.session.Selected)
	return account, c.session.AccessToken, err
}

func (c *Client) LobbySessionExpires() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential == nil {
		return time.Time{}
	}
	return c.credential.Expires
}

func (c *Client) SetLobbyAvatar(address string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential != nil && strings.HasPrefix(c.credential.Token, "lobby_") {
		c.credential.Account.AvatarURL = address
	}
}

func (c *Client) SetLobbyHandle(handle string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential != nil && strings.HasPrefix(c.credential.Token, "lobby_") {
		c.credential.Account.Handle = handle
	}
}

func (c *Client) UploadLobbyAvatar(ctx context.Context, content []byte) (string, error) {
	c.mu.Lock()
	var token string
	if c.credential != nil {
		token = c.credential.Token
	}
	c.mu.Unlock()
	if !strings.HasPrefix(token, "lobby_") {
		return "", errors.New("выберите профиль Microsoft или Ely.by")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, c.base+"/api/launcher/lobby-avatar", bytes.NewReader(content))
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/octet-stream")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return "", errors.New("модерация аватарок временно недоступна")
	}
	defer response.Body.Close()
	var result struct {
		Avatar struct {
			URL string `json:"url"`
		} `json:"avatar"`
		Error string `json:"error"`
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&result); err != nil {
		return "", errors.New("модерация аватарок временно недоступна")
	}
	if response.StatusCode != http.StatusOK || result.Avatar.URL == "" {
		if result.Error != "" {
			return "", errors.New(result.Error)
		}
		return "", errors.New("не удалось обновить аватарку")
	}
	return result.Avatar.URL, nil
}
