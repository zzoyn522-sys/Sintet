package main

import (
	"os"

	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

// Set only on local preview builds.
var backgroundExperiment string

func launcherBackground() (string, windows.BackdropType) {
	if backgroundExperiment != "on" {
		return "", windows.None
	}
	switch os.Getenv("SINTET_BACKGROUND_EXPERIMENT") {
	case "mica":
		if supportsNativeBackdrop() {
			return "mica", windows.Mica
		}
		return "dark", windows.None
	case "mesh":
		return "mesh", windows.None
	case "glow":
		return "glow", windows.None
	case "dark":
		return "dark", windows.None
	default:
		return "dots", windows.None
	}
}
