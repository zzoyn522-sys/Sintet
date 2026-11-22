package main

import (
	"context"
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

func (a *App) SaveLocalShaderOptions(targetID, filename, revision string, changes map[string]string) error {
	return a.changeLocalWorkshop(targetID, func(target workshop.Target) error {
		return workshop.SaveShaderOptions(target.Directory, filename, revision, changes)
	})
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
