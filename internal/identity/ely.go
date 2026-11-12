package identity

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sintet/internal/accountfiles"
	"strings"
	"sync"
	"time"
)

const ElyAuthURL = "https://authserver.ely.by"

type ElyProfile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}
type elySession struct {
	AccessToken string       `json:"accessToken"`
	ClientToken string       `json:"clientToken"`
	Selected    ElyProfile   `json:"selectedProfile"`
	Available   []ElyProfile `json:"availableProfiles,omitempty"`
}
type ElyClient struct {
	mu         sync.Mutex
	operation  sync.Mutex
	path, base string
	session    *elySession
	state      State
	HTTP       *http.Client
}
type elyFailure struct {
	status  int
	message string
}

func (e *elyFailure) Error() string { return e.message }
func NewEly(path string) *ElyClient {
	return &ElyClient{path: path, base: ElyAuthURL, HTTP: &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *ElyClient) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	s.Provider = "ely"
	s.SessionSaved = c.session != nil
	if s.Account != nil {
		a := *s.Account
		s.Account = &a
	}
	return s
}
func (c *ElyClient) HasSession() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.session != nil }
func elyAccount(profile ElyProfile) (Account, error) {
	id := strings.ReplaceAll(profile.ID, "-", "")
	if !regexp.MustCompile(`^[a-fA-F0-9]{32}$`).MatchString(id) {
		return Account{}, errors.New("Ely.by вернул неверный UUID")
	}
	id = strings.ToLower(id)
	a := Account{ID: id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:], Username: profile.Name}
	if !validAccount(a) {
		return Account{}, errors.New("Ely.by вернул неверный профиль")
	}
	return a, nil
}
func (c *ElyClient) request(ctx context.Context, endpoint string, body any, out any) error {
	data, e := json.Marshal(body)
	if e != nil {
		return e
	}
	r, e := http.NewRequestWithContext(ctx, "POST", c.base+endpoint, bytes.NewReader(data))
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	res, e := c.HTTP.Do(r)
	if e != nil {
		return errors.New("не удалось подключиться к Ely.by")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 65537))
	if e != nil || len(raw) > 65536 {
		return errors.New("неверный ответ Ely.by")
	}
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		var problem struct {
			Message string `json:"errorMessage"`
		}
		_ = json.Unmarshal(raw, &problem)
		message := "Ely.by временно недоступен"
		if strings.Contains(problem.Message, "two factor") {
			message = "ELY_TWO_FACTOR_REQUIRED"
		} else if res.StatusCode == 401 || res.StatusCode == 403 {
			message = "неверные данные входа или сессия Ely.by истекла"
		} else if res.StatusCode == 429 {
			message = "слишком много попыток; попробуйте позже"
		}
		return &elyFailure{res.StatusCode, message}
	}
	if out != nil {
		if e = json.Unmarshal(raw, out); e != nil {
			return errors.New("неверный ответ Ely.by")
		}
	}
	return nil
}
func (c *ElyClient) save(session elySession) (Account, error) {
	a, e := elyAccount(session.Selected)
	if e != nil {
		return Account{}, e
	}
	if len(session.AccessToken) < 16 || len(session.AccessToken) > 32768 || len(session.ClientToken) < 16 || len(session.ClientToken) > 256 {
		return Account{}, errors.New("Ely.by не выдал сессию")
	}
	session.Available = nil
	plain, _ := json.Marshal(session)
	sealed, e := protect(plain)
	if e != nil {
		return Account{}, e
	}
	e = accountfiles.Write(c.path, sealed)
	if e != nil {
		return Account{}, errors.New("не удалось безопасно сохранить сессию Ely.by")
	}
	c.mu.Lock()
	c.session = &session
	c.state.Account = &a
	c.state.Error = ""
	c.mu.Unlock()
	return a, nil
}
func (c *ElyClient) Login(login, password, totp string) error {
	c.operation.Lock()
	defer c.operation.Unlock()
	if c.HasSession() {
		return errors.New("сначала выйдите из текущего аккаунта")
	}
	login = strings.TrimSpace(login)
	if login == "" || len(login) > 320 || password == "" || len(password) > 1024 {
		return errors.New("введите почту или ник и пароль Ely.by")
	}
	if totp != "" {
		if !regexp.MustCompile(`^[0-9]{6}$`).MatchString(totp) {
			return errors.New("код подтверждения должен содержать 6 цифр")
		}
		password += ":" + totp
	}
	clientToken, e := randomString()
	if e != nil {
		return e
	}
	var session elySession
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	e = c.request(ctx, "/auth/authenticate", map[string]any{"username": login, "password": password, "clientToken": clientToken, "requestUser": true, "agent": map[string]any{"name": "Minecraft", "version": 1}}, &session)
	if e != nil {
		return e
	}
	if session.ClientToken != clientToken {
		return errors.New("неверный идентификатор сессии Ely.by")
	}
	if session.Selected.ID == "" {
		if len(session.Available) != 1 {
			return errors.New("Ely.by не выбрал игровой профиль")
		}
		session.Selected = session.Available[0]
	}
	if _, e = c.save(session); e != nil {
		_ = c.request(ctx, "/auth/invalidate", map[string]string{"accessToken": session.AccessToken, "clientToken": session.ClientToken}, nil)
	}
	return e
}
func (c *ElyClient) Refresh(ctx context.Context) (Account, string, error) {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	saved := c.session
	c.mu.Unlock()
	if saved == nil {
		return Account{}, "", errors.New("войдите через Ely.by")
	}
	var updated elySession
	e := c.request(ctx, "/auth/refresh", map[string]any{"accessToken": saved.AccessToken, "clientToken": saved.ClientToken, "requestUser": true}, &updated)
	if e != nil {
		c.mu.Lock()
		c.state.Error = e.Error()
		c.mu.Unlock()
		return Account{}, "", e
	}
	if updated.ClientToken != saved.ClientToken {
		return Account{}, "", errors.New("неверный ответ Ely.by")
	}
	old, e := elyAccount(saved.Selected)
	if e != nil {
		return Account{}, "", e
	}
	next, e := elyAccount(updated.Selected)
	if e != nil || next.ID != old.ID {
		return Account{}, "", errors.New("профиль Ely.by изменился; войдите снова")
	}
	a, e := c.save(updated)
	if e != nil {
		return Account{}, "", e
	}
	return a, updated.AccessToken, nil
}
func (c *ElyClient) Initialize() { c.mu.Lock(); c.state.Checking = true; c.mu.Unlock(); go c.Restore() }
func (c *ElyClient) LoadSaved() bool {
	sealed, err := os.ReadFile(c.path)
	if err != nil {
		return false
	}
	plain, err := unprotect(sealed)
	if err != nil {
		return false
	}
	var saved elySession
	if json.Unmarshal(plain, &saved) != nil {
		return false
	}
	account, err := elyAccount(saved.Selected)
	if err != nil || saved.AccessToken == "" || saved.ClientToken == "" {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.session = &saved
	c.state.Account = &account
	return true
}

func (c *ElyClient) Restore() {
	defer func() { c.mu.Lock(); c.state.Checking = false; c.mu.Unlock() }()
	if !c.LoadSaved() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Second)
	defer cancel()
	_, _, _ = c.Refresh(ctx)
}
func (c *ElyClient) Logout() error {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	saved := c.session
	c.mu.Unlock()
	if saved != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if e := c.request(ctx, "/auth/invalidate", map[string]string{"accessToken": saved.AccessToken, "clientToken": saved.ClientToken}, nil); e != nil {
			return e
		}
	}
	if e := os.Remove(c.path); e != nil && !os.IsNotExist(e) {
		return fmt.Errorf("не удалось удалить сохранённую сессию")
	}
	c.mu.Lock()
	c.session = nil
	c.state = State{}
	c.mu.Unlock()
	return nil
}

// Forget removes the desktop credential and invalidates this session when reachable.
func (c *ElyClient) Forget() error {
	c.operation.Lock()
	defer c.operation.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	saved := c.session
	c.session = nil
	c.state = State{}
	if saved != nil {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = c.request(ctx, "/auth/invalidate", map[string]string{"accessToken": saved.AccessToken, "clientToken": saved.ClientToken}, nil)
		}()
	}
	return nil
}
