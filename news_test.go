package main

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type newsTransport func(*http.Request) (*http.Response, error)

func (f newsTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMinecraftNewsDiskCacheAndOfflineFallback(t *testing.T) {
	root := t.TempDir()
	feed := newNewsFeed(root, mojangNewsURL, "minecraft.net")
	requests := 0
	feed.client.Transport = newsTransport(func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.String() != mojangNewsURL {
			t.Fatalf("unexpected feed URL %s", request.URL)
		}
		data := `{"entries":[{"title":"First","date":"2026-10-30","playPageImage":{"url":"/v2/images/first.jpg"},"readMoreLink":"https://www.minecraft.net/article/first"},{"title":"Unsafe","readMoreLink":"https://minecraft.net.example.com/article"},{"title":"Second","playPageImage":{"url":"https://example.com/second.jpg"},"readMoreLink":"https://minecraft.net/article/second"}]}`
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(data)), Header: make(http.Header)}, nil
	})
	entries, err := feed.entries()
	if err != nil || len(entries) != 2 || entries[0].Title != "First" || entries[0].Image != "https://launchercontent.mojang.com/v2/images/first.jpg" || entries[1].Image != "" {
		t.Fatalf("incorrect normalized feed: %v, %v", entries, err)
	}
	reopened := newNewsFeed(root, mojangNewsURL, "minecraft.net")
	reopened.client.Transport = newsTransport(func(*http.Request) (*http.Response, error) { requests++; return nil, errors.New("offline") })
	if cached, err := reopened.entries(); err != nil || len(cached) != 2 || requests != 1 {
		t.Fatalf("fresh cache requested network: %v, requests %d", err, requests)
	}
	reopened.snapshot.FetchedAt = time.Now().Add(-3 * time.Hour)
	if stale, err := reopened.entries(); err != nil || len(stale) != 2 || requests != 2 {
		t.Fatalf("stale fallback failed: %v, requests %d", err, requests)
	}
	if _, err := reopened.entries(); err != nil || requests != 2 {
		t.Fatalf("retry backoff failed: %v, requests %d", err, requests)
	}
}
