package launcher

import "syscall"
import "unsafe"

var userUILanguage = syscall.NewLazyDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")
var localeName = syscall.NewLazyDLL("kernel32.dll").NewProc("LCIDToLocaleName")

func SystemLanguage() string {
	id, _, _ := userUILanguage.Call()
	var name [85]uint16
	if count, _, _ := localeName.Call(id, uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)), 0); count > 0 {
		return InterfaceLanguage("", syscall.UTF16ToString(name[:]))
	}
	return "en"
}
