package launcher

import (
	"context"
	"errors"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var errGameWindowNotReady = errors.New("окно Minecraft ещё не готово; закройте его вручную")

// Ask only this launcher's tracked game window to close. GLFW handles WM_CLOSE
// through Minecraft's normal shutdown path; no process is forcibly terminated.
func (s *Service) RequestGameClose() error {
	state := s.State()
	if !state.Running {
		return nil
	}
	if state.PID <= 0 {
		return errors.New("не найден процесс Minecraft; закройте игру вручную")
	}
	user := windows.NewLazySystemDLL("user32.dll")
	var target uintptr
	callback := syscall.NewCallback(func(hwnd, unused uintptr) uintptr {
		var pid uint32
		user.NewProc("GetWindowThreadProcessId").Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		visible, _, _ := user.NewProc("IsWindowVisible").Call(hwnd)
		if pid == uint32(state.PID) && visible != 0 {
			target = hwnd
			return 0
		}
		return 1
	})
	user.NewProc("EnumWindows").Call(callback, 0)
	if target == 0 {
		return errGameWindowNotReady
	}
	ok, _, err := user.NewProc("PostMessageW").Call(target, 0x10, 0, 0)
	if ok == 0 {
		return err
	}
	return nil
}

// CloseGame waits for a pending start to settle, requests a normal window
// close, then waits for the Java process and its shutdown hooks to finish.
func (s *Service) CloseGame(ctx context.Context) error {
	tick := time.NewTicker(250 * time.Millisecond)
	defer tick.Stop()
	requested := false
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		state := s.State()
		if !state.Busy && !state.Running {
			return nil
		}
		if state.Running && !requested {
			err := s.RequestGameClose()
			if err != nil && !errors.Is(err, errGameWindowNotReady) {
				return err
			}
			requested = err == nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-tick.C:
		}
	}
}
