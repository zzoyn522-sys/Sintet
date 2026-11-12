package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"sintet/internal/accountfiles"
	"sintet/internal/workshop"
	"sort"
	"strconv"
	"strings"
	"time"
)

type workshopEntry struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"`
	Name        string   `json:"name"`
	Version     string   `json:"version"`
	Game        string   `json:"game"`
	Loader      string   `json:"loader"`
	Provider    string   `json:"provider"`
	Enabled     bool     `json:"enabled"`
	Icon        string   `json:"icon"`
	Description string   `json:"description"`
	Palette     []string `json:"palette,omitempty"`
}
type workshopSnapshot struct {
	Device  string          `json:"device"`
	Entries []workshopEntry `json:"entries"`
}

func workshopEntryID(kind, target, name string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + target + "\x00" + name))
	return hex.EncodeToString(sum[:])
}
func workshopDevice(root string) (string, error) {
	path := filepath.Join(root, "workshop-device-id")
	data, err := os.ReadFile(path)
	if err == nil {
		value := strings.TrimSpace(string(data))
		if raw, e := hex.DecodeString(value); e == nil && len(raw) == 16 {
			return value, nil
		}
		return "", errors.New("повреждён ID устройства мастерской")
	}
	if !os.IsNotExist(err) {
		return "", err
	}
	raw := make([]byte, 16)
	if _, err = rand.Read(raw); err != nil {
		return "", err
	}
	value := hex.EncodeToString(raw)
	return value, accountfiles.Write(path, []byte(value))
}
func workshopThumbnail(data []byte) string {
	config, err := png.DecodeConfig(bytes.NewReader(data))
	if err != nil || config.Width < 1 || config.Height < 1 || config.Width > 2048 || config.Height > 2048 {
		return ""
	}
	source, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		return ""
	}
	thumbnail := image.NewNRGBA(image.Rect(0, 0, 64, 64))
	bounds := source.Bounds()
	for y := 0; y < 64; y++ {
		for x := 0; x < 64; x++ {
			thumbnail.Set(x, y, source.At(bounds.Min.X+x*bounds.Dx()/64, bounds.Min.Y+y*bounds.Dy()/64))
		}
	}
	var output bytes.Buffer
	if png.Encode(&output, thumbnail) != nil || output.Len() > 16000 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(output.Bytes())
}
func (a *App) workshopSnapshot() (workshopSnapshot, error) {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if a.service.State().Busy || a.workshop != nil && a.workshop.State().Busy {
		return workshopSnapshot{}, errors.New("идёт установка; синхронизация отложена")
	}
	device, err := workshopDevice(a.profiles.root)
	if err != nil {
		return workshopSnapshot{}, err
	}
	snapshot := workshopSnapshot{Device: device, Entries: []workshopEntry{}}
	builds, err := a.WorkshopBuilds()
	if err != nil {
		return snapshot, err
	}
	for _, build := range builds {
		name := strings.TrimSpace(build.Name)
		if name == "" {
			name = "Minecraft " + build.Game
		}
		snapshot.Entries = append(snapshot.Entries, workshopEntry{
			ID:      workshopEntryID("modpacks", build.ID, ""),
			Kind:    "modpacks",
			Name:    name,
			Version: build.LoaderVersion,
			Game:    build.Game,
			Loader:  build.Loader,
			Enabled: build.Installed,
		})
	}
	targets, err := a.WorkshopTargets()
	if err != nil {
		return snapshot, err
	}
	for _, target := range targets {
		for _, kind := range []string{"mods", "resources", "shaders"} {
			items, e := workshop.Scan(target.Directory, kind)
			if e != nil {
				return snapshot, e
			}
			for _, item := range items {
				icon := ""
				if strings.HasPrefix(item.Icon, "https://") {
					icon = item.Icon
				}
				if data, e := workshop.Icon(target.Directory, kind, item.Filename); e == nil {
					if thumb := workshopThumbnail(data); thumb != "" {
						icon = thumb
					}
				}
				snapshot.Entries = append(snapshot.Entries, workshopEntry{ID: workshopEntryID(kind, target.ID, item.Filename), Kind: kind, Name: item.Name, Version: item.Version, Game: target.Game, Loader: target.Loader, Provider: item.Provider, Enabled: item.Enabled, Icon: icon, Description: item.Description})
			}
		}
	}
	library := a.GetAppearanceLibrary()
	for i, slot := range library.Slots {
		if slot.Name != "" {
			entry := workshopEntry{ID: workshopEntryID("themes", device, strconv.Itoa(i)), Kind: "themes", Name: slot.Name, Enabled: library.ActiveSlot == i+1, Description: "Сохранённое оформление", Palette: []string{slot.Settings.GradientStart, slot.Settings.GradientMiddle, slot.Settings.GradientEnd}}
			if cachedAvatarName.MatchString(slot.Icon) {
				if data, err := readLimitedFile(filepath.Join(a.themeIconsDir(), slot.Icon), iconFileLimit); err == nil {
					entry.Icon = workshopThumbnail(data)
				}
			}
			if slot.Package != "" {
				entry.Version = ".sldcfg"
			}
			snapshot.Entries = append(snapshot.Entries, entry)
		}
	}
	if len(snapshot.Entries) > 2000 {
		return snapshot, errors.New("слишком много элементов мастерской")
	}
	sort.Slice(snapshot.Entries, func(i, j int) bool { return snapshot.Entries[i].ID < snapshot.Entries[j].ID })
	return snapshot, nil
}
func (a *App) syncWorkshop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	lastAccount, lastHash := "", ""
	for {
		a.authMu.Lock()
		slot := a.profiles.find(a.profiles.active)
		a.authMu.Unlock()
		if slot != nil && slot.sintet != nil {
			if account := slot.sintet.SavedAccount(); account != nil {
				if snapshot, err := a.workshopSnapshot(); err == nil {
					data, err := json.Marshal(snapshot)
					sum := sha256.Sum256(data)
					hash := hex.EncodeToString(sum[:])
					if err == nil && len(data) <= 4<<20 && (account.ID != lastAccount || hash != lastHash) {
						requestCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
						if slot.sintet.PublishWorkshop(requestCtx, account.ID, snapshot) == nil {
							lastAccount, lastHash = account.ID, hash
						}
						cancel()
					}
				}
			}
		} else {
			lastAccount, lastHash = "", ""
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
