package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"sintet/internal/launcher"
	"sintet/internal/workshop"
)

func (a *App) LocalShaderOptions(targetID, filename, language string) (workshop.ShaderOptions, error) {
	a.mu.RLock()
	target, err := a.workshopTarget(targetID)
	a.mu.RUnlock()
	if err != nil {
		return workshop.ShaderOptions{}, err
	}
	return workshop.ReadShaderOptions(target.Directory, filename, language)
}

func (a *App) SaveLocalShaderOptions(targetID, filename, revision string, changes map[string]string) (string, error) {
	run, bridge, err := a.saveShaderOptions(targetID, filename, revision, changes)
	if err != nil {
		return "", err
	}
	if run == "" || bridge == nil || len(changes) == 0 {
		return "saved", nil
	}
	return bridge.shaderBridge.reload(bridge.ctx, run, filename), nil
}

func (a *App) saveShaderOptions(targetID, filename, revision string, changes map[string]string) (string, *socialManager, error) {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.RLock()
	defer a.mu.RUnlock()
	state := a.service.State()
	if state.Busy || a.workshopBusy() {
		return "", nil, errors.New("дождитесь завершения загрузки")
	}
	target, err := a.workshopTarget(targetID)
	if err != nil {
		return "", nil, err
	}
	if err := workshop.SaveShaderOptions(target.Directory, filename, revision, changes); err != nil {
		return "", nil, err
	}
	if state.Running && state.Directory != "" {
		gameDir, gameErr := os.Stat(filepath.Join(state.Directory, "shaderpacks"))
		targetDir, targetErr := os.Stat(filepath.Join(target.Directory, "shaderpacks"))
		if gameErr == nil && targetErr == nil && os.SameFile(gameDir, targetDir) {
			return state.GameBridgeRun, a.social, nil
		}
	}
	return "", nil, nil
}

func (a *App) TranslateShaderLabel(text, language string) (string, error) {
	a.mu.RLock()
	selected := a.settings.ShaderLanguage
	if selected == "" {
		selected = launcher.InterfaceLanguage(a.settings.InterfaceLanguage, launcher.SystemLanguage())
	}
	a.mu.RUnlock()
	if language != selected || selected == "original" {
		return text, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 14*time.Second)
	defer cancel()
	return a.workshop.Client.TranslateDescription(ctx, text, language)
}
