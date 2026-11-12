package launcher

import "strings"

func InterfaceLanguage(preference, system string) string {
	if preference == "" {
		preference = system
	}
	code, _, _ := strings.Cut(strings.ToLower(preference), "-")
	switch code {
	case "ru", "uk", "pt", "es":
		return code
	}
	return "en"
}
