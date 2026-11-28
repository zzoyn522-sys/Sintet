package releaseversion

import (
	"fmt"
	"strconv"
	"strings"
)

// Build maps a release version to the numeric identifier compared by existing
// installers and updaters. The second component reserves a lane per channel.
// A TEST build sorts above any public build, including future public patches.
func Build(version, channel string) (string, error) {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("expected a three-part release version")
	}
	for _, part := range parts {
		number, err := strconv.ParseUint(part, 10, 31)
		if err != nil || strconv.FormatUint(number, 10) != part {
			return "", fmt.Errorf("invalid release version %q", version)
		}
	}
	lane := "0"
	revision := parts[2]
	switch channel {
	case "test":
		lane = "1"
		if parts[0] != "0" || version == "0.0.0" {
			return "", fmt.Errorf("TEST versions must be in the 0.x.x series")
		}
	case "", "stable", "beta":
		if parts[0] != "1" || parts[1] != "1" {
			return "", fmt.Errorf("public versions must be in the 1.1.x series")
		}
		patch, err := strconv.ParseUint(parts[2], 10, 30)
		if err != nil {
			return "", fmt.Errorf("public patch is too large")
		}
		build := patch * 2
		if channel != "beta" {
			build++
		}
		revision = strconv.FormatUint(build, 10)
	default:
		return "", fmt.Errorf("unsupported release channel %q", channel)
	}
	return strings.Join([]string{"2", lane, parts[1], revision}, "."), nil
}

// Display returns the signed release number, accepting legacy four-part
// stable overrides used by existing releases.
func Display(version, build, channel string) string {
	if expected, err := Build(version, channel); err == nil && expected == build {
		return version
	}
	if channel == "" || channel == "stable" {
		parts := strings.Split(version, ".")
		if len(parts) == 4 {
			for _, part := range parts {
				number, err := strconv.ParseUint(part, 10, 31)
				if err != nil || strconv.FormatUint(number, 10) != part {
					return ""
				}
			}
			if version != "0.0.0.0" {
				return version
			}
		}
	}
	return ""
}
