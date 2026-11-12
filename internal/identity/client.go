package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sintet/internal/accountfiles"
	"strings"
	"sync"
	"time"
)

type SkinMetadata struct {
	URL    string `json:"url"`
	Model  string `json:"model"`
	Digest string `json:"digest"`
}
type Account struct {
	Handle    string       `json:"handle,omitempty"`
	AvatarURL string       `json:"avatarURL,omitempty"`
	Skin      SkinMetadata `json:"skin"`
	ID        string       `json:"id"`
	Username  string       `json:"username"`
	Email     string       `json:"email,omitempty"`
	Created   string       `json:"created_at"`
}
type State struct {
	Provider     string   `json:"provider"`
	SessionSaved bool     `json:"sessionSaved"`
	Pending      bool     `json:"pending"`
	Checking     bool     `json:"checking"`
	Account      *Account `json:"account"`
	Error        string   `json:"error"`
}
type credential struct {
	Account Account   `json:"account"`
	Token   string    `json:"token"`
	Expires time.Time `json:"expires"`
	Base    string    `json:"base"`
}
type Client struct {
	mu            sync.Mutex
	verifyMu      sync.Mutex
	base, path    string
	state         State
	credential    *credential
	pendingCancel context.CancelFunc
	generation    uint64
	HTTP          *http.Client
	ServiceBase   string
	OnSuccess     func()
}

var tokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)
var uuidPattern = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func New(base, path string) *Client {
	return &Client{base: strings.TrimRight(base, "/"), path: path, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}}
}
func randomString() (string, error) {
	b := make([]byte, 32)
	if _, e := rand.Read(b); e != nil {
		return "", e
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}
func challenge(verifier string) string {
	hash := sha256.Sum256([]byte(verifier))
	return base64.RawURLEncoding.EncodeToString(hash[:])
}
func validAccount(a Account) bool {
	return uuidPattern.MatchString(a.ID) && regexp.MustCompile(`^[A-Za-z0-9_]{3,16}$`).MatchString(a.Username)
}
func (c *Client) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	s := c.state
	s.Provider = "sintet"
	s.SessionSaved = c.credential != nil
	if s.Account != nil {
		copy := *s.Account
		s.Account = &copy
	}
	return s
}
func (c *Client) SavedAccount() *Account {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential == nil {
		return nil
	}
	account := c.credential.Account
	return &account
}
func (c *Client) HasSession() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.credential != nil }

// UpdateAvatar updates only public image metadata, without re-authenticating or changing the session.
func (c *Client) UpdateAvatar(id, address string) error {
	c.verifyMu.Lock()
	defer c.verifyMu.Unlock()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential == nil || c.credential.Account.ID != id {
		return errors.New("профиль уже удалён")
	}
	if c.credential.Account.AvatarURL == address {
		return nil
	}
	updated := *c.credential
	updated.Account.AvatarURL = address
	err := c.storeCredential(updated)
	c.credential = &updated
	if c.state.Account != nil {
		account := *c.state.Account
		account.AvatarURL = address
		c.state.Account = &account
	}
	return err
}
func (c *Client) requestOnce(ctx context.Context, method, path, token string, body any, out any) error {
	var data io.Reader
	if body != nil {
		b, e := json.Marshal(body)
		if e != nil {
			return e
		}
		data = bytes.NewReader(b)
	}
	base := c.base
	useSessionService := path == "/api/launcher/session" && c.base != "http://127.0.0.1:3033"
	if (useSessionService || path == "/api/launcher/update" || strings.HasPrefix(path, "/api/social")) && c.ServiceBase != "" {
		base = strings.TrimRight(c.ServiceBase, "/")
	}
	r, e := http.NewRequestWithContext(ctx, method, base+path, data)
	if e != nil {
		return e
	}
	r.Header.Set("Content-Type", "application/json")
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := c.HTTP.Do(r)
	if e != nil {
		return &serviceError{message: "сервис Sintet ID временно недоступен", method: method, route: path, cause: e, temporary: true}
	}
	defer response.Body.Close()
	if response.StatusCode == 401 {
		return errors.New("сессия завершена; войдите снова")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		if response.StatusCode == 408 || response.StatusCode == 429 || response.StatusCode >= 500 {
			return &serviceError{message: "сервис Sintet ID временно недоступен", method: method, route: path, cause: fmt.Errorf("HTTP %d", response.StatusCode), temporary: true}
		}
		if strings.HasPrefix(path, "/api/social") {
			var problem struct {
				Error string `json:"error"`
			}
			if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&problem) == nil && problem.Error != "" {
				return errors.New(problem.Error)
			}
		}
		return fmt.Errorf("сервер входа недоступен (%d)", response.StatusCode)
	}
	if out != nil {
		limit := int64(8192)
		if path == "/api/launcher/update" {
			limit = 64 << 10
		}
		if strings.HasPrefix(path, "/api/social") {
			limit = 256 << 10
		}
		var payload json.RawMessage
		err := json.NewDecoder(io.LimitReader(response.Body, limit)).Decode(&payload)
		if err != nil {
			return &serviceError{message: "не удалось получить ответ Sintet ID", method: method, route: path, cause: err, temporary: true}
		}
		if err := json.Unmarshal(payload, out); err != nil {
			return errors.New("неверный ответ сервера Sintet ID")
		}
	}
	return nil
}

// UpdateOffer authenticates account-scoped releases without exposing credentials.
func (c *Client) UpdateOffer(ctx context.Context) (json.RawMessage, error) {
	c.mu.Lock()
	if c.credential == nil {
		c.mu.Unlock()
		return nil, errors.New("войдите через Sintet ID")
	}
	token := c.credential.Token
	c.mu.Unlock()
	var result struct {
		Release json.RawMessage `json:"release"`
	}
	err := c.request(ctx, http.MethodGet, "/api/launcher/update", token, nil, &result)
	return result.Release, err
}

// SocialRequest uses the encrypted desktop credential without exposing it to UI or DLL.
func (c *Client) SocialRequest(ctx context.Context, method, path string, body, out any) error {
	coverRequest := method == http.MethodGet && strings.HasPrefix(path, "/api/social/music-cover?")
	traceRequest := method == http.MethodPost && path == "/api/social/ice-trace"
	if path != "/api/social" && !strings.HasPrefix(path, "/api/social/search?") && !coverRequest && !traceRequest {
		return errors.New("invalid social route")
	}
	c.mu.Lock()
	if c.credential == nil {
		c.mu.Unlock()
		return errors.New("войдите через Sintet ID")
	}
	token := c.credential.Token
	c.mu.Unlock()
	return c.request(ctx, method, path, token, body, out)
}
func (c *Client) Verify(ctx context.Context) (*Account, error) {
	c.verifyMu.Lock()
	defer c.verifyMu.Unlock()
	c.mu.Lock()
	saved := c.credential
	generation := c.generation
	c.mu.Unlock()
	if saved == nil {
		return nil, nil
	}
	if time.Now().After(saved.Expires) {
		err := errors.New("сессия истекла; войдите снова")
		c.mu.Lock()
		if generation == c.generation {
			c.state.Error = err.Error()
		}
		c.mu.Unlock()
		return nil, err
	}
	var result struct {
		Account Account `json:"account"`
	}
	if e := c.request(ctx, "GET", "/api/launcher/session", saved.Token, nil, &result); e != nil {
		c.mu.Lock()
		if generation == c.generation {
			c.state.Account = nil
			c.state.Error = e.Error()
		}
		c.mu.Unlock()
		return nil, e
	}
	if !validAccount(result.Account) || result.Account.ID != saved.Account.ID {
		return nil, errors.New("неверный ответ сервера")
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.generation != generation {
		return nil, errors.New("вход отменён")
	}
	if c.ServiceBase != "" {
		result.Account.AvatarURL = saved.Account.AvatarURL
	}
	if result.Account.Email == "" {
		result.Account.Email = saved.Account.Email
	}
	if saved.Account != result.Account {
		updated := *saved
		updated.Account = result.Account
		// A cache write must not prevent the current profile from being displayed.
		if c.storeCredential(updated) == nil {
			c.credential = &updated
		}
	}
	c.state.Account = &result.Account
	c.state.Error = ""
	return &result.Account, nil
}

// RefreshProfile updates metadata in the background while retaining the cached
// profile when the network is temporarily unavailable. Launch still uses Verify.
func (c *Client) RefreshProfile(ctx context.Context) {
	c.verifyMu.Lock()
	defer c.verifyMu.Unlock()
	c.mu.Lock()
	saved, generation := c.credential, c.generation
	c.mu.Unlock()
	if saved == nil {
		return
	}
	var result struct {
		Account Account `json:"account"`
	}
	if c.request(ctx, http.MethodGet, "/api/launcher/session", saved.Token, nil, &result) != nil ||
		!validAccount(result.Account) || result.Account.ID != saved.Account.ID {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if generation != c.generation {
		return
	}
	if c.ServiceBase != "" {
		result.Account.AvatarURL = saved.Account.AvatarURL
	}
	c.state.Account = &result.Account
	c.state.Error = ""
	if result.Account == saved.Account {
		return
	}
	updated := *saved
	updated.Account = result.Account
	if c.storeCredential(updated) == nil {
		c.credential = &updated
	}
	c.state.Account = &result.Account
	c.state.Error = ""
}

func (c *Client) storeCredential(value credential) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	sealed, err := protect(payload)
	if err != nil {
		return err
	}
	return accountfiles.Write(c.path, sealed)
}

// LoadSaved restores the encrypted credential without contacting the provider.
func (c *Client) LoadSaved() bool {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return false
	}
	plain, err := unprotect(data)
	if err != nil {
		return false
	}
	var saved credential
	if json.Unmarshal(plain, &saved) != nil || !c.acceptSavedOrigin(saved.Base) || !tokenPattern.MatchString(saved.Token) || !validAccount(saved.Account) {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.credential = &saved
	c.state.Account = &saved.Account
	return true
}

func (c *Client) acceptSavedOrigin(base string) bool {
	if base == c.base {
		return true
	}
	// These are the three deployment origins of the same Sintet database. Keep
	// encrypted desktop accounts when moving between local and installer builds.
	if c.ServiceBase != "https://sintet-identity.zzoyn522.workers.dev" {
		return false
	}
	known := func(value string) bool {
		return value == "http://127.0.0.1:3033" || value == "https://sintet.xyz" || value == "https://sintet-identity.zzoyn522.workers.dev"
	}
	return known(base) && known(c.base)
}

func (c *Client) Restore() {
	c.mu.Lock()
	c.state.Checking = true
	c.mu.Unlock()
	defer func() { c.mu.Lock(); c.state.Checking = false; c.mu.Unlock() }()
	if !c.LoadSaved() {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, _ = c.Verify(ctx)
}
func (c *Client) Initialize() { c.mu.Lock(); c.state.Checking = true; c.mu.Unlock(); go c.Restore() }
func (c *Client) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.generation++
	if c.pendingCancel != nil {
		c.pendingCancel()
		c.pendingCancel = nil
	}
	c.state.Pending = false
}
func (c *Client) Start() (string, error) {
	return c.StartWithExcludedAccounts(nil)
}

func (c *Client) StartWithExcludedAccounts(excluded []string) (string, error) {
	base, e := url.Parse(c.base)
	if e != nil || base.User != nil || base.RawQuery != "" || base.Fragment != "" || (base.Scheme != "https" && !(base.Scheme == "http" && base.Hostname() == "127.0.0.1")) {
		return "", errors.New("адрес Sintet ID должен использовать HTTPS или локальный 127.0.0.1")
	}
	c.mu.Lock()
	if c.state.Pending || c.state.Checking {
		c.mu.Unlock()
		return "", errors.New("вход уже выполняется")
	}
	c.mu.Unlock()
	listener, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		return "", e
	}
	verifier, e := randomString()
	if e != nil {
		listener.Close()
		return "", e
	}
	state, e := randomString()
	if e != nil {
		listener.Close()
		return "", e
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	c.mu.Lock()
	c.generation++
	generation := c.generation
	c.pendingCancel = cancel
	c.state.Pending = true
	c.state.Error = ""
	c.mu.Unlock()
	callback := "http://" + listener.Addr().String() + "/sintet/callback"
	server := &http.Server{ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 10 * time.Second, WriteTimeout: 25 * time.Second}
	var once sync.Once
	var callbackMu sync.Mutex
	consumed := false
	finish := func(message string) {
		once.Do(func() {
			c.mu.Lock()
			if c.generation == generation {
				c.state.Pending = false
				c.state.Error = message
				c.pendingCancel = nil
			}
			c.mu.Unlock()
			cancel()
		})
	}
	server.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; frame-ancestors 'none'")
		if r.Method != "GET" || r.URL.Path != "/sintet/callback" || r.Host != listener.Addr().String() || subtle.ConstantTimeCompare([]byte(r.URL.Query().Get("state")), []byte(state)) != 1 {
			http.Error(w, "Invalid callback", 400)
			return
		}
		callbackMu.Lock()
		defer callbackMu.Unlock()
		if consumed {
			http.Error(w, "Code already used", 409)
			return
		}
		if r.URL.Query().Get("error") == "access_denied" {
			consumed = true
			fmt.Fprint(w, callbackHTML("Вход отменён", "Можно вернуться в Sintet."))
			finish("Вход отменён на сайте.")
			return
		}
		code := r.URL.Query().Get("code")
		if !tokenPattern.MatchString(code) {
			http.Error(w, "Invalid code", 400)
			return
		}
		consumed = true
		var result credential
		e := c.request(ctx, "POST", "/api/launcher/token", "", map[string]string{"code": code, "code_verifier": verifier}, &result)
		if e != nil || !validAccount(result.Account) || !tokenPattern.MatchString(result.Token) || !result.Expires.After(time.Now()) {
			http.Error(w, "Не удалось завершить вход. Повторите его из Sintet.", 502)
			finish("Не удалось подтвердить вход. Попробуйте снова.")
			return
		}
		c.mu.Lock()
		if c.generation != generation || ctx.Err() != nil {
			c.mu.Unlock()
			_ = c.request(context.Background(), "DELETE", "/api/launcher/session", result.Token, nil, nil)
			http.Error(w, "Вход отменён", 409)
			return
		}
		result.Base = c.base
		if e = c.storeCredential(result); e != nil {
			c.mu.Unlock()
			http.Error(w, "Не удалось безопасно сохранить сессию.", 500)
			_ = c.request(context.Background(), "DELETE", "/api/launcher/session", result.Token, nil, nil)
			finish("Не удалось безопасно сохранить сессию.")
			return
		}
		c.credential = &result
		c.state.Account = &result.Account
		c.mu.Unlock()
		fmt.Fprint(w, callbackHTML("Ты в Sintet.", "Вход выполнен. Возвращайся в лаунчер – эту вкладку можно закрыть."))
		finish("")
		if c.OnSuccess != nil {
			go c.OnSuccess()
		}
	})
	go func() { _ = server.Serve(listener) }()
	go func() {
		<-ctx.Done()
		time.Sleep(150 * time.Millisecond)
		_ = server.Close()
		finish("Время ожидания истекло. Повторите вход.")
	}()
	authorize := base.ResolveReference(&url.URL{Path: "/launcher/authorize"})
	q := url.Values{"redirect_uri": {callback}, "state": {state}, "code_challenge": {challenge(verifier)}}
	if len(excluded) > 0 {
		q.Set("excluded", strings.Join(excluded, ","))
	}
	authorize.RawQuery = q.Encode()
	return authorize.String(), nil
}
func (c *Client) Logout() error {
	c.Cancel()
	c.mu.Lock()
	saved := c.credential
	c.mu.Unlock()
	if saved != nil {
		if e := c.request(context.Background(), "DELETE", "/api/launcher/session", saved.Token, nil, nil); e != nil {
			return e
		}
	}
	if e := os.Remove(c.path); e != nil && !os.IsNotExist(e) {
		return e
	}
	c.mu.Lock()
	c.credential = nil
	c.state = State{}
	c.mu.Unlock()
	return nil
}
func callbackHTML(title, subtitle string) string {
	return `<!doctype html><html lang="ru"><meta charset="utf-8"><meta name="viewport" content="width=device-width"><title>Sintet ID</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#0d0d0d;color:#eee;font:18px system-ui}main{padding:40px;max-width:540px}b{display:block;font-size:32px;margin-bottom:50px}h1{font-size:46px;letter-spacing:-2px}p{color:#999;line-height:1.7}</style><main><b>Sintet · ID</b><h1>` + title + `</h1><p>` + subtitle + `</p></main></html>`
}

// GameIdentity keeps the private session token inside Go, outside Wails state.
func (c *Client) GameIdentity(ctx context.Context) (*Account, string, error) {
	account, err := c.Verify(ctx)
	if err != nil || account == nil {
		return account, "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.credential == nil || c.credential.Account.ID != account.ID {
		return nil, "", errors.New("сессия изменилась")
	}
	return account, c.credential.Token, nil
}
func (c *Client) ServerURL() string {
	base := c.base
	if c.ServiceBase != "" {
		base = strings.TrimRight(c.ServiceBase, "/")
	}
	return base + "/api/minecraft"
}

// Forget removes the desktop credential even if the provider is offline.
func (c *Client) Forget() error {
	c.Cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	saved := c.credential
	c.credential = nil
	c.state = State{}
	if saved != nil {
		go func() { _ = c.request(context.Background(), "DELETE", "/api/launcher/session", saved.Token, nil, nil) }()
	}
	return nil
}
