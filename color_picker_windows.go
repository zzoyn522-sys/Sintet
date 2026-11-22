package main

import (
	"errors"
	"fmt"
	"syscall"
	"time"
	"unsafe"
)

var (
	colorPickerUser32 = syscall.NewLazyDLL("user32.dll")
	colorPickerGDI32  = syscall.NewLazyDLL("gdi32.dll")
	colorGetCursor    = colorPickerUser32.NewProc("GetCursorPos")
	colorGetKeyState  = colorPickerUser32.NewProc("GetAsyncKeyState")
	colorGetDC        = colorPickerUser32.NewProc("GetDC")
	colorReleaseDC    = colorPickerUser32.NewProc("ReleaseDC")
	colorGetPixel     = colorPickerGDI32.NewProc("GetPixel")
)

type colorPickerPoint struct {
	X int32
	Y int32
}

// PickScreenColor waits for a screen click and returns the pixel under the cursor.
func (a *App) PickScreenColor() (string, error) {
	if err := colorGetCursor.Find(); err != nil {
		return "", err
	}
	dc, _, _ := colorGetDC.Call(0)
	if dc == 0 {
		return "", errors.New("не удалось открыть экран для выбора цвета")
	}
	defer colorReleaseDC.Call(0, dc)

	started := time.Now()
	armed := false
	for time.Since(started) < 90*time.Second {
		if state, _, _ := colorGetKeyState.Call(0x1B); state&0x8000 != 0 {
			return "", errors.New("выбор цвета отменён")
		}
		state, _, _ := colorGetKeyState.Call(0x01)
		pressed := state&0x8000 != 0
		if !pressed {
			armed = true
		} else if armed {
			var point colorPickerPoint
			if ok, _, _ := colorGetCursor.Call(uintptr(unsafe.Pointer(&point))); ok == 0 {
				return "", errors.New("не удалось определить положение указателя")
			}
			pixel, _, _ := colorGetPixel.Call(dc, uintptr(int64(point.X)), uintptr(int64(point.Y)))
			if uint32(pixel) == 0xFFFFFFFF {
				return "", errors.New("не удалось прочитать цвет этого пикселя")
			}
			return fmt.Sprintf("#%02X%02X%02X", byte(pixel), byte(pixel>>8), byte(pixel>>16)), nil
		}
		time.Sleep(12 * time.Millisecond)
	}
	return "", errors.New("истекло время выбора цвета")
}
