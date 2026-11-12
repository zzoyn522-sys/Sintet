package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
	"sintet/internal/identity"
)

func (a *App) CatalogThemes(offset int, query string) (identity.PublicThemePage, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 12*time.Second)
	defer cancel()
	return identity.ReadPublicThemes(ctx, offset, query)
}

func (a *App) InstallCatalogTheme(id string) (*identity.ThemeCatalogStats, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 20*time.Second)
	defer cancel()
	data, err := identity.DownloadPublicTheme(ctx, id)
	if err != nil {
		return nil, err
	}
	if err = a.importTheme(data); err != nil {
		if errors.Is(err, errThemeAlreadyImported) {
			return nil, nil
		}
		return nil, err
	}
	statsCtx, stop := context.WithTimeout(a.ctx, 4*time.Second)
	defer stop()
	stats, _ := identity.ReportThemeActivity(statsCtx, id, "downloads")
	return stats, nil
}

func (a *App) ViewCatalogTheme(id string) (*identity.ThemeCatalogStats, error) {
	ctx, cancel := context.WithTimeout(a.ctx, 6*time.Second)
	defer cancel()
	return identity.ReportThemeActivity(ctx, id, "views")
}

func serveCatalogThemePreview(w http.ResponseWriter, r *http.Request, path string) {
	id, screen, ok := strings.Cut(path, "/")
	if !ok || r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	frame, err := identity.DownloadThemePreview(ctx, id, screen)
	if err != nil || len(frame) < 12 || string(frame[:4]) != "RIFF" || string(frame[8:12]) != "WEBP" {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "public, max-age=60, must-revalidate")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Write(frame)
}

func (a *App) watchCatalogThemes(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		a.syncCatalogThemes(ctx)
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (a *App) syncCatalogThemes(ctx context.Context) {
	a.authMu.Lock()
	slot := a.profiles.find(a.profiles.active)
	a.authMu.Unlock()
	if slot == nil || slot.sintet == nil {
		return
	}
	client := slot.sintet
	account := client.SavedAccount()
	if account == nil || !avatarAccountID.MatchString(account.ID) {
		return
	}
	request, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	acquisitions, err := client.ThemeCatalogAcquisitions(request, account.ID)
	if err != nil || len(acquisitions) > maxAppearanceSlots {
		return
	}
	path := filepath.Join(a.profiles.root, "CatalogThemes", account.ID+".json")
	seen := map[string]string{}
	if raw, readErr := readLimitedFile(path, 16<<10); readErr == nil {
		_ = json.Unmarshal(raw, &seen)
	}
	next := make(map[string]string, len(acquisitions))
	for _, item := range acquisitions {
		if themePackageID.MatchString(item.ID) && avatarAccountID.MatchString(item.Revision) && seen[item.ID] == item.Revision {
			next[item.ID] = item.Revision
		}
	}
	changed := false
	for _, item := range acquisitions {
		if !themePackageID.MatchString(item.ID) || !avatarAccountID.MatchString(item.Revision) || next[item.ID] == item.Revision {
			continue
		}
		if request.Err() != nil {
			break
		}
		data, err := client.ThemeLibrary(request, account.ID, http.MethodGet, item.ID, nil, false)
		if err != nil || fmt.Sprintf("%x", sha256.Sum256(data)) != item.ID {
			continue
		}
		a.authMu.Lock()
		if a.profiles.find(a.profiles.active) != slot {
			a.authMu.Unlock()
			break
		}
		err = a.importTheme(data)
		a.authMu.Unlock()
		if err == nil || errors.Is(err, errThemeAlreadyImported) {
			next[item.ID] = item.Revision
			changed = changed || err == nil
		}
	}
	if changed && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "sintet:catalog-themes")
	}
	if !maps.Equal(seen, next) {
		if data, err := json.Marshal(next); err == nil {
			_ = accountfiles.Write(path, data)
		}
	}
}
