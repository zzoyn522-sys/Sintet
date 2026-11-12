//go:build windows

package main

import (
	"context"
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
	"sintet/internal/accountfiles"
)

func showTargetedBootstrapError(err error) {
	message, _ := windows.UTF16PtrFromString(err.Error())
	title, _ := windows.UTF16PtrFromString("Sintet – тестовое обновление")
	windows.MessageBox(0, message, title, 0x10)
}

// This one-time bootstrap upgrades old clients that cannot identify themselves
// to the release service. It never publishes a manifest to the stable channel.
func runTargetedBootstrap(allowedAccount string) error {
	app := NewApp()
	client := app.activeUpdateClient()
	if client == nil || client.SavedAccount() == nil || client.SavedAccount().ID != allowedAccount {
		return errors.New("Это тестовое обновление для folk. Сначала выбери этот Sintet ID в установленном лаунчере, затем закрой Sintet и запусти файл ещё раз.")
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall\Sintet`, registry.QUERY_VALUE)
	if err != nil {
		return errors.New("Установленный Sintet не найден. Этот файл обновляет лаунчер, установленный через установщик.")
	}
	root, _, err := key.GetStringValue("InstallLocation")
	key.Close()
	if err != nil || !filepath.IsAbs(root) {
		return errors.New("Не удалось найти папку установленного Sintet.")
	}
	var marker struct {
		Product   string
		Directory string
	}
	markerBytes, err := os.ReadFile(filepath.Join(root, "install.xml"))
	if err != nil || xml.Unmarshal(markerBytes, &marker) != nil || marker.Product != "Sintet.Launcher.8E6E8495-DB20-44FC-963E-567A8AC82181" || filepath.Clean(marker.Directory) != filepath.Clean(root) {
		return errors.New("Не удалось проверить установленный Sintet.")
	}
	target := filepath.Join(root, "Sintet-launcher.exe")
	if _, err = os.Stat(target); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	offer, err := client.UpdateOffer(ctx)
	if err != nil {
		return err
	}
	if len(offer) == 0 || string(offer) == "null" {
		return errors.New("Для этого аккаунта пока нет тестового обновления.")
	}
	if _, err = parseSignedUpdate(offer); err != nil {
		return err
	}
	text, _ := windows.UTF16PtrFromString("Закрой Sintet через значок в трее перед продолжением.\n\nБудет установлено тестовое обновление лобби для folk. Аккаунты, сборки и настройки сохранятся.")
	title, _ := windows.UTF16PtrFromString("Sintet – тестовое обновление")
	choice, _ := windows.MessageBox(0, text, title, 0x21)
	if choice != 1 {
		return nil
	}
	dir := filepath.Join(app.profiles.root, "Updates")
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	manifest := filepath.Join(dir, "targeted-offer.json")
	if err = accountfiles.Write(manifest, offer); err != nil {
		return err
	}
	updater := filepath.Join(dir, "Sintet-Private-Update.exe")
	if err = accountfiles.Write(updater, prelaunchUpdater); err != nil {
		return err
	}
	command := exec.Command(updater, "--update", target, launcherVersion, strconv.Itoa(os.Getpid()), manifest, releaseVersionLabel(launcherVersion, launcherChannel), prelaunchLanguage(app.profiles.root))
	command.Dir = root
	return command.Start()
}
