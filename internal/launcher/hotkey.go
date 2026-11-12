package launcher

import (
	"fmt"
	"strconv"
	"strings"
)

// The allowlist is shared by settings validation and the game environment.
// Modifier bits follow Win32 MOD_ALT, MOD_CONTROL and MOD_SHIFT.
func ParseOverlayHotkey(value string) (label string, key, modifiers int, err error) {
	if value == "" {
		value = "Shift + Tab"
	}
	parts := strings.Split(value, "+")
	bad := func() (string, int, int, error) {
		return "", 0, 0, fmt.Errorf("выберите Ctrl, Alt или Shift и клавишу Tab, F1–F12, букву или цифру")
	}
	if len(parts) < 2 || len(parts) > 4 {
		return bad()
	}
	for _, part := range parts[:len(parts)-1] {
		var bit int
		switch strings.TrimSpace(part) {
		case "Ctrl":
			bit = 2
		case "Alt":
			bit = 1
		case "Shift":
			bit = 4
		default:
			return bad()
		}
		if modifiers&bit != 0 {
			return bad()
		}
		modifiers |= bit
	}
	name := strings.TrimSpace(parts[len(parts)-1])
	switch {
	case name == "Tab":
		key = 9
	case len(name) == 1 && ((name[0] >= 'A' && name[0] <= 'Z') || (name[0] >= '0' && name[0] <= '9')):
		key = int(name[0])
	case strings.HasPrefix(name, "F"):
		n, e := strconv.Atoi(name[1:])
		if e != nil || n < 1 || n > 12 {
			return bad()
		}
		key = 111 + n
	default:
		return bad()
	}
	// Keep OS/application switching and task management shortcuts available.
	if (key == 9 && modifiers&1 != 0) || (key == 115 && modifiers&1 != 0) || (key == 0x57 && modifiers == 2) {
		return "", 0, 0, fmt.Errorf("эта комбинация занята системой; выберите другую")
	}
	ordered := []string{}
	for _, m := range []struct {
		name string
		bit  int
	}{{"Ctrl", 2}, {"Alt", 1}, {"Shift", 4}} {
		if modifiers&m.bit != 0 {
			ordered = append(ordered, m.name)
		}
	}
	return strings.Join(append(ordered, name), " + "), key, modifiers, nil
}
