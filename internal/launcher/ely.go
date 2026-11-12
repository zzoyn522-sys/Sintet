package launcher

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
)

const elyInjectorAPI = "https://account.ely.by/api/authlib-injector"
const injectorSHA256 = "9c7f4343e6c82034958ffb48c14a2cb0c85928be7283103ce17da00c6d5a7b10"

func validateSkinServer(api string) error {
	u, e := url.Parse(api)
	if e != nil || u.User != nil || u.RawQuery != "" || u.Fragment != "" || u.Hostname() == "" || (u.Scheme != "https" && !(u.Scheme == "http" && u.Hostname() == "127.0.0.1")) {
		return errors.New("неверный адрес сервера скинов")
	}
	return nil
}
func (s *Service) prepareEly(ctx context.Context) (string, string, error) {
	return s.prepareSkinServer(ctx, elyInjectorAPI)
}
func (s *Service) prepareSkinServer(ctx context.Context, api string) (string, string, error) {
	if e := validateSkinServer(api); e != nil {
		return "", "", e
	}
	file := File{Path: ".sintet/authlib-injector/authlib-injector-1.2.8.jar", URL: "https://github.com/yushijinhun/authlib-injector/releases/download/v1.2.8/authlib-injector-1.2.8.jar", SHA256: injectorSHA256, Size: 349681}
	s.stage("Подключение скинов")
	if e := s.download(ctx, file, nil); e != nil {
		return "", "", e
	}
	r, e := http.NewRequestWithContext(ctx, "GET", api, nil)
	if e != nil {
		return "", "", e
	}
	res, e := s.HTTP.Do(r)
	if e != nil {
		return "", "", errors.New("не удалось получить настройки сервера скинов")
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, 1048577))
	if e != nil || res.StatusCode != 200 || len(raw) > 1048576 || !json.Valid(raw) {
		return "", "", errors.New("сервер вернул неверные настройки скинов")
	}
	return filepath.Join(s.Root, filepath.FromSlash(file.Path)), base64.StdEncoding.EncodeToString(raw), nil
}

// Keep token fragments across stdout/stderr writes so no partial write can leak it.
type tokenRedactor struct {
	mu      sync.Mutex
	target  io.Writer
	secret  string
	pending []byte
}

func (w *tokenRedactor) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.secret == "" {
		return w.target.Write(data)
	}
	w.pending = append(w.pending, data...)
	pending := strings.ReplaceAll(string(w.pending), w.secret, "[REDACTED]")
	keep := min(len(pending), len(w.secret)-1)
	for keep > 0 && !strings.HasSuffix(pending, w.secret[:keep]) {
		keep--
	}
	safe := len(pending) - keep
	_, e := io.WriteString(w.target, pending[:safe])
	w.pending = append(w.pending[:0], pending[safe:]...)
	return len(data), e
}
func (w *tokenRedactor) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	if len(w.pending) == 0 {
		return
	}
	_, _ = io.WriteString(w.target, strings.ReplaceAll(string(w.pending), w.secret, "[REDACTED]"))
	w.pending = nil
}
