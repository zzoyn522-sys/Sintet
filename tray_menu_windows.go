//go:build windows

package main

import (
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var trayGDI32 = windows.NewLazySystemDLL("gdi32.dll")
var trayShcore = windows.NewLazySystemDLL("shcore.dll")
var trayDWM = windows.NewLazySystemDLL("dwmapi.dll")

type trayMenuItem struct {
	action string
	label  string
	icon   string
}

var trayMenuItems = [...]trayMenuItem{
	{action: "open", label: "Открыть Sintet", icon: "\uE8A7"},
	{action: "friends", label: "Друзья и лобби", icon: "\uE716"},
	{action: "workshop", label: "Мастерская", icon: "\uE80A"},
	{action: "settings", label: "Настройки", icon: "\uE713"},
	{action: "folder", label: "Папка Minecraft", icon: "\uE8B7"},
	{action: "logs", label: "Журнал игры", icon: "\uE9F9"},
	{action: "quit", label: "Выйти", icon: "\uE8BB"},
}

var trayLabels = map[string][7]string{
	"en": {"Open Sintet", "Friends and lobby", "Workshop", "Settings", "Minecraft folder", "Game log", "Quit"},
	"uk": {"Відкрити Sintet", "Друзі й лобі", "Майстерня", "Налаштування", "Папка Minecraft", "Журнал гри", "Вийти"},
	"pt": {"Abrir Sintet", "Amigos e sala", "Oficina", "Definições", "Pasta do Minecraft", "Registo do jogo", "Sair"},
	"es": {"Abrir Sintet", "Amigos y sala", "Taller", "Ajustes", "Carpeta de Minecraft", "Registro del juego", "Salir"},
}

func (m *trayMenu) label(index int) string {
	if labels, ok := trayLabels[m.language]; ok {
		return labels[index]
	}
	return trayMenuItems[index].label
}

type trayRect struct{ Left, Top, Right, Bottom int32 }

type trayIconIdentifier struct {
	size uint32
	hwnd uintptr
	id   uint32
	guid windows.GUID
}

type trayPopupParams struct {
	size    uint32
	exclude trayRect
}

type trayMenuInfo struct {
	size, mask, style, maxHeight uint32
	background                   uintptr
	helpID                       uint32
	data                         uintptr
}

type trayMenuItemInfo struct {
	size, mask, kind, state, id uint32
	submenu, checked, unchecked uintptr
	data                        uintptr
	label                       *uint16
	labelLength                 uint32
	bitmap                      uintptr
}

type trayMeasureItem struct {
	kind, control, id, width, height uint32
	data                             uintptr
}

type trayDrawItem struct {
	kind, control, id, action, state uint32
	window, dc                       uintptr
	rect                             trayRect
	data                             uintptr
}

type trayMenu struct {
	language             string
	tracking             bool
	styledWindow         uintptr
	dpi                  int32
	font, symbols        uintptr
	background, selected uintptr
}

func (m *trayMenu) px(value int32) int32 { return (value*m.dpi + 48) / 96 }

func (m *trayMenu) measure(item *trayMeasureItem) bool {
	if item.kind != 1 || item.id == 0 || item.id > uint32(len(trayMenuItems)) {
		return false
	}
	item.width = uint32(m.px(173))
	item.height = uint32(m.px(32))
	return true
}

func (m *trayMenu) styleWindow(owner, popup uintptr) {
	if m.font == 0 || m.background == 0 || m.selected == 0 || popup == 0 || popup == owner || popup == m.styledWindow {
		return
	}
	ownerThread, _, _ := trayUser32.NewProc("GetWindowThreadProcessId").Call(owner, 0)
	popupThread, _, _ := trayUser32.NewProc("GetWindowThreadProcessId").Call(popup, 0)
	if popupThread == 0 || popupThread != ownerThread {
		return
	}
	var window, client trayRect
	var origin trayPoint
	outer, _, _ := trayUser32.NewProc("GetWindowRect").Call(popup, uintptr(unsafe.Pointer(&window)))
	inner, _, _ := trayUser32.NewProc("GetClientRect").Call(popup, uintptr(unsafe.Pointer(&client)))
	mapped, _, _ := trayUser32.NewProc("ClientToScreen").Call(popup, uintptr(unsafe.Pointer(&origin)))
	if outer == 0 || inner == 0 || mapped == 0 {
		return
	}
	// Clip the menu's system frame while leaving its native input loop active.
	inset := m.px(2)
	left := max(inset, origin.X-window.Left)
	top := max(inset, origin.Y-window.Top)
	right := min(window.Right-window.Left-inset, origin.X-window.Left+client.Right)
	bottom := min(window.Bottom-window.Top-inset, origin.Y-window.Top+client.Bottom)
	if right <= left || bottom <= top {
		return
	}
	var region uintptr
	if supportsRoundedWindow() {
		border := uint32(0xfffffffe) // DWMWA_COLOR_NONE
		trayDWM.NewProc("DwmSetWindowAttribute").Call(popup, 34, uintptr(unsafe.Pointer(&border)), unsafe.Sizeof(border))
		diameter := uintptr(m.px(20))
		region, _, _ = trayGDI32.NewProc("CreateRoundRectRgn").Call(uintptr(left), uintptr(top), uintptr(right), uintptr(bottom), diameter, diameter)
	} else {
		region, _, _ = trayGDI32.NewProc("CreateRectRgn").Call(uintptr(left), uintptr(top), uintptr(right), uintptr(bottom))
	}
	if region == 0 {
		return
	}
	ok, _, _ := trayUser32.NewProc("SetWindowRgn").Call(popup, region, 1)
	if ok == 0 {
		trayGDI32.NewProc("DeleteObject").Call(region)
		return
	}
	m.styledWindow = popup
}

func (m *trayMenu) paint(item *trayDrawItem) bool {
	if item.kind != 1 || item.id == 0 || item.id > uint32(len(trayMenuItems)) {
		return false
	}
	row := trayMenuItems[item.id-1]
	rect := item.rect
	dc := item.dc
	trayUser32.NewProc("FillRect").Call(dc, uintptr(unsafe.Pointer(&rect)), m.background)
	if item.state&1 != 0 {
		region, _, _ := trayGDI32.NewProc("CreateRoundRectRgn").Call(
			uintptr(rect.Left+m.px(3)), uintptr(rect.Top+m.px(1)), uintptr(rect.Right-m.px(3)), uintptr(rect.Bottom-m.px(1)),
			uintptr(m.px(12)), uintptr(m.px(12)))
		if region != 0 {
			trayGDI32.NewProc("FillRgn").Call(dc, region, m.selected)
			trayGDI32.NewProc("DeleteObject").Call(region)
		}
	}
	saved, _, _ := trayGDI32.NewProc("SaveDC").Call(dc)
	defer trayGDI32.NewProc("RestoreDC").Call(dc, saved)
	trayGDI32.NewProc("SetBkMode").Call(dc, 1)
	draw := func(label string, font, color uintptr, bounds trayRect) {
		trayGDI32.NewProc("SelectObject").Call(dc, font)
		trayGDI32.NewProc("SetTextColor").Call(dc, color)
		value, _ := windows.UTF16PtrFromString(label)
		trayUser32.NewProc("DrawTextW").Call(dc, uintptr(unsafe.Pointer(value)), ^uintptr(0), uintptr(unsafe.Pointer(&bounds)), 0x20|0x4|0x800|0x8000)
	}
	if m.symbols != 0 {
		draw(row.icon, m.symbols, 0xa8a8a8, trayRect{rect.Left + m.px(12), rect.Top, rect.Left + m.px(31), rect.Bottom})
	}
	draw(m.label(int(item.id)-1), m.font, 0xebebeb, trayRect{rect.Left + m.px(34), rect.Top, rect.Right - m.px(10), rect.Bottom})
	return true
}

func (m *trayMenu) show(hwnd uintptr, point trayPoint) string {
	popup, _, _ := trayUser32.NewProc("CreatePopupMenu").Call()
	if popup == 0 {
		return ""
	}
	id := trayIconIdentifier{hwnd: hwnd, id: 1}
	id.size = uint32(unsafe.Sizeof(id))
	var params trayPopupParams
	params.size = uint32(unsafe.Sizeof(params))
	placement, _, _ := trayShell32.NewProc("Shell_NotifyIconGetRect").Call(uintptr(unsafe.Pointer(&id)), uintptr(unsafe.Pointer(&params.exclude)))
	var paramPtr *trayPopupParams
	if placement == 0 {
		paramPtr = &params
		point = trayPoint{params.exclude.Right, params.exclude.Top}
	}

	coordinates := uintptr(uint32(point.X)) | uintptr(uint32(point.Y))<<32
	monitor, _, _ := trayUser32.NewProc("MonitorFromPoint").Call(coordinates, 2)
	var dpiX, dpiY uint32
	dpiResult, _, _ := trayShcore.NewProc("GetDpiForMonitor").Call(monitor, 0, uintptr(unsafe.Pointer(&dpiX)), uintptr(unsafe.Pointer(&dpiY)))
	if dpiResult != 0 || dpiX == 0 {
		dpiX = 96
	}
	*m = trayMenu{dpi: int32(dpiX), language: prelaunchLanguage(filepath.Join(documentsFolder(), "Sintet"))}
	defer func() {
		if m.styledWindow != 0 {
			trayUser32.NewProc("SetWindowRgn").Call(m.styledWindow, 0, 0)
		}
		trayUser32.NewProc("DestroyMenu").Call(popup)
		for _, object := range []uintptr{m.font, m.symbols, m.background, m.selected} {
			if object != 0 {
				trayGDI32.NewProc("DeleteObject").Call(object)
			}
		}
		*m = trayMenu{}
	}()
	font := func(face string, size, weight int32) uintptr {
		name, _ := windows.UTF16PtrFromString(face)
		handle, _, _ := trayGDI32.NewProc("CreateFontW").Call(uintptr(-m.px(size)), 0, 0, 0, uintptr(weight), 0, 0, 0, 1, 0, 0, 5, 0, uintptr(unsafe.Pointer(name)))
		return handle
	}
	m.font = font("Segoe UI", 16, 550)
	m.symbols = font("Segoe MDL2 Assets", 15, 400)
	m.background, _, _ = trayGDI32.NewProc("CreateSolidBrush").Call(0x191919)
	m.selected, _, _ = trayGDI32.NewProc("CreateSolidBrush").Call(0x2b2b2b)
	ownerDraw := m.font != 0 && m.background != 0 && m.selected != 0
	if ownerDraw {
		info := trayMenuInfo{mask: 0x2, background: m.background}
		info.size = uint32(unsafe.Sizeof(info))
		trayUser32.NewProc("SetMenuInfo").Call(popup, uintptr(unsafe.Pointer(&info)))
	}
	for index := range trayMenuItems {
		label, _ := windows.UTF16PtrFromString(m.label(index))
		info := trayMenuItemInfo{mask: 0x100 | 0x40 | 0x2, id: uint32(index + 1), label: label}
		info.size = uint32(unsafe.Sizeof(info))
		if ownerDraw {
			info.kind = 0x100 // MFT_OWNERDRAW
		}
		ok, _, _ := trayUser32.NewProc("InsertMenuItemW").Call(popup, uintptr(index), 1, uintptr(unsafe.Pointer(&info)))
		if ok == 0 {
			return ""
		}
	}

	// The shell recognizes the native menu while it is tracking the pointer.
	m.tracking = true
	trayUser32.NewProc("SetForegroundWindow").Call(hwnd)
	selected, _, _ := trayUser32.NewProc("TrackPopupMenuEx").Call(popup, 0x100|0x40|0x20|0x8|0x2,
		uintptr(point.X), uintptr(point.Y), hwnd, uintptr(unsafe.Pointer(paramPtr)))
	trayUser32.NewProc("PostMessageW").Call(hwnd, 0, 0, 0)
	if selected == 0 || selected > uintptr(len(trayMenuItems)) {
		return ""
	}
	return trayMenuItems[selected-1].action
}

func trayMessagePoint(value uintptr) trayPoint {
	return trayPoint{int32(int16(value)), int32(int16(value >> 16))}
}
