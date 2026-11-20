package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"sync"
	"time"
	"unicode/utf8"
)

const overlayBadgeSize = 24
const maxBadgeCatalogBytes = 9 << 20

var overlayBadgeID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

type overlayBadgeIcon struct {
	Label  string `json:"label"`
	Pixels string `json:"pixels"`
}

type overlayBadgeResponse struct {
	Revision string          `json:"revision"`
	Icons    json.RawMessage `json:"icons,omitempty"`
}

type overlayBadgeCatalog struct {
	mu    sync.Mutex
	value overlayBadgeResponse
	next  time.Time
	busy  bool
}

func decodeOverlayBadges(body io.Reader) (overlayBadgeResponse, error) {
	var payload struct {
		Items []struct{ ID, Label, PNG string }
	}
	data, err := io.ReadAll(io.LimitReader(body, maxBadgeCatalogBytes+1))
	if err != nil {
		return overlayBadgeResponse{}, err
	}
	if len(data) > maxBadgeCatalogBytes || json.Unmarshal(data, &payload) != nil || payload.Items == nil || len(payload.Items) > 256 {
		return overlayBadgeResponse{}, errors.New("invalid overlay badge catalog")
	}
	icons := make(map[string]overlayBadgeIcon, len(payload.Items))
	for _, item := range payload.Items {
		if !overlayBadgeID.MatchString(item.ID) || item.Label == "" || utf8.RuneCountInString(item.Label) > 80 || len(item.PNG) > 32000 {
			continue
		}
		png, err := base64.StdEncoding.DecodeString(item.PNG)
		if err != nil {
			continue
		}
		pixels, err := overlayPNG(png, false, overlayBadgeSize)
		if err == nil {
			icons[item.ID] = overlayBadgeIcon{Label: item.Label, Pixels: pixels}
		}
	}
	encoded, err := json.Marshal(icons)
	if err != nil {
		return overlayBadgeResponse{}, err
	}
	revision := sha256.Sum256(encoded)
	return overlayBadgeResponse{Revision: hex.EncodeToString(revision[:]), Icons: encoded}, nil
}

func (c *overlayBadgeCatalog) read(ctx context.Context, revision string) overlayBadgeResponse {
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.busy && time.Now().After(c.next) {
		c.busy = true
		go c.refresh(ctx)
	}
	result := c.value
	if result.Revision == revision {
		result.Icons = nil
	}
	return result
}

func (c *overlayBadgeCatalog) refresh(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, publicIdentityService+"/api/profile-badges", nil)
	var catalog overlayBadgeResponse
	if err == nil {
		resp, err := http.DefaultClient.Do(req)
		if err == nil {
			defer resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				catalog, _ = decodeOverlayBadges(resp.Body)
			}
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.busy = false
	c.next = time.Now().Add(10 * time.Second)
	if catalog.Revision != "" {
		c.value = catalog
		c.next = time.Now().Add(20 * time.Second)
	}
}
