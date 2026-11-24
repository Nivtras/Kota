//go:build windows

package taskbar

// Pill extras: hover tooltip and right-click menu (window thread).

import (
	"kota/internal/i18n"
	"kota/internal/infrastructure/win32"
	"strings"
	"unsafe"
)

var (
	pCreatePopupMenu = win32.User32.NewProc("CreatePopupMenu")
	pAppendMenuW     = win32.User32.NewProc("AppendMenuW")
	pTrackPopupMenu  = win32.User32.NewProc("TrackPopupMenu")
	pDestroyMenu     = win32.User32.NewProc("DestroyMenu")
	pSendMessageW    = win32.User32.NewProc("SendMessageW")
)

const (
	wmRButtonUp = 0x0205
	wmNull      = 0x0000

	mfString       = 0x0000
	mfSeparator    = 0x0800
	tpmReturnCmd   = 0x0100
	tpmRightButton = 0x0002
	tpmBottomAlign = 0x0020

	ttsAlwaysTip      = 0x01
	ttsNoPrefix       = 0x02
	ttfIDIsHwnd       = 0x0001
	ttfSubclass       = 0x0010
	ttmAddToolW       = 0x0400 + 50
	ttmUpdateTipTextW = 0x0400 + 57
	ttmSetMaxTipWidth = 0x0400 + 24
)

// Menu commands.
const (
	MenuOpen = iota + 1
	MenuRefresh
	MenuSettings
	MenuResetPosition
	MenuQuit
)

type toolInfo struct {
	Size     uint32
	Flags    uint32
	Hwnd     uintptr
	ID       uintptr
	Rect     win32.Rect
	Inst     uintptr
	Text     *uint16
	LParam   uintptr
	Reserved uintptr
}

// tooltipState is a pill's tooltip (window thread only).
type tooltipState struct {
	hwnd  uintptr
	owner uintptr // pill window the tool is registered for
	text  string
}

// attachTooltip (re)creates the tooltip for the pill window h. The tooltip
// is owned by our hidden host window (a top-level window of ours), not by
// the taskbar.
func (p *pill) attachTooltip(host, h, inst uintptr) {
	if p.tip.hwnd != 0 {
		win32.DestroyWindow.Call(p.tip.hwnd)
		p.tip = tooltipState{}
	}
	t, _, _ := win32.CreateWindowExW.Call(0x8 /*WS_EX_TOPMOST*/, uintptr(unsafe.Pointer(win32.UTF16Ptr("tooltips_class32"))), 0,
		win32.WsPopup|ttsAlwaysTip|ttsNoPrefix, 0, 0, 0, 0, host, 0, inst, 0)
	if t == 0 {
		return
	}
	p.tip = tooltipState{hwnd: t, owner: h}
	ti := toolInfo{Size: uint32(unsafe.Sizeof(toolInfo{})), Flags: ttfIDIsHwnd | ttfSubclass, Hwnd: h, ID: h, Inst: inst, Text: win32.UTF16Ptr(" ")}
	pSendMessageW.Call(t, ttmAddToolW, 0, uintptr(unsafe.Pointer(&ti)))
	pSendMessageW.Call(t, ttmSetMaxTipWidth, 0, 420)
}

// updateTooltip sets the text from the pill rows.
func (p *pill) updateTooltip(rows []pillRow) {
	if p.tip.hwnd == 0 || p.tip.owner == 0 {
		return
	}
	var parts []string
	for _, r := range rows {
		if r.Tooltip != "" {
			parts = append(parts, r.Tooltip)
		}
	}
	text := strings.Join(parts, "\r\n\r\n")
	if text == "" {
		text = "Kota"
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	if text == p.tip.text {
		return
	}
	p.tip.text = text
	ti := toolInfo{Size: uint32(unsafe.Sizeof(toolInfo{})), Flags: ttfIDIsHwnd, Hwnd: p.tip.owner, ID: p.tip.owner, Text: win32.UTF16Ptr(text)}
	pSendMessageW.Call(p.tip.hwnd, ttmUpdateTipTextW, 0, uintptr(unsafe.Pointer(&ti)))
}

// showPillMenu shows the right-click menu at the cursor and returns the
// chosen command (0 = none). host must be a top-level window of ours.
func showPillMenu(host uintptr) int {
	m, _, _ := pCreatePopupMenu.Call()
	if m == 0 {
		return 0
	}
	defer pDestroyMenu.Call(m)
	add := func(id int, s string) {
		pAppendMenuW.Call(m, mfString, uintptr(id), uintptr(unsafe.Pointer(win32.UTF16Ptr(s))))
	}
	add(MenuOpen, i18n.T("Open panel"))
	add(MenuRefresh, i18n.T("Refresh now"))
	add(MenuSettings, i18n.T("Settings…"))
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(MenuResetPosition, i18n.T("Reset position"))
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(MenuQuit, i18n.T("Quit Kota"))

	var pt win32.Point
	win32.GetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Standard dance so the menu closes when clicking elsewhere.
	win32.SetForegroundWindow.Call(host)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton|tpmBottomAlign, uintptr(pt.X), uintptr(pt.Y), 0, host, 0)
	win32.PostMessageW.Call(host, wmNull, 0, 0)
	return int(cmd)
}
