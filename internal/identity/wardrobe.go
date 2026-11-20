package identity

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"regexp"
	"strings"
)

const (
	skinRoute  = "/api/account/skin"
	capeRoute  = "/api/account/capes"
	MaxSkinPNG = 256 << 10
)

type WardrobeSkin struct {
	URL   string `json:"url"`
	Key   string `json:"key"`
	Model string `json:"model"`
}

type SkinWardrobe struct {
	Skin     WardrobeSkin   `json:"skin"`
	Wardrobe []WardrobeSkin `json:"wardrobe"`
}

type WardrobeCape struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	URL  string `json:"url"`
}

type CapeWardrobe struct {
	Capes    []WardrobeCape `json:"capes"`
	Catalog  []WardrobeCape `json:"catalog"`
	Equipped string         `json:"equipped"`
}

func (c *Client) wardrobeRequest(ctx context.Context, id, method, route, contentType string, body io.Reader, out any) error {
	c.mu.Lock()
	if c.credential == nil || c.credential.Account.ID != id {
		c.mu.Unlock()
		return errors.New("Аккаунт изменился. Повторите действие")
	}
	token := c.credential.Token
	c.mu.Unlock()
	request, err := http.NewRequestWithContext(ctx, method, c.base+route, body)
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+token)
	request.Header.Set("X-Sintet-Account", id)
	if body != nil {
		request.Header.Set("Content-Type", contentType)
		request.Header.Set("Origin", request.URL.Scheme+"://"+request.URL.Host)
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return errors.New("Сервис Sintet ID временно недоступен.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem struct {
			Error string `json:"error"`
		}
		if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&problem) == nil && problem.Error != "" {
			return errors.New(problem.Error)
		}
		return errors.New("Не удалось открыть гардероб Sintet ID.")
	}
	return json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out)
}

func (c *Client) wardrobeAssetURL(path string) string {
	if validWardrobeTexture(path) {
		return c.base + path
	}
	return ""
}

var (
	wardrobeSkinURL = regexp.MustCompile(`^/api/textures/skin/([a-f0-9-]{36})/[a-f0-9]{64}\.png$`)
	wardrobeCapeURL = regexp.MustCompile(`^/api/account/capes\?texture=[a-f0-9]{64}$`)
)

func validWardrobeTexture(path string) bool {
	return path == "/skins/default.png" || wardrobeSkinURL.MatchString(path) || wardrobeCapeURL.MatchString(path)
}

func (c *Client) WardrobeTexture(ctx context.Context, id, address string) (string, error) {
	path := strings.TrimPrefix(address, c.base)
	match := wardrobeSkinURL.FindStringSubmatch(path)
	if path == address || !validWardrobeTexture(path) || len(match) != 0 && match[1] != id {
		return "", errors.New("Недопустимый адрес текстуры.")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	response, err := c.HTTP.Do(request)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("Не удалось загрузить текстуру.")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, MaxSkinPNG+1))
	if err != nil || !validWardrobePNG(data, wardrobeCapeURL.MatchString(path)) {
		return "", errors.New("Некорректная текстура.")
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(data), nil
}

func validWardrobePNG(data []byte, cape bool) bool {
	if len(data) > MaxSkinPNG {
		return false
	}
	config, err := png.DecodeConfig(bytes.NewReader(data))
	return err == nil && config.Width == 64 && (config.Height == 32 || !cape && config.Height == 64)
}

func (c *Client) skinWardrobe(ctx context.Context, id, method, contentType string, body io.Reader) (SkinWardrobe, error) {
	var result SkinWardrobe
	if err := c.wardrobeRequest(ctx, id, method, skinRoute, contentType, body, &result); err != nil {
		return result, err
	}
	result.Skin.URL = c.wardrobeAssetURL(result.Skin.URL)
	for i := range result.Wardrobe {
		result.Wardrobe[i].URL = c.wardrobeAssetURL(result.Wardrobe[i].URL)
	}
	return result, nil
}

func (c *Client) SkinWardrobe(ctx context.Context, id string) (SkinWardrobe, error) {
	return c.skinWardrobe(ctx, id, http.MethodGet, "", nil)
}

func (c *Client) ApplyWardrobeSkin(ctx context.Context, id, key string) (SkinWardrobe, error) {
	action := "wardrobe"
	if key == "" {
		action = "reset"
	}
	body, err := json.Marshal(map[string]string{"action": action, "key": key})
	if err != nil {
		return SkinWardrobe{}, err
	}
	return c.skinWardrobe(ctx, id, http.MethodPatch, "application/json", bytes.NewReader(body))
}

func (c *Client) UploadWardrobeSkin(ctx context.Context, id, model string, data []byte) (SkinWardrobe, error) {
	if !validWardrobePNG(data, false) || (model != "default" && model != "slim") {
		return SkinWardrobe{}, errors.New("Выберите PNG 64 × 64 или 64 × 32 до 256 КБ.")
	}
	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	field, err := form.CreateFormFile("skin", "skin.png")
	if err != nil {
		return SkinWardrobe{}, err
	}
	if _, err := field.Write(data); err != nil {
		return SkinWardrobe{}, err
	}
	if err := form.WriteField("model", model); err != nil {
		return SkinWardrobe{}, err
	}
	if err := form.Close(); err != nil {
		return SkinWardrobe{}, err
	}
	return c.skinWardrobe(ctx, id, http.MethodPost, form.FormDataContentType(), &body)
}

func (c *Client) CapeWardrobe(ctx context.Context, id string, equipped *string) (CapeWardrobe, error) {
	method := http.MethodGet
	var body io.Reader
	if equipped != nil {
		method = http.MethodPut
		data, err := json.Marshal(map[string]string{"equipped": *equipped})
		if err != nil {
			return CapeWardrobe{}, err
		}
		body = bytes.NewReader(data)
	}
	var result CapeWardrobe
	if err := c.wardrobeRequest(ctx, id, method, capeRoute, "application/json", body, &result); err != nil {
		return result, err
	}
	for i := range result.Capes {
		result.Capes[i].URL = c.wardrobeAssetURL(result.Capes[i].URL)
	}
	for i := range result.Catalog {
		result.Catalog[i].URL = c.wardrobeAssetURL(result.Catalog[i].URL)
	}
	return result, nil
}
