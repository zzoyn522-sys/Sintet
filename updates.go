package main

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sintet/internal/identity"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Set these values at release time with Go linker flags. The signing key never ships.
var launcherVersion = "0.9.25.11"
var updatePublicKeyHex = "6465d83b4eece79f1bae8ef11c302271f9c79683c4b3cf38f5a2e5c8fa5ae99f"
var updateHealthMarker string
var targetedBootstrapAccountID string
var launcherChannel string

const updateManifestURL = "https://sintet-updates.zzoyn522.workers.dev/stable/latest.json"

type signedUpdateEnvelope struct {
	Payload   string `json:"payload"`
	Signature string `json:"signature"`
}

type updatePayload struct {
	Version  string   `json:"version"`
	Channel  string   `json:"channel,omitempty"`
	Notes    []string `json:"notes,omitempty"`
	URL      string   `json:"url"`
	SHA256   string   `json:"sha256"`
	Size     int64    `json:"size"`
	Released string   `json:"releasedAt,omitempty"`
}

type updateView struct {
	CurrentVersion string   `json:"currentVersion"`
	LatestVersion  string   `json:"latestVersion,omitempty"`
	LatestLabel    string   `json:"latestLabel,omitempty"`
	Notes          []string `json:"notes,omitempty"`
	Available      bool     `json:"available"`
	Checking       bool     `json:"checking"`
	Downloading    bool     `json:"downloading"`
	Percent        float64  `json:"percent"`
	Downloaded     int64    `json:"downloaded"`
	Total          int64    `json:"total"`
	Stage          string   `json:"stage,omitempty"`
	Error          string   `json:"error,omitempty"`
}

type updateManager struct {
	mu      sync.RWMutex
	state   updateView
	payload updatePayload
	client  *http.Client
	root    string
}

func newUpdateManager(root string) *updateManager {
	return &updateManager{root: root, client: &http.Client{}, state: updateView{CurrentVersion: launcherVersion}}
}

func (a *App) CheckForUpdates() updateView {
	if a.updates == nil {
		return updateView{CurrentVersion: launcherVersion, Error: "Проверка обновлений недоступна"}
	}
	return a.updates.check(contextWithoutCancel(), a.activeUpdateClient())
}

func (a *App) activeUpdateClient() *identity.Client {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if a.profiles == nil {
		return nil
	}
	slot := a.profiles.find(a.profiles.active)
	if slot == nil || slot.Provider != "sintet" {
		return nil
	}
	return slot.sintet
}

func (a *App) LauncherVersion() string { return launcherVersion }
func (a *App) LauncherDisplayVersion() string {
	return releaseVersionLabel(launcherVersion, launcherChannel)
}
func releaseVersionLabel(version, channel string) string {
	if channel == "test" {
		parts := strings.Split(version, ".")
		if len(parts) > 3 {
			version = strings.Join(parts[:3], ".")
		}
		return version + " TEST"
	}
	return version
}

func (a *App) UpdateState() updateView {
	if a.updates == nil {
		return updateView{CurrentVersion: launcherVersion}
	}
	return a.updates.view()
}

func (a *App) InstallUpdate() error {
	if a.updates == nil {
		return errors.New("Обновление недоступно")
	}
	a.updates.mu.RLock()
	payload := a.updates.payload
	a.updates.mu.RUnlock()
	if payload.Channel == "test" {
		client := a.activeUpdateClient()
		if client == nil {
			return errors.New("тестовое обновление доступно только выбранному Sintet ID")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Second)
		offer, err := client.UpdateOffer(ctx)
		cancel()
		if err != nil {
			return err
		}
		current, err := parseSignedUpdate(offer)
		if err != nil || current.Version != payload.Version || current.SHA256 != payload.SHA256 {
			a.updates.mu.Lock()
			a.updates.state.Available = false
			a.updates.mu.Unlock()
			return errors.New("это тестовое обновление не назначено текущему аккаунту")
		}
	}
	return a.updates.install(a)
}

func (u *updateManager) view() updateView {
	u.mu.RLock()
	defer u.mu.RUnlock()
	v := u.state
	v.Notes = append([]string(nil), v.Notes...)
	return v
}

func (u *updateManager) check(ctx context.Context, accounts ...*identity.Client) updateView {
	u.mu.Lock()
	if u.state.Checking || u.state.Downloading {
		v := u.state
		u.mu.Unlock()
		return v
	}
	u.state = updateView{CurrentVersion: launcherVersion, Checking: true}
	u.mu.Unlock()

	checkCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(checkCtx, http.MethodGet, updateManifestURL, nil)
	if err != nil {
		return u.checkFailed(err)
	}
	checkClient := &http.Client{Timeout: 18 * time.Second}
	resp, err := checkClient.Do(req)
	if err != nil {
		return u.checkFailed(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return u.checkFailed(fmt.Errorf("сервер обновлений ответил %s", resp.Status))
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64*1024+1))
	if err != nil || len(data) > 64*1024 {
		return u.checkFailed(errors.New("не удалось прочитать манифест обновлений"))
	}
	payload, err := parseSignedUpdate(data)
	if err != nil {
		return u.checkFailed(err)
	}
	if len(accounts) > 0 && accounts[0] != nil {
		private, offerErr := accounts[0].UpdateOffer(checkCtx)
		if offerErr == nil && len(private) > 0 && string(private) != "null" {
			candidate, validationErr := parseSignedUpdate(private)
			if validationErr == nil && versionIsNewer(candidate.Version, payload.Version) {
				payload = candidate
			}
		}
	}
	u.mu.Lock()
	u.payload = payload
	u.state = updateView{CurrentVersion: launcherVersion, LatestVersion: payload.Version, LatestLabel: releaseVersionLabel(payload.Version, payload.Channel), Notes: append([]string(nil), payload.Notes...), Available: versionIsNewer(payload.Version, launcherVersion)}
	v := u.state
	u.mu.Unlock()
	return v
}

func parseSignedUpdate(data []byte) (updatePayload, error) {
	var envelope signedUpdateEnvelope
	if len(data) > 64*1024 || json.Unmarshal(data, &envelope) != nil {
		return updatePayload{}, errors.New("манифест обновлений повреждён")
	}
	pub, err := hex.DecodeString(updatePublicKeyHex)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return updatePayload{}, errors.New("проверка обновлений не настроена в этой сборке")
	}
	payloadBytes, err := base64.StdEncoding.DecodeString(envelope.Payload)
	if err != nil {
		return updatePayload{}, errors.New("подпись манифеста некорректна")
	}
	sig, err := base64.StdEncoding.DecodeString(envelope.Signature)
	if err != nil || !ed25519.Verify(ed25519.PublicKey(pub), payloadBytes, sig) {
		return updatePayload{}, errors.New("подпись манифеста не прошла проверку")
	}
	var payload updatePayload
	if err = json.Unmarshal(payloadBytes, &payload); err != nil {
		return updatePayload{}, errors.New("содержимое манифеста некорректно")
	}
	if !validUpdatePayload(payload) {
		return updatePayload{}, errors.New("манифест содержит недопустимые параметры")
	}
	return payload, nil
}

func contextWithoutCancel() context.Context { return context.Background() }

func (u *updateManager) checkFailed(err error) updateView {
	u.mu.Lock()
	u.state = updateView{CurrentVersion: launcherVersion, Error: err.Error()}
	view := u.state
	u.mu.Unlock()
	return view
}

func validUpdatePayload(p updatePayload) bool {
	if p.Channel != "" && p.Channel != "test" {
		return false
	}
	if !versionIsNewer(p.Version, "0.0.0") || p.Size < 1 || p.Size > 350*1024*1024 || len(p.SHA256) != 64 {
		return false
	}
	if _, err := hex.DecodeString(p.SHA256); err != nil {
		return false
	}
	parsed, err := http.NewRequest(http.MethodGet, p.URL, nil)
	return err == nil && parsed.URL.Scheme == "https" && parsed.URL.Host == "sintet-updates.zzoyn522.workers.dev" && parsed.URL.Path == "/stable/Sintet-"+p.Version+".exe" && parsed.URL.RawQuery == "" && parsed.URL.User == nil
}

func versionIsNewer(candidate, current string) bool {
	parse := func(s string) ([]int, bool) {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(s), "v"), ".")
		if len(parts) < 2 || len(parts) > 4 {
			return nil, false
		}
		out := make([]int, 4)
		for i, part := range parts {
			n, err := strconv.Atoi(part)
			if err != nil || n < 0 {
				return nil, false
			}
			out[i] = n
		}
		return out, true
	}
	a, okA := parse(candidate)
	b, okB := parse(current)
	if !okA || !okB {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}

func (u *updateManager) install(app *App) error {
	u.mu.Lock()
	if !u.state.Available || u.state.Downloading {
		u.mu.Unlock()
		return errors.New("Нет доступного обновления")
	}
	payload := u.payload
	u.state.Downloading = true
	u.state.Stage = "Подготовка обновления"
	u.state.Percent = 0
	u.state.Error = ""
	u.mu.Unlock()
	go u.downloadAndApply(app, payload)
	return nil
}

func (u *updateManager) setProgress(stage string, percent float64, downloaded, total int64) {
	u.mu.Lock()
	u.state.Stage = stage
	u.state.Percent = percent
	u.state.Downloaded = downloaded
	u.state.Total = total
	u.mu.Unlock()
}

func (u *updateManager) fail(err error) {
	u.mu.Lock()
	u.state.Downloading = false
	u.state.Stage = ""
	u.state.Error = err.Error()
	u.mu.Unlock()
}

func (u *updateManager) downloadAndApply(app *App, p updatePayload) {
	if err := os.MkdirAll(u.root, 0700); err != nil {
		u.fail(err)
		return
	}
	temp, err := os.CreateTemp(u.root, "Sintet-update-*.exe.part")
	if err != nil {
		u.fail(err)
		return
	}
	tempName := temp.Name()
	keepTempForHelper := false
	defer func() {
		if !keepTempForHelper {
			_ = os.Remove(tempName)
		}
	}()
	defer temp.Close()
	downloadCtx, cancel := context.WithTimeout(context.Background(), 15*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(downloadCtx, http.MethodGet, p.URL, nil)
	if err != nil {
		u.fail(err)
		return
	}
	resp, err := u.client.Do(req)
	if err != nil {
		u.fail(err)
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		u.fail(fmt.Errorf("не удалось скачать обновление: %s", resp.Status))
		return
	}
	if resp.ContentLength > 0 && resp.ContentLength != p.Size {
		u.fail(errors.New("размер обновления не совпадает с манифестом"))
		return
	}
	u.setProgress("Скачивание Sintet", 0, 0, p.Size)
	h := sha256.New()
	reader := &updateProgressReader{r: resp.Body, total: p.Size, progress: func(downloaded, total int64, percent float64) {
		u.setProgress("Скачивание Sintet", percent, downloaded, total)
	}}
	written, err := io.Copy(io.MultiWriter(temp, h), reader)
	if err != nil {
		u.fail(err)
		return
	}
	if written != p.Size || hex.EncodeToString(h.Sum(nil)) != strings.ToLower(p.SHA256) {
		u.fail(errors.New("файл обновления не прошёл проверку целостности"))
		return
	}
	if err = temp.Sync(); err != nil {
		u.fail(err)
		return
	}
	if err = temp.Close(); err != nil {
		u.fail(err)
		return
	}
	current, err := os.Executable()
	if err != nil {
		u.fail(err)
		return
	}
	helper := filepath.Join(u.root, "Sintet-Updater-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".exe")
	if err = copyFile(current, helper); err != nil {
		u.fail(err)
		return
	}
	defer func() {
		if !keepTempForHelper {
			_ = os.Remove(helper)
		}
	}()

	// Keep new game launches from racing the verified update handoff.
	app.contentMu.Lock()
	defer app.contentMu.Unlock()
	app.mu.RLock()
	game := app.service
	app.mu.RUnlock()
	if state := game.State(); state.Busy || state.Running {
		u.setProgress("Ждём завершения Minecraft", 100, written, p.Size)
		closeCtx, cancelClose := context.WithTimeout(context.Background(), 2*time.Minute)
		err = game.CloseGame(closeCtx)
		cancelClose()
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				err = errors.New("Minecraft не завершился. Закройте игру и повторите обновление.")
			}
			u.fail(err)
			return
		}
	}
	if app.forceQuit.Load() {
		u.fail(context.Canceled)
		return
	}
	u.setProgress("Установка обновления", 100, written, p.Size)
	marker := filepath.Join(u.root, "update-health-"+strconv.FormatInt(time.Now().UnixNano(), 10)+".ok")
	cmd := exec.Command(helper, "--sintet-update-helper", current, tempName, strconv.Itoa(os.Getpid()), marker)
	cmd.Dir = u.root
	if err = cmd.Start(); err != nil {
		u.fail(err)
		return
	}
	keepTempForHelper = true
	if app.ctx != nil {
		app.ExitLauncher()
	}
}

type updateProgressReader struct {
	r        io.Reader
	total    int64
	read     int64
	progress func(int64, int64, float64)
}

func (r *updateProgressReader) Read(p []byte) (int, error) {
	n, err := r.r.Read(p)
	r.read += int64(n)
	if r.total > 0 {
		r.progress(r.read, r.total, float64(r.read)*100/float64(r.total))
	}
	return n, err
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return copyErr
	}
	if syncErr != nil {
		_ = os.Remove(dst)
		return syncErr
	}
	return closeErr
}
