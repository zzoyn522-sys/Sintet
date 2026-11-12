package identity

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"sintet/internal/accountfiles"
)

const microsoftOAuth = "https://login.microsoftonline.com/consumers/oauth2/v2.0"
const microsoftScope = "XboxLive.signin XboxLive.offline_access"
const minecraftServices = "https://api.minecraftservices.com"

type MicrosoftPrompt struct {
	Code string `json:"code"`
	URL  string `json:"-"`
}
type microsoftDevice struct {
	DeviceCode      string `json:"device_code"`
	UserCode        string `json:"user_code"`
	VerificationURI string `json:"verification_uri"`
	Expires         int    `json:"expires_in"`
	Interval        int    `json:"interval"`
}
type microsoftTokens struct {
	Access  string `json:"access_token"`
	Refresh string `json:"refresh_token"`
}
type microsoftSaved struct {
	Account  Account   `json:"account"`
	ClientID string    `json:"clientId"`
	Refresh  string    `json:"refresh"`
	Access   string    `json:"access"`
	Expires  time.Time `json:"expires"`
	XUID     string    `json:"xuid,omitempty"`
}
type MicrosoftClient struct {
	path, clientID string
	HTTP           *http.Client
	mu             sync.Mutex
	saved          *microsoftSaved
	pending        bool
	cancel         context.CancelFunc
	message        string
}

func NewMicrosoft(path, clientID string) *MicrosoftClient {
	return &MicrosoftClient{path: path, clientID: strings.TrimSpace(clientID), HTTP: &http.Client{
		Timeout:       15 * time.Second,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

func (c *MicrosoftClient) State() State {
	c.mu.Lock()
	defer c.mu.Unlock()
	state := State{Provider: "microsoft", Pending: c.pending, SessionSaved: c.saved != nil, Error: c.message}
	if c.saved != nil {
		account := c.saved.Account
		state.Account = &account
	}
	return state
}

func (c *MicrosoftClient) LoadSaved() bool {
	data, err := os.ReadFile(c.path)
	if err != nil {
		return false
	}
	plain, err := unprotect(data)
	if err != nil {
		return false
	}
	var saved microsoftSaved
	if json.Unmarshal(plain, &saved) != nil || !validAccount(saved.Account) || !uuidPattern.MatchString(saved.ClientID) || saved.Refresh == "" {
		return false
	}
	c.mu.Lock()
	c.saved = &saved
	c.mu.Unlock()
	return true
}

type microsoftFailure struct {
	status int
	code   string
	xbox   uint64
}

func (e *microsoftFailure) Error() string {
	return fmt.Sprintf("сервис Microsoft ответил HTTP %d", e.status)
}

func (c *MicrosoftClient) request(ctx context.Context, method, endpoint, bearer string, body any, out any) error {
	var reader io.Reader
	var contentType string
	switch data := body.(type) {
	case nil:
	case url.Values:
		reader = strings.NewReader(data.Encode())
		contentType = "application/x-www-form-urlencoded"
	default:
		raw, err := json.Marshal(data)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(raw)
		contentType = "application/json"
	}
	request, err := http.NewRequestWithContext(ctx, method, endpoint, reader)
	if err != nil {
		return err
	}
	request.Header.Set("Accept", "application/json")
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	if bearer != "" {
		request.Header.Set("Authorization", "Bearer "+bearer)
	}
	if strings.HasSuffix(request.URL.Hostname(), ".xboxlive.com") {
		request.Header.Set("x-xbl-contract-version", "1")
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return errors.New("не удалось связаться с Microsoft или Minecraft")
	}
	defer response.Body.Close()
	const limit = 256 << 10
	raw, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if err != nil || len(raw) > limit {
		return errors.New("не удалось прочитать ответ сервиса")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		var problem struct {
			Code string `json:"error"`
			Xbox uint64 `json:"XErr"`
		}
		_ = json.Unmarshal(raw, &problem)
		return &microsoftFailure{status: response.StatusCode, code: problem.Code, xbox: problem.Xbox}
	}
	if err = json.Unmarshal(raw, out); err != nil {
		return errors.New("сервис вернул неверный ответ")
	}
	return nil
}

func (c *MicrosoftClient) Start() (MicrosoftPrompt, error) {
	if !uuidPattern.MatchString(c.clientID) {
		return MicrosoftPrompt{}, errors.New("Вход Microsoft пока недоступен. Нужен Client ID Sintet с допуском Minecraft Services.")
	}
	var device microsoftDevice
	err := c.request(context.Background(), http.MethodPost, microsoftOAuth+"/devicecode", "", url.Values{
		"client_id": {c.clientID}, "scope": {microsoftScope},
	}, &device)
	if err != nil {
		return MicrosoftPrompt{}, err
	}
	verify, err := url.Parse(device.VerificationURI)
	if err != nil || verify.Scheme != "https" || verify.User != nil || (verify.Host != "www.microsoft.com" && verify.Host != "microsoft.com") ||
		(verify.Path != "/link" && verify.Path != "/devicelogin") ||
		device.DeviceCode == "" || device.UserCode == "" || device.Expires <= 0 {
		return MicrosoftPrompt{}, errors.New("Microsoft вернул неверный запрос входа")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Duration(min(device.Expires, 900))*time.Second)
	c.mu.Lock()
	c.pending, c.cancel, c.message = true, cancel, ""
	c.mu.Unlock()
	go func() {
		defer cancel()
		tokens, err := c.poll(ctx, device)
		var saved microsoftSaved
		if err == nil {
			saved, err = c.exchange(ctx, c.clientID, tokens)
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		if !c.pending {
			return
		}
		c.pending, c.cancel = false, nil
		if err == nil && ctx.Err() == nil {
			err = c.save(saved)
		}
		if err != nil {
			c.message = err.Error()
		} else if ctx.Err() != nil {
			c.message = "Время входа истекло. Попробуй ещё раз."
		}
	}()
	return MicrosoftPrompt{Code: device.UserCode, URL: verify.String()}, nil
}

func (c *MicrosoftClient) poll(ctx context.Context, device microsoftDevice) (microsoftTokens, error) {
	interval := time.Duration(max(5, device.Interval)) * time.Second
	timer := time.NewTimer(interval)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return microsoftTokens{}, errors.New("Время входа истекло. Попробуй ещё раз.")
		case <-timer.C:
		}
		var token microsoftTokens
		err := c.request(ctx, http.MethodPost, microsoftOAuth+"/token", "", url.Values{
			"grant_type": {"urn:ietf:params:oauth:grant-type:device_code"}, "client_id": {c.clientID}, "device_code": {device.DeviceCode},
		}, &token)
		if err == nil {
			return token, nil
		}
		var failure *microsoftFailure
		if errors.As(err, &failure) {
			switch failure.code {
			case "authorization_pending":
			case "slow_down":
				interval += 5 * time.Second
			case "authorization_declined", "access_denied":
				return microsoftTokens{}, errors.New("Вход отклонён в браузере.")
			case "expired_token":
				return microsoftTokens{}, errors.New("Код Microsoft истёк. Попробуй ещё раз.")
			default:
				return microsoftTokens{}, err
			}
		} else {
			return microsoftTokens{}, err
		}
		timer.Reset(interval)
	}
}

type xboxReply struct {
	Token         string `json:"Token"`
	DisplayClaims struct {
		Users []struct {
			Hash string `json:"uhs"`
			XUID string `json:"xid"`
		} `json:"xui"`
	} `json:"DisplayClaims"`
}

func (c *MicrosoftClient) exchange(ctx context.Context, clientID string, tokens microsoftTokens) (microsoftSaved, error) {
	if tokens.Access == "" || tokens.Refresh == "" {
		return microsoftSaved{}, errors.New("Microsoft не выдал сессию")
	}
	var xbox xboxReply
	err := c.request(ctx, http.MethodPost, "https://user.auth.xboxlive.com/user/authenticate", "", map[string]any{
		"RelyingParty": "http://auth.xboxlive.com", "TokenType": "JWT",
		"Properties": map[string]string{"AuthMethod": "RPS", "SiteName": "user.auth.xboxlive.com", "RpsTicket": "d=" + tokens.Access},
	}, &xbox)
	if err != nil {
		return microsoftSaved{}, err
	}
	if xbox.Token == "" {
		return microsoftSaved{}, errors.New("Xbox не выдал токен")
	}
	var xsts xboxReply
	err = c.request(ctx, http.MethodPost, "https://xsts.auth.xboxlive.com/xsts/authorize", "", map[string]any{
		"RelyingParty": "rp://api.minecraftservices.com/", "TokenType": "JWT",
		"Properties": map[string]any{"SandboxId": "RETAIL", "UserTokens": []string{xbox.Token}},
	}, &xsts)
	if err != nil {
		var failure *microsoftFailure
		if errors.As(err, &failure) {
			switch failure.xbox {
			case 2148916233:
				return microsoftSaved{}, errors.New("У этого аккаунта ещё нет профиля Xbox.")
			case 2148916238:
				return microsoftSaved{}, errors.New("Для этого аккаунта Xbox требуется разрешение организатора семьи.")
			}
		}
		return microsoftSaved{}, err
	}
	if xsts.Token == "" || len(xsts.DisplayClaims.Users) != 1 || xsts.DisplayClaims.Users[0].Hash == "" {
		return microsoftSaved{}, errors.New("Xbox вернул неполный профиль")
	}
	claim := xsts.DisplayClaims.Users[0]
	var game struct {
		Access  string `json:"access_token"`
		Expires int    `json:"expires_in"`
	}
	err = c.request(ctx, http.MethodPost, minecraftServices+"/authentication/login_with_xbox", "", map[string]string{
		"identityToken": "XBL3.0 x=" + claim.Hash + ";" + xsts.Token,
	}, &game)
	if err != nil {
		var failure *microsoftFailure
		if errors.As(err, &failure) && failure.status == http.StatusForbidden {
			return microsoftSaved{}, errors.New("Minecraft Services не разрешил вход для Client ID Sintet. Проверь допуск приложения.")
		}
		return microsoftSaved{}, err
	}
	if game.Access == "" || game.Expires <= 0 {
		return microsoftSaved{}, errors.New("Minecraft не выдал игровой токен")
	}
	if err = c.checkMinecraftLicense(ctx, game.Access); err != nil {
		return microsoftSaved{}, err
	}
	var profile struct {
		ID   string `json:"id"`
		Name string `json:"name"`
	}
	err = c.request(ctx, http.MethodGet, minecraftServices+"/minecraft/profile", game.Access, nil, &profile)
	if err != nil {
		var failure *microsoftFailure
		if errors.As(err, &failure) && failure.status == http.StatusNotFound {
			return microsoftSaved{}, errors.New("Minecraft Java Edition не нашёл игровой профиль. Проверь лицензию и создай ник в официальном лаунчере.")
		}
		return microsoftSaved{}, err
	}
	account, err := elyAccount(ElyProfile{ID: profile.ID, Name: profile.Name})
	if err != nil {
		return microsoftSaved{}, errors.New("Minecraft вернул неверный игровой профиль")
	}
	return microsoftSaved{Account: account, ClientID: clientID, Refresh: tokens.Refresh, Access: game.Access, Expires: time.Now().Add(time.Duration(game.Expires) * time.Second), XUID: claim.XUID}, nil
}

func (c *MicrosoftClient) checkMinecraftLicense(ctx context.Context, token string) error {
	var requestID [16]byte
	if _, err := rand.Read(requestID[:]); err != nil {
		return err
	}
	requestID[6] = requestID[6]&0x0f | 0x40
	requestID[8] = requestID[8]&0x3f | 0x80
	id := hex.EncodeToString(requestID[:])
	id = id[:8] + "-" + id[8:12] + "-" + id[12:16] + "-" + id[16:20] + "-" + id[20:]
	var license struct {
		Items []struct {
			Name string `json:"name"`
		} `json:"items"`
	}
	endpoint := minecraftServices + "/entitlements/license?requestId=" + id
	if err := c.request(ctx, http.MethodGet, endpoint, token, nil, &license); err != nil {
		return fmt.Errorf("не удалось проверить лицензию Minecraft Java Edition: %w", err)
	}
	for _, item := range license.Items {
		if item.Name == "game_minecraft" || item.Name == "product_minecraft" {
			return nil
		}
	}
	return errors.New("У этого аккаунта нет доступа к Minecraft Java Edition.")
}

func (c *MicrosoftClient) save(saved microsoftSaved) error {
	plain, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	sealed, err := protect(plain)
	if err != nil {
		return errors.New("не удалось защитить сессию Microsoft")
	}
	if err = accountfiles.Write(c.path, sealed); err != nil {
		return errors.New("не удалось сохранить сессию Microsoft")
	}
	c.saved = &saved
	return nil
}

func (c *MicrosoftClient) GameIdentity(ctx context.Context) (Account, string, string, string, error) {
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.saved == nil {
		return Account{}, "", "", "", errors.New("войдите через Microsoft")
	}
	saved := *c.saved
	if saved.Access != "" && time.Until(saved.Expires) > 2*time.Minute {
		return saved.Account, saved.Access, saved.ClientID, saved.XUID, nil
	}
	var tokens microsoftTokens
	if err := c.request(ctx, http.MethodPost, microsoftOAuth+"/token", "", url.Values{
		"grant_type": {"refresh_token"}, "client_id": {saved.ClientID}, "scope": {microsoftScope}, "refresh_token": {saved.Refresh},
	}, &tokens); err != nil {
		return Account{}, "", "", "", errors.New("не удалось обновить вход Microsoft. Проверь подключение или добавь аккаунт заново.")
	}
	if tokens.Refresh == "" {
		tokens.Refresh = saved.Refresh
	}
	updated, err := c.exchange(ctx, saved.ClientID, tokens)
	if err != nil {
		return Account{}, "", "", "", err
	}
	if updated.Account.ID != saved.Account.ID {
		return Account{}, "", "", "", errors.New("игровой профиль Microsoft изменился")
	}
	if err = c.save(updated); err != nil {
		return Account{}, "", "", "", err
	}
	return updated.Account, updated.Access, updated.ClientID, updated.XUID, nil
}

func (c *MicrosoftClient) Cancel() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.cancel != nil {
		c.cancel()
		c.cancel = nil
	}
	c.pending = false
}
func (c *MicrosoftClient) Forget() error {
	c.Cancel()
	c.mu.Lock()
	defer c.mu.Unlock()
	if err := os.Remove(c.path); err != nil && !os.IsNotExist(err) {
		return err
	}
	c.saved = nil
	return nil
}
