//go:build !windows

package launcher

import "os"
import "strings"

func SystemLanguage() string {
	language, _, _ := strings.Cut(os.Getenv("LANG"), ".")
	return InterfaceLanguage("", strings.ReplaceAll(language, "_", "-"))
}
