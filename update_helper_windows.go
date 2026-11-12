//go:build windows

package main

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

func updateHealthMarkerFromArgs(args []string) string {
	for i := 0; i+1 < len(args); i++ {
		if args[i] == "--sintet-update-health" {
			return args[i+1]
		}
	}
	return ""
}

func runUpdateHelper(args []string) int {
	if len(args) != 4 {
		return 2
	}
	target, staged, pidText, marker := args[0], args[1], args[2], args[3]
	self, _ := os.Executable()
	if self != "" {
		defer scheduleUpdateHelperCleanup(self)
	}
	defer os.Remove(staged)
	pid, err := strconv.ParseUint(pidText, 10, 32)
	if err != nil {
		return 2
	}
	parent, err := windows.OpenProcess(windows.SYNCHRONIZE, false, uint32(pid))
	if err == nil {
		_, _ = windows.WaitForSingleObject(parent, windows.INFINITE)
		_ = windows.CloseHandle(parent)
	}
	backup := target + ".sintet-previous"
	_ = os.Remove(backup)
	if err = os.Rename(target, backup); err != nil {
		return 3
	}
	if err = os.Rename(staged, target); err != nil {
		_ = os.Rename(backup, target)
		return 4
	}
	process := exec.Command(target, "--sintet-update-health", marker)
	err = process.Start()
	if err != nil {
		return rollbackUpdate(target, backup, marker, 5)
	}
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		if _, err = os.Stat(marker); err == nil {
			_ = os.Remove(marker)
			_ = os.Remove(backup)
			return 0
		}
		time.Sleep(250 * time.Millisecond)
	}
	terminateProcess(uint32(process.Process.Pid))
	return rollbackUpdate(target, backup, marker, 6)
}

func scheduleUpdateHelperCleanup(path string) {
	command := "Start-Sleep -Milliseconds 1200; Remove-Item -LiteralPath '" + strings.ReplaceAll(path, "'", "''") + "' -Force -ErrorAction SilentlyContinue"
	wide := utf16.Encode([]rune(command))
	bytes := make([]byte, len(wide)*2)
	for i, unit := range wide {
		binary.LittleEndian.PutUint16(bytes[i*2:], unit)
	}
	encoded := base64.StdEncoding.EncodeToString(bytes)
	powershell := filepath.Join(os.Getenv("WINDIR"), "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
	process := exec.Command(powershell, "-NoLogo", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-EncodedCommand", encoded)
	process.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
	_ = process.Start()
}

func rollbackUpdate(target, backup, marker string, code int) int {
	failed := target + ".sintet-failed"
	_ = os.Remove(failed)
	if err := os.Rename(target, failed); err != nil {
		return code
	}
	if err := os.Rename(backup, target); err != nil {
		_ = os.Rename(failed, target)
		return code
	}
	_ = os.Remove(marker)
	_ = exec.Command(target).Start()
	_ = os.Remove(failed)
	return code
}

func terminateProcess(pid uint32) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	_ = windows.TerminateProcess(h, 1)
	_, _ = windows.WaitForSingleObject(h, 10_000)
}
