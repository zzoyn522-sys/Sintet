package main

import (
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"unsafe"
)

func supportsRoundedWindow() bool {
	version := windows.RtlGetVersion()
	return roundedWindowForVersion(version.MajorVersion, version.BuildNumber)
}
func supportsNativeBackdrop() bool {
	version := windows.RtlGetVersion()
	return version.MajorVersion >= 10 && version.BuildNumber >= 22621
}

func roundedWindowForVersion(major, build uint32) bool { return major >= 10 && build >= 22000 }

func documentsFolder() string {
	path, e := windows.KnownFolderPath(windows.FOLDERID_Documents, 0)
	if e == nil {
		return path
	}
	home, e := os.UserHomeDir()
	if e != nil {
		panic(e)
	}
	return filepath.Join(home, "Documents")
}
func systemMemoryMB() int {
	var state struct {
		Length        uint32
		Load          uint32
		TotalPhys     uint64
		AvailPhys     uint64
		TotalPage     uint64
		AvailPage     uint64
		TotalVirtual  uint64
		AvailVirtual  uint64
		AvailExtended uint64
	}
	state.Length = uint32(unsafe.Sizeof(state))
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("GlobalMemoryStatusEx")
	ok, _, _ := proc.Call(uintptr(unsafe.Pointer(&state)))
	if ok == 0 {
		return 8192
	}
	return int(state.TotalPhys / (1024 * 1024))
}
