package identity

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"

	"github.com/google/uuid"

	"sintet/internal/sldcfg"
)

const catalogThemePageSize = 18

type PublicTheme struct {
	SavedTheme
	ThemeCatalogStats
	Author          string `json:"author"`
	Description     string `json:"description"`
	AuthorAvatar    string `json:"authorAvatar"`
	LauncherVersion string `json:"launcherVersion"`
	FormatVersion   int    `json:"formatVersion"`
}

type ThemeCatalogStats struct {
	Views     int64 `json:"views"`
	Downloads int64 `json:"downloads"`
}

type PublicThemePage struct {
	Themes     []PublicTheme `json:"themes"`
	More       bool          `json:"more"`
	NextOffset int           `json:"nextOffset"`
}

type CatalogThemeAcquisition struct {
	ID       string `json:"id"`
	Revision string `json:"revision"`
}

func (c *Client) ThemeCatalogAcquisitions(ctx context.Context, accountID string) ([]CatalogThemeAcquisition, error) {
	data, err := c.themeLibrary(ctx, accountID, http.MethodGet, "", nil, false, true)
	if err != nil {
		return nil, err
	}
	var response struct {
		Themes []CatalogThemeAcquisition `json:"themes"`
	}
	if err = json.Unmarshal(data, &response); err != nil {
		return nil, errors.New("Некорректный ответ кабинета.")
	}
	return response.Themes, nil
}

func ReadPublicThemes(ctx context.Context, offset int, query string) (PublicThemePage, error) {
	var page PublicThemePage
	offset = max(0, min(10000, offset))
	data, err := publicThemeRequest(ctx, "?offset="+strconv.Itoa(offset)+"&q="+url.QueryEscape(query), 256<<10)
	if err != nil {
		return page, err
	}
	if json.Unmarshal(data, &page) != nil || len(page.Themes) > catalogThemePageSize {
		return page, errors.New("Некорректный ответ каталога оформлений.")
	}
	for _, theme := range page.Themes {
		if !themeID.MatchString(theme.ID) {
			return page, errors.New("Некорректный ответ каталога оформлений.")
		}
	}
	page.NextOffset = offset + catalogThemePageSize
	page.More = page.More && page.NextOffset <= 10000
	return page, nil
}

func DownloadPublicTheme(ctx context.Context, id string) ([]byte, error) {
	if !themeID.MatchString(id) {
		return nil, errors.New("Оформление не найдено.")
	}
	data, err := publicThemeRequest(ctx, "?package="+id, sldcfg.MaxFile)
	if err != nil {
		return nil, err
	}
	if fmt.Sprintf("%x", sha256.Sum256(data)) != id {
		return nil, errors.New("Файл оформления изменился.")
	}
	return data, nil
}

func DownloadThemePreview(ctx context.Context, id, screen string) ([]byte, error) {
	if !themeID.MatchString(id) || (screen != "play" && screen != "settings" && screen != "profile" && screen != "icon") {
		return nil, errors.New("Снимок оформления не найден.")
	}
	return catalogRequest(ctx, http.MethodGet, "/previews?id="+id+"&screen="+screen, nil, 768<<10)
}

func ReportThemeActivity(ctx context.Context, id, kind string) (*ThemeCatalogStats, error) {
	if !themeID.MatchString(id) || (kind != "views" && kind != "downloads") {
		return nil, errors.New("Некорректный запрос.")
	}
	payload, err := json.Marshal(map[string]string{"id": id, "kind": kind, "event": uuid.NewString()})
	if err != nil {
		return nil, err
	}
	data, err := catalogRequest(ctx, http.MethodPost, "/activity", payload, 4096)
	if err != nil {
		return nil, err
	}
	var stats ThemeCatalogStats
	if err = json.Unmarshal(data, &stats); err != nil {
		return nil, err
	}
	return &stats, nil
}

func publicThemeRequest(ctx context.Context, query string, limit int64) ([]byte, error) {
	return catalogRequest(ctx, http.MethodGet, query, nil, limit)
}

func catalogRequest(ctx context.Context, method, path string, body []byte, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, method, themePortalOrigin()+"/api/catalog/themes"+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	if method == http.MethodPost {
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", themePortalOrigin())
	}
	response, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, errors.New("Каталог оформлений временно недоступен.")
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		var problem struct{ Error string }
		if json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&problem) == nil && problem.Error != "" {
			return nil, errors.New(problem.Error)
		}
		return nil, errors.New("Каталог оформлений временно недоступен.")
	}
	if response.ContentLength > limit {
		return nil, errors.New("Файл оформления слишком велик.")
	}
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	if int64(len(data)) > limit {
		return nil, errors.New("Файл оформления слишком велик.")
	}
	return data, err
}
