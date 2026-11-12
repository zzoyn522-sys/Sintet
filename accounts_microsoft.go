package main

import (
	"errors"
	"os"
	"strings"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

var microsoftAppID = "e266c497-6ae6-4863-816a-a1ad4632942b"

func microsoftClientID() string {
	if configured := strings.TrimSpace(os.Getenv("SINTET_MICROSOFT_CLIENT_ID")); configured != "" {
		return configured
	}
	return microsoftAppID
}

func (a *App) BeginMicrosoftLogin() (string, error) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return "", err
	}
	slot, err := a.profiles.newSlot("microsoft", "")
	if err != nil {
		return "", err
	}
	prompt, err := slot.microsoft.Start()
	if err != nil {
		return "", err
	}
	a.profiles.pending = slot
	a.profiles.microsoftVerification = prompt.URL
	a.profiles.loginError = ""
	return prompt.Code, nil
}

func (a *App) OpenMicrosoftVerification() error {
	a.authMu.Lock()
	address := a.profiles.microsoftVerification
	a.authMu.Unlock()
	if address == "" {
		return errors.New("Запрос входа Microsoft уже завершён.")
	}
	runtime.BrowserOpenURL(a.ctx, address)
	return nil
}

func (a *App) CancelMicrosoftLogin() {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	r := a.profiles
	if slot := r.pending; slot != nil && slot.microsoft != nil {
		r.pending = nil
		r.microsoftVerification = ""
		_ = slot.microsoft.Forget()
	}
}
