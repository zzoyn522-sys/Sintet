package main

import (
	"context"
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"os"
	"path/filepath"

	"sintet/internal/identity"
)

//go:embed all:frontend
var assets embed.FS

func main() {
	if len(os.Args) == 3 && os.Args[1] == "--sintet-import-relaunch" {
		runImportRelaunch(os.Args[2])
		return
	}
	if len(os.Args) == 3 && os.Args[1] == "--sintet-scoped-offer" {
		if err := writeScopedUpdateOffer(os.Args[2]); err != nil {
			os.Exit(1)
		}
		return
	}
	if targetedBootstrapAccountID != "" {
		if err := runTargetedBootstrap(targetedBootstrapAccountID); err != nil {
			showTargetedBootstrapError(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "--sintet-update-helper" {
		os.Exit(runUpdateHelper(os.Args[2:]))
	}
	updateHealthMarker = updateHealthMarkerFromArgs(os.Args[1:])
	if !identity.ThemePortalIsLocal() && updateHealthMarker == "" && !hasLauncherArg(os.Args[1:], "--startup") && !hasLauncherArg(os.Args[1:], "--sintet-skip-update") {
		if err := startPrelaunchUpdater(); err == nil {
			return
		}
	}
	app := NewApp()
	windowTitle := "Sintet"
	if launcherChannel == "test" {
		windowTitle += " " + app.LauncherDisplayVersion()
	}
	startHidden := hasLauncherArg(os.Args[1:], "--startup") && app.settings.StartInTray
	webviewPath := ""
	if previewDataDirectory != "" {
		webviewPath = filepath.Join(previewDataDirectory, "WebView")
	}
	backgroundMode, backdrop := launcherBackground()
	nativeBackdrop := backdrop != windows.None
	background := &options.RGBA{R: 13, G: 13, B: 13, A: 255}
	if nativeBackdrop {
		background.A = 0
	}
	err := wails.Run(&options.App{
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "sintet.launcher." + previewDataDirectory, OnSecondInstanceLaunch: func(data options.SecondInstanceData) {
			if !hasLauncherArg(data.Args, "--startup") {
				app.ShowLauncher()
			}
		}},
		StartHidden: startHidden, OnDomReady: func(ctx context.Context) {
			if backgroundMode != "" {
				script := `document.documentElement.dataset.backgroundExperiment = "` + backgroundMode + `"`
				if backgroundMode == "glow" {
					script += "\nvoid import('./background-wave.js')"
				}
				runtime.WindowExecJS(ctx, script)
			}
			if startHidden {
				if app.tray != nil {
					app.windowHidden.Store(true)
				} else {
					app.ShowLauncher()
				}
			}
		},
		OnBeforeClose: app.beforeClose,
		Title:         windowTitle, OnStartup: app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app}, Width: windowWidth, Height: windowHeight, MinWidth: windowWidth, MaxWidth: windowWidth, MinHeight: windowHeight, MaxHeight: windowHeight, DisableResize: true,
		DragAndDrop:      &options.DragAndDrop{EnableFileDrop: true},
		Frameless:        true,
		BackgroundColour: background,
		AssetServer:      &assetserver.Options{Assets: assets, Middleware: app.serveLauncherAssets},
		Windows:          &windows.Options{Theme: windows.Dark, WebviewUserDataPath: webviewPath, DisableWindowIcon: true, DisableFramelessWindowDecorations: !supportsRoundedWindow(), WebviewIsTransparent: nativeBackdrop, WindowIsTranslucent: nativeBackdrop, BackdropType: backdrop},
	})
	if err != nil {
		println(err.Error())
	}
}

func hasLauncherArg(args []string, expected string) bool {
	for _, arg := range args {
		if arg == expected {
			return true
		}
	}
	return false
}
