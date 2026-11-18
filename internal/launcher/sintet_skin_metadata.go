package launcher

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"time"

	"sintet/internal/accountfiles"
)

const sintetSkinWorker = "https://sintet-identity.zzoyn522.workers.dev/api/minecraft"
const sintetSkinSite = "https://sintet.xyz/api/minecraft"
const skinMetadataLimit = 1 << 20

var errSintetSkinMetadata = errors.New("не удалось получить настройки сервера скинов Sintet ID")

func (s *Service) prepareSintetSkin(ctx context.Context, api string) (agent, metadata, note string, err error) {
	if err = validateSkinServer(api); err != nil {
		return
	}
	agent, err = s.prepareSkinAgent(ctx)
	if err != nil {
		return
	}
	raw, note, err := s.sintetSkinMetadata(ctx, api)
	if err != nil {
		return "", "", "", err
	}
	return agent, base64.StdEncoding.EncodeToString(raw), note, nil
}

func (s *Service) sintetSkinMetadata(ctx context.Context, api string) ([]byte, string, error) {
	endpoints := []string{api, api}
	switch api {
	case sintetSkinWorker:
		endpoints[1] = sintetSkinSite
	case sintetSkinSite:
		endpoints[1] = sintetSkinWorker
	}
	cache := filepath.Join(s.Root, ".sintet", "authlib-injector", fmt.Sprintf("skin-%x.json", sha256.Sum256([]byte(api))))
	var last error
	for i, endpoint := range endpoints {
		if err := ctx.Err(); err != nil {
			return nil, "", err
		}
		call, cancel := context.WithTimeout(ctx, 4*time.Second)
		raw, err := s.fetchSintetSkinMetadata(call, endpoint, api)
		cancel()
		if err != nil {
			last = err
			continue
		}
		_ = accountfiles.Write(cache, raw)
		if i > 0 && endpoint != api {
			return raw, "Использованы настройки с резервного адреса Sintet ID", nil
		}
		return raw, "", nil
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}
	if info, err := os.Lstat(cache); err == nil && info.Mode().IsRegular() &&
		info.Size() <= skinMetadataLimit && time.Since(info.ModTime()) <= 7*24*time.Hour {
		if data, err := os.ReadFile(cache); err == nil && len(data) <= skinMetadataLimit {
			if cached, err := validateSintetMetadata(data, api); err == nil {
				return cached, "Использованы сохранённые настройки Sintet ID", nil
			}
		}
	}
	return nil, "", fmt.Errorf("%w: %v", errSintetSkinMetadata, last)
}

func (s *Service) fetchSintetSkinMetadata(ctx context.Context, endpoint, api string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	client := *s.HTTP
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	response, err := client.Do(request)
	if err != nil {
		if ctx.Err() != nil {
			return nil, errors.New("сервер не ответил вовремя")
		}
		return nil, errors.New("не удалось подключиться к серверу")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("сервер ответил HTTP %d", response.StatusCode)
	}
	raw, err := io.ReadAll(io.LimitReader(response.Body, skinMetadataLimit+1))
	if err != nil || len(raw) > skinMetadataLimit {
		return nil, errors.New("не удалось прочитать настройки скинов")
	}
	return validateSintetMetadata(raw, api)
}

func validateSintetMetadata(raw []byte, api string) ([]byte, error) {
	var document map[string]json.RawMessage
	var signature string
	var domains []string
	if json.Unmarshal(raw, &document) != nil || json.Unmarshal(document["signaturePublickey"], &signature) != nil || signature == "" ||
		json.Unmarshal(document["skinDomains"], &domains) != nil || len(domains) == 0 {
		return nil, errors.New("сервер вернул неверные настройки скинов")
	}
	parsed, err := url.Parse(api)
	if err != nil {
		return nil, err
	}
	if !slices.Contains(domains, parsed.Hostname()) {
		domains = append(domains, parsed.Hostname())
		document["skinDomains"], _ = json.Marshal(domains)
		return json.Marshal(document)
	}
	return raw, nil
}
