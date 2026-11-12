package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"sintet/internal/accountfiles"
	"sintet/internal/identity"
	"sintet/internal/launcher"
)

const defaultProfile = "local-default"
const profileLimit = 5

var localNickname = regexp.MustCompile("^[A-Za-z0-9_]{3,16}$")
var storedProfileKey = regexp.MustCompile("^[a-f0-9]{32}$")

type profileRecord struct {
	Key      string `json:"key"`
	Provider string `json:"provider"`
	Name     string `json:"name,omitempty"`
}
type profileIndex struct {
	HideDefault bool            `json:"hideDefault,omitempty"`
	Active      string          `json:"active"`
	Profiles    []profileRecord `json:"profiles"`
}
type profileSlot struct {
	profileRecord
	sintet    *identity.Client
	ely       *identity.ElyClient
	microsoft *identity.MicrosoftClient
}
type profileRoster struct {
	hideDefault           bool
	root, base            string
	active                string
	slots                 []*profileSlot
	pending               *profileSlot
	microsoftVerification string
	loginError            string
	loadError             error
}
type profileView struct {
	Key      string           `json:"key"`
	Provider string           `json:"provider"`
	Account  identity.Account `json:"account"`
}
type accountSelection struct {
	CanAdd bool `json:"canAdd"`
	identity.State
	Active          string        `json:"active"`
	Profiles        []profileView `json:"profiles"`
	PendingProvider string        `json:"pendingProvider,omitempty"`
	LoginError      string        `json:"loginError"`
}

func (r *profileRoster) restore(record profileRecord) *profileSlot {
	if !storedProfileKey.MatchString(record.Key) &&
		!(record.Key == "legacy-sintet" && record.Provider == "sintet") &&
		!(record.Key == "legacy-ely" && record.Provider == "ely") {
		return nil
	}
	slot := r.makeSlot(record)
	switch record.Provider {
	case "sintet":
		if slot.sintet.LoadSaved() {
			return slot
		}
	case "ely":
		if slot.ely.LoadSaved() {
			return slot
		}
	case "microsoft":
		if slot.microsoft.LoadSaved() {
			return slot
		}
	case "local":
		if localNickname.MatchString(record.Name) {
			return slot
		}
	}
	return nil
}

func (r *profileRoster) makeSlot(record profileRecord) *profileSlot {
	slot := &profileSlot{profileRecord: record}
	path := filepath.Join(r.root, "accounts", record.Key+".dpapi")
	switch record.Provider {
	case "sintet":
		if record.Key == "legacy-sintet" {
			path = filepath.Join(r.root, "identity.dpapi")
		}
		slot.sintet = identity.New(r.base, path)
		slot.sintet.ServiceBase = "https://sintet-identity.zzoyn522.workers.dev"
	case "ely":
		if record.Key == "legacy-ely" {
			path = filepath.Join(r.root, "ely.dpapi")
		}
		slot.ely = identity.NewEly(path)
	case "microsoft":
		slot.microsoft = identity.NewMicrosoft(path, microsoftClientID())
	}
	return slot
}

func (r *profileRoster) canAdd() bool {
	count := len(r.slots)
	if !r.hideDefault {
		count++
	}
	return count < profileLimit
}

func (r *profileRoster) newSlot(provider, name string) (*profileSlot, error) {
	if !r.canAdd() {
		return nil, errors.New("в лаунчере можно сохранить до пяти аккаунтов")
	}
	var key [16]byte
	if _, err := rand.Read(key[:]); err != nil {
		return nil, err
	}
	return r.makeSlot(profileRecord{Key: hex.EncodeToString(key[:]), Provider: provider, Name: name}), nil
}

func (r *profileRoster) find(key string) *profileSlot {
	for _, slot := range r.slots {
		if slot.Key == key {
			return slot
		}
	}
	return nil
}

func (r *profileRoster) save(active string, slots []*profileSlot) error {
	return r.saveIndex(active, slots, r.hideDefault)
}

func (r *profileRoster) saveIndex(active string, slots []*profileSlot, hideDefault bool) error {
	if r.loadError != nil {
		return r.loadError
	}
	index := profileIndex{Active: active, HideDefault: hideDefault, Profiles: make([]profileRecord, 0, len(slots))}
	for _, slot := range slots {
		index.Profiles = append(index.Profiles, slot.profileRecord)
	}
	data, err := json.Marshal(index)
	if err != nil {
		return err
	}
	if err = accountfiles.Write(r.indexPath(), data); err != nil {
		return err
	}
	// The primary index is already committed. A backup failure must not roll back a successful change.
	_ = accountfiles.Write(r.backupPath(), data)
	r.active, r.slots, r.hideDefault = active, slots, hideDefault
	return nil
}

func localAccount(name string) identity.Account {
	name = strings.TrimSpace(name)
	if !localNickname.MatchString(name) {
		name = "Player"
	}
	return identity.Account{Username: name, ID: launcher.OfflineProfileID(name)}
}

func (s *profileSlot) state() identity.State {
	switch s.Provider {
	case "sintet":
		state := s.sintet.State()
		if state.Account == nil {
			state.Account = s.sintet.SavedAccount()
		}
		return state
	case "ely":
		return s.ely.State()
	case "microsoft":
		return s.microsoft.State()
	default:
		account := localAccount(s.Name)
		return identity.State{Provider: "local", Account: &account}
	}
}
func (s *profileSlot) logout() error {
	if s.sintet != nil {
		return s.sintet.Logout()
	}
	if s.ely != nil {
		return s.ely.Logout()
	}
	if s.microsoft != nil {
		return s.microsoft.Forget()
	}
	return nil
}

func (r *profileRoster) add(slot *profileSlot, defaultName string) error {
	account := slot.state().Account
	if account == nil {
		return errors.New("профиль не получен")
	}
	if slot.Provider == "local" && !r.hideDefault && account.ID == localAccount(defaultName).ID {
		return r.save(defaultProfile, r.slots)
	}
	slots := append([]*profileSlot(nil), r.slots...)
	for i, previous := range slots {
		old := previous.state().Account
		if previous.Provider == slot.Provider && old != nil && strings.EqualFold(old.ID, account.ID) {
			if slot.Provider == "local" {
				return r.save(previous.Key, r.slots)
			}
			if slot.Provider == "sintet" {
				return errors.New("этот Sintet ID уже добавлен. Войди в другой аккаунт или создай новый")
			}
			slots[i] = slot
			if err := r.save(slot.Key, slots); err != nil {
				return err
			}
			go func() { _ = previous.logout() }()
			return nil
		}
	}
	return r.save(slot.Key, append(slots, slot))
}

func (r *profileRoster) finishLogin() {
	if r.pending == nil {
		return
	}
	slot := r.pending
	state := slot.state()
	if state.Pending {
		return
	}
	r.pending = nil
	r.microsoftVerification = ""
	r.loginError = state.Error
	if state.Account == nil {
		return
	}
	if err := r.add(slot, ""); err != nil {
		r.loginError = "Не удалось сохранить аккаунт: " + err.Error()
		go func() { _ = slot.logout() }()
	}
}

func (r *profileRoster) selection(defaultName string) accountSelection {
	r.finishLogin()
	local := localAccount(defaultName)
	result := accountSelection{
		State:      identity.State{Provider: "local", Account: &local},
		Active:     r.active,
		CanAdd:     r.canAdd(),
		LoginError: r.loginError,
	}
	if !r.hideDefault {
		result.Profiles = append(result.Profiles, profileView{Key: defaultProfile, Provider: "local", Account: local})
	}
	for _, slot := range r.slots {
		state := slot.state()
		if slot.Key == r.active {
			result.State = state
		}
		if state.Account != nil {
			result.Profiles = append(result.Profiles, profileView{Key: slot.Key, Provider: slot.Provider, Account: *state.Account})
		}
	}
	if r.pending != nil {
		result.Pending = true
		result.PendingProvider = r.pending.Provider
	}
	if r.loadError != nil {
		result.Error = r.loadError.Error()
	}
	return result
}

func (a *App) IdentityState(defaultName string) accountSelection {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return a.profiles.selection(defaultName)
}

func (a *App) accountChangeAllowed(allowPack ...bool) error {
	if a.remotePackBusy.Load() && !(len(allowPack) > 0 && allowPack[0]) {
		return errors.New("дождитесь синхронизации сборки перед сменой аккаунта")
	}
	state := a.State()
	if state.Busy || state.Running {
		return errors.New("закройте игру и дождитесь завершения установки перед сменой аккаунта")
	}
	return nil
}

func (a *App) profileChangeAllowed(allowPack ...bool) error {
	if err := a.accountChangeAllowed(allowPack...); err != nil {
		return err
	}
	a.profiles.finishLogin()
	if a.profiles.pending != nil {
		return errors.New("дождитесь завершения входа")
	}
	return a.profiles.loadError
}

func (a *App) SelectAccount(key string) error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return err
	}
	if (key == defaultProfile && a.profiles.hideDefault) || (key != defaultProfile && a.profiles.find(key) == nil) {
		return errors.New("аккаунт не найден")
	}
	if key == a.profiles.active {
		return nil
	}
	return a.profiles.save(key, a.profiles.slots)
}

func (a *App) AddLocalAccount(name, defaultName string) error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return err
	}
	name = strings.TrimSpace(name)
	if !localNickname.MatchString(name) {
		return errors.New("ник должен содержать 3–16 латинских букв, цифр или _")
	}
	slot, err := a.profiles.newSlot("local", name)
	if err != nil {
		return err
	}
	return a.profiles.add(slot, defaultName)
}

func (a *App) RenameLocalAccount(key, name, defaultName string) error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return err
	}
	r := a.profiles
	slot := r.find(key)
	if r.active != key || slot == nil || slot.Provider != "local" {
		return errors.New("можно изменить ник только у выбранного локального профиля")
	}
	name = strings.TrimSpace(name)
	if !localNickname.MatchString(name) {
		return errors.New("ник должен содержать 3–16 латинских букв, цифр или _")
	}
	if slot.Name == name {
		return nil
	}
	if !r.hideDefault && localAccount(defaultName).Username == name {
		return errors.New("локальный профиль с таким ником уже есть")
	}
	slots := append([]*profileSlot(nil), r.slots...)
	for i, other := range slots {
		if other == slot {
			updated := *slot
			updated.Name = name
			slots[i] = &updated
		} else if other.Provider == "local" && other.Name == name {
			return errors.New("локальный профиль с таким ником уже есть")
		}
	}
	return r.save(key, slots)
}

func (a *App) LoginEly(login, password, totp string) error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return err
	}
	slot, err := a.profiles.newSlot("ely", "")
	if err != nil {
		return err
	}
	if err = slot.ely.Login(login, password, totp); err != nil {
		return err
	}
	if err = a.profiles.add(slot, ""); err != nil {
		go func() { _ = slot.logout() }()
	}
	return err
}

func (a *App) beginSintetLogin() (string, error) {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(); err != nil {
		return "", err
	}
	slot, err := a.profiles.newSlot("sintet", "")
	if err != nil {
		return "", err
	}
	slot.sintet.OnSuccess = func() {
		a.authMu.Lock()
		a.profiles.finishLogin()
		a.authMu.Unlock()
		if a.ctx != nil {
			runtime.WindowShow(a.ctx)
			runtime.WindowUnminimise(a.ctx)
		}
	}
	var excluded []string
	for _, existing := range a.profiles.slots {
		if existing.Provider == "sintet" {
			if account := existing.state().Account; account != nil {
				excluded = append(excluded, account.ID)
			}
		}
	}
	address, err := slot.sintet.StartWithExcludedAccounts(excluded)
	if err != nil {
		return "", err
	}
	a.profiles.pending = slot
	a.profiles.loginError = ""
	return address, nil
}
func (a *App) BeginSintetLogin() error {
	address, err := a.beginSintetLogin()
	if err == nil {
		runtime.BrowserOpenURL(a.ctx, address)
	}
	return err
}
func (a *App) CancelSintetLogin() {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	r := a.profiles
	if slot := r.pending; slot != nil && slot.sintet != nil {
		slot.sintet.Cancel()
		r.pending = nil
		if slot.sintet.HasSession() {
			go func() { _ = slot.logout() }()
		}
	}
}
func (a *App) OpenElyRegistration() { runtime.BrowserOpenURL(a.ctx, "https://account.ely.by/register") }

func (a *App) RemoveAccount(key string) error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return a.removeAccount(key)
}

func (a *App) LogoutAccount() error {
	a.authMu.Lock()
	defer a.authMu.Unlock()
	return a.removeAccount(a.profiles.active)
}

func (a *App) removeAccount(key string) error {
	if err := a.profileChangeAllowed(); err != nil {
		return err
	}
	r := a.profiles
	if key == defaultProfile {
		if r.hideDefault || len(r.slots) == 0 {
			return errors.New("нужен хотя бы один профиль")
		}
		next := r.active
		if next == key {
			next = r.slots[len(r.slots)-1].Key
		}
		return r.saveIndex(next, r.slots, true)
	}
	slot := r.find(key)
	if slot == nil {
		return errors.New("аккаунт не найден")
	}
	remaining := make([]*profileSlot, 0, len(r.slots)-1)
	for _, other := range r.slots {
		if other != slot {
			remaining = append(remaining, other)
		}
	}
	next := r.active
	if next == key {
		next = defaultProfile
		if len(remaining) > 0 {
			next = remaining[len(remaining)-1].Key
		}
	}
	hideDefault := r.hideDefault && len(remaining) > 0
	if slot.sintet != nil {
		if err := slot.sintet.Forget(); err != nil {
			return err
		}
	}
	if slot.ely != nil {
		if err := slot.ely.Forget(); err != nil {
			return err
		}
	}
	if slot.microsoft != nil {
		if err := slot.microsoft.Forget(); err != nil {
			return err
		}
	}
	if err := r.saveIndex(next, remaining, hideDefault); err != nil {
		if slot.Provider != "local" {
			r.active, r.slots, r.hideDefault = next, remaining, hideDefault
		}
		return fmt.Errorf("не удалось сохранить список аккаунтов: %w", err)
	}
	return nil
}
func (a *App) LogoutSintetID() error { return a.LogoutAccount() }

func (a *App) RefreshProfiles() {
	select {
	case a.profileRefresh <- struct{}{}:
	default:
	}
}

func (a *App) syncSavedProfiles(ctx context.Context) {
	a.authMu.Lock()
	var clients []*identity.Client
	for _, slot := range a.profiles.slots {
		if slot.sintet != nil {
			clients = append(clients, slot.sintet)
		}
	}
	a.authMu.Unlock()
	var pending sync.WaitGroup
	for _, client := range clients {
		pending.Add(1)
		go func() {
			defer pending.Done()
			a.syncAvatar(ctx, client)
		}()
	}
	pending.Wait()
}

func (a *App) syncActiveProfile(ctx context.Context) {
	a.authMu.Lock()
	slot := a.profiles.find(a.profiles.active)
	a.authMu.Unlock()
	if slot != nil && slot.sintet != nil {
		refreshCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		slot.sintet.RefreshProfile(refreshCtx)
		cancel()
		a.syncAvatar(ctx, slot.sintet)
	}
}
