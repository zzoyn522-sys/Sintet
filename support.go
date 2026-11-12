package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"sintet/internal/accountfiles"
)

type supportNotice struct {
	Kind     string `json:"kind"`
	Version  string `json:"version"`
	Deferred bool   `json:"deferred"`
}

type supportManager struct {
	mu          sync.Mutex
	path        string
	seenVersion string
	firstLaunch bool
}

func newSupportManager(root string, firstLaunch bool) *supportManager {
	welcome := filepath.Join(root, "support-welcome.pending")
	if firstLaunch {
		_ = accountfiles.Write(welcome, []byte("pending"))
	}
	_, pending := os.Stat(welcome)
	manager := &supportManager{path: filepath.Join(root, "support-notice.json"), firstLaunch: firstLaunch || pending == nil}
	var saved struct {
		Version string `json:"version"`
	}
	if data, err := os.ReadFile(manager.path); err == nil && json.Unmarshal(data, &saved) == nil && versionIsNewer(saved.Version, "0.0.0") {
		manager.seenVersion = saved.Version
	}
	return manager
}

func (m *supportManager) notice(version string) supportNotice {
	m.mu.Lock()
	defer m.mu.Unlock()
	notice := supportNotice{Version: version}
	if m.seenVersion == version || m.seenVersion != "" && !versionIsNewer(version, m.seenVersion) {
		return notice
	}
	notice.Kind = "update"
	if m.seenVersion == "" && m.firstLaunch {
		notice.Kind = "welcome"
	}
	return notice
}

func (m *supportManager) acknowledge(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.seenVersion == version || m.seenVersion != "" && !versionIsNewer(version, m.seenVersion) {
		return nil
	}
	data, err := json.Marshal(struct {
		Version string `json:"version"`
	}{Version: version})
	if err != nil {
		return err
	}
	if err := accountfiles.Write(m.path, data); err != nil {
		return fmt.Errorf("не удалось запомнить уведомление поддержки: %w", err)
	}
	m.seenVersion = version
	_ = os.Remove(filepath.Join(filepath.Dir(m.path), "support-welcome.pending"))
	return nil
}

func (a *App) SupportNotice() supportNotice {
	if a.support == nil {
		return supportNotice{}
	}
	notice := a.support.notice(launcherVersion)
	notice.Deferred = a.windowHidden.Load()
	return notice
}

func (a *App) AcknowledgeSupportNotice(version string) error {
	if a.support == nil {
		return fmt.Errorf("Sintet ещё запускается")
	}
	if version != launcherVersion {
		return fmt.Errorf("версия Sintet изменилась")
	}
	return a.support.acknowledge(version)
}
