package workshop

import (
	"context"
	"encoding/json"
	"errors"
	"html"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/abadojack/whatlanggo"
)

var translationLanguage = regexp.MustCompile(`^[a-z]{2}(-[A-Z]{2})?$`)
var errTranslationQuota = errors.New("Лимит сервиса перевода исчерпан. Показан оригинал.")

func (c *Client) translateMyMemory(ctx context.Context, text, source, target string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || target == "" {
		return text, nil
	}
	if len(text) > 4096 || !translationLanguage.MatchString(target) {
		return "", errors.New("не удалось подготовить описание к переводу")
	}
	c.translationMu.Lock()
	blocked := time.Now().Before(c.translationBlockedUntil)
	c.translationMu.Unlock()
	if blocked {
		return "", errTranslationQuota
	}
	client := *c.HTTP
	client.Timeout = 8 * time.Second
	translated := make([]string, 0, 1+len(text)/500)
	for remaining := text; remaining != ""; {
		chunk, rest := splitTranslationText(remaining)
		remaining = rest
		query := url.Values{"q": {chunk}, "langpair": {source + "|" + target}}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.mymemory.translated.net/get?"+query.Encode(), nil)
		if err != nil {
			return "", err
		}
		resp, err := client.Do(req)
		if err != nil {
			return "", errors.New("Сервис перевода недоступен. Показан оригинал.")
		}
		var result struct {
			Status json.Number `json:"responseStatus"`
			Quota  bool        `json:"quotaFinished"`
			Data   struct {
				Text string `json:"translatedText"`
			} `json:"responseData"`
		}
		err = json.NewDecoder(io.LimitReader(resp.Body, 64<<10)).Decode(&result)
		resp.Body.Close()
		if resp.StatusCode == 429 || result.Status == "429" || result.Quota {
			c.translationMu.Lock()
			c.translationBlockedUntil = time.Now().Add(30 * time.Minute)
			c.translationMu.Unlock()
			return "", errTranslationQuota
		}
		if err != nil || resp.StatusCode != 200 || result.Status != "200" || strings.TrimSpace(result.Data.Text) == "" {
			return "", errors.New("Не удалось перевести описание. Показан оригинал.")
		}
		translated = append(translated, html.UnescapeString(result.Data.Text))
	}
	return strings.Join(translated, " "), nil
}

func splitTranslationText(text string) (string, string) {
	const limit = 500
	if len(text) <= limit {
		return text, ""
	}
	cut := limit
	for !utf8.RuneStart(text[cut]) {
		cut--
	}
	if space := strings.LastIndexFunc(text[:cut], unicode.IsSpace); space > cut/2 {
		cut = space
	}
	return strings.TrimSpace(text[:cut]), strings.TrimSpace(text[cut:])
}

func (c *Client) TranslateDescription(ctx context.Context, text, target string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || target == "" {
		return text, nil
	}
	if len(text) > 4096 || !translationLanguage.MatchString(target) {
		return "", errors.New("не удалось подготовить текст к переводу")
	}
	source := whatlanggo.Detect(text).Lang.Iso6391()
	if source == "" {
		source = "en"
	}
	if source == target || (source == "zh" && target == "zh-CN") {
		return text, nil
	}

	primary, cancel := context.WithTimeout(ctx, 4*time.Second)
	result, err := c.translateMyMemory(primary, text, source, target)
	cancel()
	if err == nil && result != text {
		return result, nil
	}

	c.translationMu.Lock()
	lingvaAvailable := time.Now().After(c.lingvaBlockedUntil)
	c.translationMu.Unlock()
	if lingvaAvailable {
		for _, host := range []string{"https://translate.plausibility.cloud", "https://lingva.thedaviddelta.com"} {
			probe, cancel := context.WithTimeout(ctx, 1800*time.Millisecond)
			result, err := c.translateLingva(probe, host, text, target)
			cancel()
			if err == nil {
				return result, nil
			}
		}
		if ctx.Err() == nil {
			c.translationMu.Lock()
			c.lingvaBlockedUntil = time.Now().Add(2 * time.Minute)
			c.translationMu.Unlock()
		}
	}
	return c.translateGoogle(ctx, text, target)
}

func (c *Client) translateLingva(ctx context.Context, host, text, target string) (string, error) {
	address := host + "/api/v1/auto/" + url.PathEscape(target) + "/" + url.PathEscape(text)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Accept", "application/json")
	response, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	var result struct {
		Translation string `json:"translation"`
	}
	if response.StatusCode != http.StatusOK ||
		json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&result) != nil ||
		strings.TrimSpace(result.Translation) == "" {
		return "", errors.New("Lingva Translate недоступен")
	}
	return result.Translation, nil
}

func (c *Client) translateGoogle(ctx context.Context, text, target string) (string, error) {
	query := url.Values{"client": {"gtx"}, "sl": {"auto"}, "tl": {target}, "dt": {"t"}, "q": {text}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://translate.googleapis.com/translate_a/single?"+query.Encode(), nil)
	if err != nil {
		return "", err
	}
	response, err := c.HTTP.Do(req)
	if err != nil {
		return "", err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return "", errors.New("сервисы перевода недоступны")
	}
	var body []json.RawMessage
	if err := json.NewDecoder(io.LimitReader(response.Body, 128<<10)).Decode(&body); err != nil || len(body) == 0 {
		return "", errors.New("не удалось прочитать перевод шейдера")
	}
	var segments [][]json.RawMessage
	if err := json.Unmarshal(body[0], &segments); err != nil {
		return "", err
	}
	var translated strings.Builder
	for _, segment := range segments {
		if len(segment) == 0 {
			continue
		}
		var part string
		if err := json.Unmarshal(segment[0], &part); err != nil {
			return "", err
		}
		translated.WriteString(part)
	}
	if strings.TrimSpace(translated.String()) == "" {
		return "", errors.New("сервис вернул пустой перевод")
	}
	return translated.String(), nil
}
