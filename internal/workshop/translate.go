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

func (c *Client) TranslateDescription(ctx context.Context, text, target string) (string, error) {
	text = strings.TrimSpace(text)
	if text == "" || target == "" {
		return text, nil
	}
	if len(text) > 4096 || !translationLanguage.MatchString(target) {
		return "", errors.New("не удалось подготовить описание к переводу")
	}
	info := whatlanggo.Detect(text)
	source := info.Lang.Iso6391()
	if source == "" {
		source = "en"
	}
	if source == target || (source == "zh" && target == "zh-CN") {
		return text, nil
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
