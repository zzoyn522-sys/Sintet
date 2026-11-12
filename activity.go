package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

type activityTotals = identity.ActivityTotals

type activityStore struct {
	Device   string                    `json:"device"`
	Accounts map[string]activityTotals `json:"accounts"`
}

type activityTracker struct {
	mu     sync.Mutex
	path   string
	store  activityStore
	last   time.Time
	active string
	game   string
	dirty  bool
	other  map[string]activityTotals
}

func openActivity(root string) (*activityTracker, error) {
	tracker := &activityTracker{
		path:  filepath.Join(root, "activity.json"),
		last:  time.Now(),
		other: make(map[string]activityTotals),
	}
	data, err := os.ReadFile(tracker.path)
	if errors.Is(err, os.ErrNotExist) {
		var device [16]byte
		if _, err = rand.Read(device[:]); err != nil {
			return nil, err
		}
		tracker.store = activityStore{Device: hex.EncodeToString(device[:]), Accounts: make(map[string]activityTotals)}
		tracker.dirty = true
		return tracker, tracker.flush()
	}
	if err != nil {
		return nil, err
	}
	if err = json.Unmarshal(data, &tracker.store); err != nil {
		return nil, err
	}
	if device, err := hex.DecodeString(tracker.store.Device); err != nil || len(device) != 16 || tracker.store.Accounts == nil {
		return nil, errors.New("не удалось прочитать статистику Sintet")
	}
	return tracker, nil
}

// Advance both clocks before changing either owner. The two timers can overlap.
func (t *activityTracker) advance(now time.Time) {
	elapsed := now.Sub(t.last).Milliseconds()
	if elapsed <= 0 {
		return
	}
	t.last = t.last.Add(time.Duration(elapsed) * time.Millisecond)
	if t.active != "" {
		total := t.store.Accounts[t.active]
		total.LauncherMS += elapsed
		t.store.Accounts[t.active] = total
		t.dirty = true
	}
	if t.game != "" {
		total := t.store.Accounts[t.game]
		total.MinecraftMS += elapsed
		t.store.Accounts[t.game] = total
		t.dirty = true
	}
}

func (t *activityTracker) selectAccount(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	t.active = id
}

func (t *activityTracker) gameChanged(id string, running bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	if running {
		t.game = id
	} else {
		t.game = ""
	}
}

func (t *activityTracker) flush() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.advance(time.Now())
	if !t.dirty {
		return nil
	}
	data, err := json.Marshal(t.store)
	if err == nil {
		err = accountfiles.Write(t.path, data)
	}
	if err == nil {
		t.dirty = false
	}
	return err
}

func (a *App) activityAccounts() (string, map[string]*identity.Client) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	active := ""
	clients := make(map[string]*identity.Client)
	for _, slot := range a.profiles.slots {
		if slot.sintet == nil {
			continue
		}
		if account := slot.sintet.SavedAccount(); account != nil {
			clients[account.ID] = slot.sintet
			if slot.Key == a.profiles.active {
				active = account.ID
			}
		}
	}
	return active, clients
}

func (a *App) trackActivity(ctx context.Context) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	checkpoint := time.NewTicker(10 * time.Second)
	defer checkpoint.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			active, _ := a.activityAccounts()
			a.activity.selectAccount(active)
		case <-checkpoint.C:
			_ = a.activity.flush()
		}
	}
}

func (a *App) sendActivity(ctx context.Context, id string, client *identity.Client, totals activityTotals) bool {
	if a.avatarDB == nil {
		return false
	}
	other, err := client.PublishActivity(ctx, id, a.avatarDB.URL, a.avatarDB.Key, a.activity.store.Device, totals)
	if err != nil {
		return false
	}
	a.activity.mu.Lock()
	a.activity.other[id] = other
	a.activity.mu.Unlock()
	return true
}

func (a *App) syncActivity(ctx context.Context) {
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	sent := make(map[string]activityTotals)
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			if a.activity.flush() == nil {
				active, clients := a.activityAccounts()
				for id, client := range clients {
					a.activity.mu.Lock()
					totals := a.activity.store.Accounts[id]
					a.activity.mu.Unlock()
					if (id == active || totals != sent[id]) && a.sendActivity(ctx, id, client, totals) {
						sent[id] = totals
					}
				}
			}
			timer.Reset(30 * time.Second)
		}
	}
}

func (a *App) finishActivity() {
	if a.activity == nil {
		return
	}
	a.activity.selectAccount("")
	if a.activity.flush() != nil {
		return
	}
	active, clients := a.activityAccounts()
	if client := clients[active]; client != nil {
		a.activity.mu.Lock()
		totals := a.activity.store.Accounts[active]
		a.activity.mu.Unlock()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		a.sendActivity(ctx, active, client, totals)
	}
}

func (a *App) ProfileActivity(accountID string) *activityTotals {
	active, _ := a.activityAccounts()
	if a.activity == nil || active == "" || active != accountID {
		return nil
	}
	a.activity.mu.Lock()
	defer a.activity.mu.Unlock()
	a.activity.advance(time.Now())
	totals, other := a.activity.store.Accounts[active], a.activity.other[active]
	totals.LauncherMS += other.LauncherMS
	totals.MinecraftMS += other.MinecraftMS
	return &totals
}

func (a *App) OpenAccountSettings() error {
	if a.ctx == nil {
		return errors.New("Sintet ещё запускается")
	}
	address := "https://sintet.xyz/settings"
	if a.profiles != nil && a.profiles.base == "http://127.0.0.1:3033" {
		runtime.BrowserOpenURL(a.ctx, a.profiles.base+"/settings")
		return nil
	}
	ctx, cancel := context.WithTimeout(a.ctx, 3*time.Second)
	defer cancel()
	request, err := http.NewRequestWithContext(ctx, http.MethodHead, address, nil)
	if err == nil {
		if response, err := http.DefaultClient.Do(request); err == nil {
			response.Body.Close()
			if response.StatusCode == http.StatusNotFound {
				address = "https://sintet.xyz/account"
			}
		}
	}
	runtime.BrowserOpenURL(a.ctx, address)
	return nil
}
