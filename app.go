package main

import (
	"context"
	"fmt"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"os/exec"
	"path/filepath"
	"sintet/internal/identity"
	"sintet/internal/launcher"
	"sintet/internal/workshop"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type App struct {
	remotePackBusy   atomic.Bool
	tray             *launcherTray
	forceQuit        atomic.Bool
	windowHidden     atomic.Bool
	gameHidWindow    atomic.Bool
	cacheMu          sync.Mutex
	internetOffline  atomic.Bool
	social           *socialManager
	overlayMu        sync.Mutex
	overlay          *overlayBridge
	overlayError     error
	contentMu        sync.Mutex
	workshop         *workshop.Manager
	avatarDB         *avatarDatabase
	news             *newsFeed
	authMu           sync.Mutex
	profiles         *profileRoster
	mu               sync.RWMutex
	service          *launcher.Service
	ctx              context.Context
	settings         launcher.Settings
	defaults         launcher.Settings
	settingsPath     string
	syncCancel       context.CancelFunc
	profileRefresh   chan struct{}
	activity         *activityTracker
	warning          string
	appearanceMu     sync.Mutex
	appearance       Appearance
	appearanceSlots  []AppearanceSlot
	appearanceActive int
	appearancePath   string
	backgroundDir    string
	draftBackground  string
	draftFont        string
	updates          *updateManager
	support          *supportManager
	importer         *launcherImporter
}

// Installer builds override this public URL; local development keeps localhost.
var defaultIdentityURL = "https://sintet.xyz"

// Preview builds use a separate data directory to keep real profiles intact.
var previewDataDirectory string

func NewApp() *App {
	appRoot := filepath.Join(documentsFolder(), "Sintet")
	if previewDataDirectory != "" {
		appRoot = previewDataDirectory
	}
	defaults := launcher.DefaultSettings(filepath.Join(appRoot, "Minecraft"))
	settingsPath := filepath.Join(appRoot, "settings.json")
	settings, e := launcher.ReadSettings(settingsPath, defaults)
	firstLaunch := os.IsNotExist(e)
	support := newSupportManager(appRoot, firstLaunch)
	importer := newLauncherImporter(appRoot, firstLaunch)
	warning := ""
	if e != nil {
		settings = defaults
		if !os.IsNotExist(e) {
			warning = "Не удалось прочитать настройки: " + e.Error()
		}
	}
	if os.IsNotExist(e) {
		exe, _ := os.Executable()
		base := filepath.Dir(exe)
		if filepath.Base(base) == "bin" && filepath.Base(filepath.Dir(base)) == "build" {
			base = filepath.Dir(filepath.Dir(base))
		}
		legacy := filepath.Join(base, ".minecraft")
		if info, err := os.Stat(legacy); err == nil && info.IsDir() {
			if err = launcher.MigrateRoot(legacy, settings.GameDirectory); err != nil {
				settings.GameDirectory = legacy
				warning = "Не удалось перенести папку игры: " + err.Error()
			}
		}
	}
	if err := launcher.WriteSettings(settingsPath, settings); err != nil {
		warning = "Не удалось сохранить настройки: " + err.Error()
	}
	base := os.Getenv("SINTET_ID_URL")
	if base == "" {
		base = defaultIdentityURL
	}
	appearancePath := filepath.Join(appRoot, "appearance.json")
	library := readAppearanceLibrary(appearancePath)
	return &App{avatarDB: newAvatarDatabase(), appearance: library.Current, appearanceSlots: library.Slots, appearanceActive: library.ActiveSlot, appearancePath: appearancePath, backgroundDir: filepath.Join(appRoot, "Backgrounds"), news: newNewsFeed(appRoot, mojangNewsURL, "minecraft.net"), profiles: newProfileRoster(appRoot, base), service: launcher.New(settings.GameDirectory), settings: settings, defaults: defaults, settingsPath: settingsPath, warning: warning, updates: newUpdateManager(filepath.Join(appRoot, "Updates")), support: support, importer: importer}
}
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	if tray, err := newLauncherTray(a.runTrayAction); err == nil {
		a.tray = tray
	} else {
		a.mu.Lock()
		a.warning = "Трей недоступен: " + err.Error()
		a.mu.Unlock()
	}
	if updateHealthMarker != "" {
		_ = os.WriteFile(updateHealthMarker, []byte("ok"), 0600)
	}
	a.workshop = newWorkshop(a.profiles.root)
	a.profileRefresh = make(chan struct{}, 1)
	syncCtx, cancel := context.WithCancel(ctx)
	a.syncCancel = cancel
	go a.watchConnectivity(syncCtx)
	go a.watchWindowMinimize(syncCtx)
	if tracker, err := openActivity(a.profiles.root); err == nil {
		a.activity = tracker
		active, _ := a.activityAccounts()
		tracker.selectAccount(active)
		go a.trackActivity(syncCtx)
		go a.syncActivity(syncCtx)
	}
	a.social = newSocialManager(a, syncCtx)
	if err := a.social.startIPC(); err != nil {
		a.social.error(err)
	}
	go a.social.run()
	go a.syncWorkshop(syncCtx)
	go a.watchCatalogThemes(syncCtx)
	go a.watchOverlay(syncCtx)
	go func() {
		a.syncSavedProfiles(syncCtx)
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-syncCtx.Done():
				return
			case <-ticker.C:
				a.syncActiveProfile(syncCtx)
			case <-a.profileRefresh:
				a.syncActiveProfile(syncCtx)
			}
		}
	}()
}
func (a *App) shutdown(ctx context.Context) {
	if a.tray != nil {
		a.tray.close()
	}
	if a.workshop != nil {
		a.workshop.Cancel()
	}
	a.appearanceMu.Lock()
	a.removeDraftBackground()
	font := a.draftFont
	a.draftFont = ""
	a.removeUnusedAppearanceAsset(font)
	a.appearanceMu.Unlock()
	if a.syncCancel != nil {
		a.syncCancel()
	}
	a.finishActivity()
	a.CancelSintetLogin()
	a.CancelMicrosoftLogin()
	a.mu.RLock()
	defer a.mu.RUnlock()
	a.service.Shutdown()
}
func (a *App) OpenCommunity(service string) error {
	if a.ctx == nil {
		return fmt.Errorf("Sintet ещё запускается")
	}
	var address string
	switch service {
	case "discord":
		address = "https://discord.gg/KURVdKAUAy"
	case "telegram":
		address = "https://t.me/sinted_launcher"
	case "monobank":
		address = "https://send.monobank.ua/jar/9bgbYgZ9QU"
	default:
		return fmt.Errorf("неизвестная ссылка сообщества")
	}
	runtime.BrowserOpenURL(a.ctx, address)
	return nil
}
func (a *App) ListVersions() (launcher.Catalog, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.service.ListVersions()
}
func (a *App) LoaderVersions(base, kind string) ([]string, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.service.LoaderVersions(base, kind)
}
func (a *App) Installed(id string) bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.service.Installed(id)
}
func (a *App) Install(base, kind, version string) error {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if a.workshopBusy() {
		return fmt.Errorf("дождитесь установки модов")
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.service.StartInstallWithJava(base, kind, version, a.settings.JavaPath)
}
func (a *App) CancelInstall()        { a.mu.RLock(); defer a.mu.RUnlock(); a.service.Cancel() }
func (a *App) State() launcher.State { a.mu.RLock(); defer a.mu.RUnlock(); return a.service.State() }
func (a *App) Logs() string          { a.mu.RLock(); defer a.mu.RUnlock(); return a.service.Logs() }
func (a *App) Journal(revision string) (launcher.GameJournal, error) {
	a.mu.RLock()
	service := a.service
	a.mu.RUnlock()
	return service.Journal(revision)
}
func (a *App) Launch(id, name string) error {
	return a.launchInstance(id, name, 0, false, nil)
}
func (a *App) launchInstance(id, name string, quickPlayPort int, openLobby bool, queueBridgeJoin func()) error {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	if a.forceQuit.Load() {
		return context.Canceled
	}
	if (a.workshop != nil && a.workshop.State().Busy) || (a.remotePackBusy.Load() && quickPlayPort == 0) {
		return fmt.Errorf("дождитесь установки модов")
	}
	a.authMu.Lock()
	defer a.authMu.Unlock()
	if err := a.profileChangeAllowed(quickPlayPort > 0); err != nil {
		return err
	}
	var account *identity.Account
	var accessToken, provider, authlibAPI, clientID, xuid string
	if slot := a.profiles.find(a.profiles.active); slot != nil {
		provider = slot.Provider
		switch provider {
		case "sintet":
			verified, token, err := slot.sintet.GameIdentity(context.Background())
			if err != nil {
				return err
			}
			if verified == nil {
				return fmt.Errorf("войдите через Sintet ID заново")
			}
			account, accessToken, authlibAPI = verified, token, slot.sintet.ServerURL()
		case "ely":
			verified, token, err := slot.ely.Refresh(context.Background())
			if err != nil {
				return err
			}
			account, accessToken = &verified, token
		case "microsoft":
			verified, token, appID, xboxID, err := slot.microsoft.GameIdentity(context.Background())
			if err != nil {
				return err
			}
			account, accessToken, clientID, xuid = &verified, token, appID, xboxID
		case "local":
			name = slot.Name
		}
	}
	a.mu.RLock()
	overlayEnabled := a.settings.OverlayEnabled
	a.mu.RUnlock()
	var overlayAvatar string
	if overlayEnabled && account != nil {
		overlayAvatar = a.overlayAvatar(account)
	}
	a.mu.RLock()
	service := a.service
	settings := a.settings
	options := settings.LaunchOptions(systemMemoryMB())
	options.LauncherVersion = launcherVersion
	options.QuickPlayPort = quickPlayPort
	options.OpenOverlayLobby = openLobby && settings.OverlayEnabled
	options.OverlayHotkey = settings.OverlayHotkey
	options.InterfaceLanguage = launcher.InterfaceLanguage(settings.InterfaceLanguage, launcher.SystemLanguage())
	if strings.HasPrefix(id, "build-") {
		if build, ok := a.workshopBuildByID(id); ok {
			options.OverlayBuildName = build.Name
			if settings.OverlayEnabled {
				options.OverlayBuildPreset, options.OverlayBuildIcon = a.overlayBuildIcon(build.Icon)
			}
			if !build.UseGlobalSettings {
				options.MinMemoryMB = build.MinMemoryMB
				options.MemoryMB = build.MemoryMB
				options.JavaPath = build.JavaPath
				options.Width, options.Height = build.Width, build.Height
				options.Fullscreen = build.Fullscreen
				options.JVMArgs = strings.Fields(build.JVMArgs)
			}
		}
	}
	if a.social != nil {
		a.social.resetGameLog()
		options.ObserveLog = a.social.observeGameLog
	}
	if settings.OverlayEnabled {
		options.PrepareOverlay = a.prepareGameOverlay
		options.OverlayAvatar = overlayAvatar
		options.OverlayNetworkPinFile = filepath.Join(a.profiles.root, "overlay-network-pinned")
		if settings.ApplyOverlayFont {
			options.OverlayFont = a.overlayFontPath()
		}
	}
	if a.social != nil && a.social.address != "" {
		options.PrepareGameBridge = a.prepareJavaBridge
		options.OverlayIPC = a.social.address
		options.OverlayIPCSecret = a.social.secret
		options.QueueBridgeJoin = queueBridgeJoin
	}
	if account != nil {
		if provider == "sintet" && a.activity != nil {
			accountID := account.ID
			options.ObserveRun = func(running bool) { a.activity.gameChanged(accountID, running) }
		}
		name = account.Username
		options.IdentityUUID = account.ID
		options.IdentityProvider = provider
		options.AccessToken = accessToken
		options.IdentityClientID = clientID
		options.IdentityXUID = xuid
		options.AuthlibAPI = authlibAPI
	}
	observeRun := options.ObserveRun
	lobbyLaunch := quickPlayPort > 0 || a.social != nil && a.social.snapshot().Room != nil
	options.ObserveRun = func(running bool) {
		if observeRun != nil {
			observeRun(running)
		}
		action := settings.GameStartAction
		if lobbyLaunch && action == "quit" {
			action = "tray"
		}
		a.gameWindowBehavior(running, action)
	}
	err := service.LaunchWithOptions(id, name, options)
	a.mu.RUnlock()
	return err
}
func (a *App) OpenGameFolder() {
	a.mu.RLock()
	root := a.settings.GameDirectory
	a.mu.RUnlock()
	_ = exec.Command("explorer.exe", root).Start()
}
func (a *App) OpenProfileFolder(id string) error {
	a.mu.RLock()
	path, err := a.service.ProfileDirectory(id, a.settings.InstanceMode)
	a.mu.RUnlock()
	if err != nil {
		return err
	}
	return exec.Command("explorer.exe", path).Start()
}
func (a *App) Installations() []launcher.InstalledProfile {
	a.mu.RLock()
	service := a.service
	a.mu.RUnlock()
	store, _ := a.readWorkshopBuilds()
	builds := make(map[string]WorkshopBuild, len(store.Builds))
	for _, build := range store.Builds {
		builds[build.ID] = build
	}
	result := service.Installations()
	for i := range result {
		build := builds[result[i].ID]
		result[i].Name, result[i].Icon = build.Name, build.Icon
	}
	return result
}
func (a *App) configuration() launcher.Configuration {
	return launcher.Configuration{InterfaceLanguage: launcher.InterfaceLanguage(a.settings.InterfaceLanguage, launcher.SystemLanguage()), Settings: a.settings, Defaults: a.defaults, SystemMemoryMB: systemMemoryMB(), EffectiveMemoryMB: launcher.EffectiveMemory(a.settings, systemMemoryMB()), Warning: a.warning}
}
func (a *App) GetSettings() launcher.Configuration {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.configuration()
}
func (a *App) SaveSettings(settings launcher.Settings, moveFiles bool) (launcher.Configuration, error) {
	a.contentMu.Lock()
	defer a.contentMu.Unlock()
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.workshopBusy() {
		return a.configuration(), fmt.Errorf("дождитесь установки модов перед изменением настроек")
	}
	settings.GameDirectory = filepath.Clean(strings.TrimSpace(settings.GameDirectory))
	settings.JavaPath = strings.TrimSpace(settings.JavaPath)
	if settings.GameStartAction == "" {
		settings.GameStartAction = "tray"
	}
	if settings.CloseAction == "" {
		settings.CloseAction = "quit"
	}
	label, _, _, hotkeyErr := launcher.ParseOverlayHotkey(settings.OverlayHotkey)
	if hotkeyErr != nil {
		return a.configuration(), hotkeyErr
	}
	settings.OverlayHotkey = label
	if e := launcher.ValidateSettings(settings); e != nil {
		return a.configuration(), e
	}
	if !settings.AutoMemory && settings.MemoryMB > systemMemoryMB() {
		return a.configuration(), fmt.Errorf("нельзя выделить больше установленной оперативной памяти")
	}
	autostartChanged := settings.StartWithWindows != a.settings.StartWithWindows || settings.StartInTray != a.settings.StartInTray
	oldAutostart, oldStartInTray := a.settings.StartWithWindows, a.settings.StartInTray
	autostartCommitted := false
	if autostartChanged {
		if err := configureAutostart(settings.StartWithWindows); err != nil {
			return a.configuration(), err
		}
		defer func() {
			if !autostartCommitted {
				_ = configureAutostart(oldAutostart)
			}
		}()
	}
	changed := !strings.EqualFold(settings.GameDirectory, a.settings.GameDirectory)
	if changed {
		state := a.service.State()
		if state.Busy || state.Running || a.remotePackBusy.Load() {
			return a.configuration(), fmt.Errorf("закройте Minecraft и дождитесь завершения загрузки перед сменой папки")
		}
		if moveFiles {
			if e := launcher.MigrateRoot(a.settings.GameDirectory, settings.GameDirectory); e != nil {
				return a.configuration(), e
			}
			settings.JavaPath = launcher.RebasePath(settings.JavaPath, a.settings.GameDirectory, settings.GameDirectory)
		} else if e := os.MkdirAll(settings.GameDirectory, 0755); e != nil {
			return a.configuration(), e
		}
	}
	// Persist before switching the in-memory service. On a rare save failure, keep
	// the usable new path in this process and surface the persistence error.
	err := launcher.WriteSettings(a.settingsPath, settings)
	autostartCommitted = err == nil
	if err != nil && autostartChanged {
		settings.StartWithWindows = oldAutostart
		settings.StartInTray = oldStartInTray
	}
	a.settings = settings
	if changed {
		a.service = launcher.New(settings.GameDirectory)
	}
	a.warning = ""
	if err != nil {
		a.warning = "Настройки не сохранены на диск: " + err.Error()
		return a.configuration(), err
	}
	return a.configuration(), nil
}
func (a *App) ChooseGameDirectory() (string, error) {
	a.mu.RLock()
	path := a.settings.GameDirectory
	a.mu.RUnlock()
	if _, e := os.Stat(path); e != nil {
		path = documentsFolder()
	}
	return runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{Title: "Папка Minecraft", DefaultDirectory: path, CanCreateDirectories: true})
}
func (a *App) ChooseJava() (string, error) {
	return runtime.OpenFileDialog(a.ctx, runtime.OpenDialogOptions{Title: "Выберите java.exe", Filters: []runtime.FileFilter{{DisplayName: "Java", Pattern: "java.exe;javaw.exe"}}})
}
func (a *App) CheckJava(path string) (launcher.JavaInfo, error) {
	return launcher.CheckJava(context.Background(), path)
}
func (a *App) VersionVariants() (launcher.VariantCatalog, error) {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.service.VersionVariants()
}
