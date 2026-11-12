//go:build windows

package main

import (
	"crypto/sha256"
	_ "embed"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"sintet/internal/launcher"
	"strconv"
)

// Build scripts compile the compact WPF updater before building the launcher.
//
//go:embed updater/Sintet-Update.exe
var prelaunchUpdater []byte

func startPrelaunchUpdater() error {
	if len(prelaunchUpdater) == 0 {
		return os.ErrNotExist
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}
	root := filepath.Join(documentsFolder(), "Sintet", "Updates")
	if err = os.MkdirAll(root, 0700); err != nil {
		return err
	}
	path := filepath.Join(root, "Sintet-Update.exe")
	expected := sha256.Sum256(prelaunchUpdater)
	if actual, readErr := os.ReadFile(path); readErr == nil && sha256.Sum256(actual) == expected {
		return startPrelaunchUpdaterProcess(path, executable)
	}
	temporary := path + ".tmp"
	file, err := os.OpenFile(temporary, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0700)
	if err != nil {
		return err
	}
	if _, err = file.Write(prelaunchUpdater); err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil {
		_ = os.Remove(temporary)
		return err
	}
	if closeErr != nil {
		_ = os.Remove(temporary)
		return closeErr
	}
	if err = os.Remove(path); err != nil && !os.IsNotExist(err) {
		_ = os.Remove(temporary)
		return err
	}
	if err = os.Rename(temporary, path); err != nil {
		_ = os.Remove(temporary)
		return err
	}
	return startPrelaunchUpdaterProcess(path, executable)
}

func prelaunchLanguage(root string) string {
	var preferences struct {
		InterfaceLanguage string `json:"interfaceLanguage"`
	}
	if data, err := os.ReadFile(filepath.Join(root, "settings.json")); err == nil {
		_ = json.Unmarshal(data, &preferences)
	}
	return launcher.InterfaceLanguage(preferences.InterfaceLanguage, launcher.SystemLanguage())
}

func startPrelaunchUpdaterProcess(updater, executable string) error {
	command := exec.Command(updater, "--update", executable, launcherVersion, strconv.Itoa(os.Getpid()), "", releaseVersionLabel(launcherVersion, launcherChannel), prelaunchLanguage(filepath.Dir(filepath.Dir(updater))))
	command.Dir = filepath.Dir(executable)
	return command.Start()
}
