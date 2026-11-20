package main

import (
	"errors"
	"os"
	"path/filepath"

	"sintet/internal/accountfiles"
)

func prepareLauncherTour(root string, firstLaunch bool) string {
	marker := filepath.Join(root, "launcher-tour.pending")
	if firstLaunch {
		_ = accountfiles.Write(marker, []byte("pending\n"))
	}
	return marker
}

func (a *App) LauncherTourPending() bool {
	_, err := os.Stat(a.launcherTourMarker)
	return err == nil
}

func (a *App) FinishLauncherTour() error {
	err := os.Remove(a.launcherTourMarker)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}
