package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
)

const mojangNewsURL = "https://launchercontent.mojang.com/v2/news.json"
const newsTTL = 2 * time.Hour
const maxNewsBytes = 2 << 20

type newsEntry struct {
	Title string `json:"title"`
	Date  string `json:"date"`
	Image string `json:"image"`
	Link  string `json:"link"`
}
type newsSnapshot struct {
	FetchedAt time.Time   `json:"fetchedAt"`
	Entries   []newsEntry `json:"entries"`
}
type newsFeed struct {
	source      *url.URL
	articleHost string
	cachePath   string
	client      *http.Client
	mu          sync.Mutex
	snapshot    newsSnapshot
	retryAfter  time.Time
}

func newNewsFeed(root, endpoint, articleHost string) *newsFeed {
	source, _ := url.Parse(endpoint)
	return &newsFeed{
		source: source, articleHost: articleHost, cachePath: filepath.Join(root, "minecraft-news.json"),
		client: &http.Client{Timeout: 8 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
	}
}

func isHTTPSHost(link *url.URL, host string, subdomains bool) bool {
	name := strings.ToLower(link.Hostname())
	return link.Scheme == "https" && link.User == nil && (link.Port() == "" || link.Port() == "443") &&
		(name == host || (subdomains && strings.HasSuffix(name, "."+host)))
}
func newsArticleURL(raw, host string) bool {
	link, err := url.Parse(raw)
	return err == nil && isHTTPSHost(link, host, true)
}

func (f *newsFeed) decode(reader io.Reader) ([]newsEntry, error) {
	data, err := io.ReadAll(io.LimitReader(reader, maxNewsBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxNewsBytes {
		return nil, errors.New("слишком большой ответ ленты")
	}
	var feed struct {
		Entries []struct {
			Title         string `json:"title"`
			Date          string `json:"date"`
			ReadMoreLink  string `json:"readMoreLink"`
			PlayPageImage struct {
				URL string `json:"url"`
			} `json:"playPageImage"`
		} `json:"entries"`
	}
	if err = json.Unmarshal(data, &feed); err != nil {
		return nil, err
	}
	entries := make([]newsEntry, 0, len(feed.Entries))
	for _, item := range feed.Entries {
		title := strings.TrimSpace(item.Title)
		if title == "" || !newsArticleURL(item.ReadMoreLink, f.articleHost) {
			continue
		}
		entry := newsEntry{Title: title, Date: item.Date, Link: item.ReadMoreLink}
		if item.PlayPageImage.URL != "" {
			if img, err := f.source.Parse(item.PlayPageImage.URL); err == nil && isHTTPSHost(img, f.source.Hostname(), false) {
				entry.Image = img.String()
			}
		}
		entries = append(entries, entry)
	}
	if len(entries) == 0 {
		return nil, errors.New("лента не содержит новостей")
	}
	return entries, nil
}

func (f *newsFeed) entries() ([]newsEntry, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.snapshot.FetchedAt.IsZero() {
		if data, err := os.ReadFile(f.cachePath); err == nil && len(data) <= maxNewsBytes {
			var cached newsSnapshot
			if json.Unmarshal(data, &cached) == nil && len(cached.Entries) > 0 {
				f.snapshot = cached
			}
		}
	}
	now := time.Now()
	age := now.Sub(f.snapshot.FetchedAt)
	if len(f.snapshot.Entries) > 0 && ((age >= 0 && age < newsTTL) || now.Before(f.retryAfter)) {
		return f.snapshot.Entries, nil
	}
	entries, err := f.fetch()
	if err != nil {
		if len(f.snapshot.Entries) > 0 {
			f.retryAfter = now.Add(time.Minute)
			return f.snapshot.Entries, nil
		}
		return nil, fmt.Errorf("не удалось загрузить новости Minecraft: %w", err)
	}
	f.snapshot = newsSnapshot{FetchedAt: time.Now(), Entries: entries}
	f.retryAfter = time.Time{}
	if data, err := json.Marshal(f.snapshot); err == nil {
		_ = accountfiles.Write(f.cachePath, data)
	}
	return entries, nil
}

func (f *newsFeed) fetch() ([]newsEntry, error) {
	response, err := f.client.Get(f.source.String())
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", response.StatusCode)
	}
	return f.decode(response.Body)
}

func (a *App) MinecraftNews() ([]newsEntry, error) { return a.news.entries() }

func (a *App) OpenMinecraftNews(link string) error {
	if !newsArticleURL(link, a.news.articleHost) {
		return errors.New("недопустимая ссылка на новость")
	}
	runtime.BrowserOpenURL(a.ctx, link)
	return nil
}
