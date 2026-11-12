package main

import (
	"context"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

func (m *socialManager) notifyNewInvites(previous, current []socialInvite) {
	a := m.app
	if len(current) == 0 || a == nil || a.ctx == nil || !a.tray.available() || a.State().Running {
		return
	}
	if !a.windowHidden.Load() && !runtime.WindowIsMinimised(a.ctx) {
		return
	}
	known := make(map[string]bool, len(previous))
	for _, invite := range previous {
		known[invite.ID] = true
	}
	for _, invite := range current {
		if invite.Account == nil || known[invite.ID] {
			continue
		}
		var details []string
		if invite.Game != "" {
			details = append(details, "Minecraft "+invite.Game)
		}
		if invite.Build != "" && (len(details) == 0 || !strings.EqualFold(invite.Build, details[0])) {
			details = append(details, invite.Build)
		}
		a.tray.notify(invite.Account.Username+" приглашает в лобби", strings.Join(append(details, "Открыть приглашение"), " · "))
		return
	}
}

func (a *App) ShowLauncher() {
	if a.ctx == nil {
		return
	}
	a.gameHidWindow.Store(false)
	runtime.WindowShow(a.ctx)
	runtime.WindowUnminimise(a.ctx)
	a.windowHidden.Store(false)
}
func (a *App) hideToTray() bool {
	if a.ctx == nil || a.tray == nil || !a.tray.available() {
		return false
	}
	a.windowHidden.Store(true)
	runtime.WindowHide(a.ctx)
	return true
}

func (a *App) minimizeToTrayEnabled() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.settings.MinimizeToTray
}

func (a *App) MinimizeLauncher() {
	if a.ctx == nil {
		return
	}
	a.gameHidWindow.Store(false)
	if !a.minimizeToTrayEnabled() || !a.hideToTray() {
		runtime.WindowMinimise(a.ctx)
	}
}

func (a *App) watchWindowMinimize(ctx context.Context) {
	ticker := time.NewTicker(300 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if a.tray.available() && !a.windowHidden.Load() && a.minimizeToTrayEnabled() && runtime.WindowIsMinimised(a.ctx) {
				a.gameHidWindow.Store(false)
				a.hideToTray()
			}
		}
	}
}

func (a *App) runTrayAction(action string) {
	switch action {
	case "quit":
		a.ExitLauncher()
	case "folder":
		a.OpenGameFolder()
	default:
		a.ShowLauncher()
		if action != "open" && a.ctx != nil {
			runtime.EventsEmit(a.ctx, "sintet:tray-action", action)
		}
	}
}
func (a *App) ExitLauncher() {
	a.forceQuit.Store(true)
	if a.ctx != nil {
		runtime.Quit(a.ctx)
	}
}
func (a *App) beforeClose(ctx context.Context) bool {
	if a.forceQuit.Load() {
		return false
	}
	a.mu.RLock()
	action := a.settings.CloseAction
	a.mu.RUnlock()
	if action != "tray" {
		return false
	}
	a.gameHidWindow.Store(false)
	return a.hideToTray()
}
func (a *App) gameWindowBehavior(running bool, action string) {
	if a.ctx == nil {
		return
	}
	if running {
		switch action {
		case "quit":
			go a.ExitLauncher()
		case "tray", "":
			if !a.windowHidden.Load() && a.hideToTray() {
				a.gameHidWindow.Store(true)
			}
		}
	} else if a.gameHidWindow.Swap(false) && a.windowHidden.Load() {
		a.ShowLauncher()
	}
}
