//go:build windows

package main

// Pill extras: hover tooltip and right-click menu (window thread).

import (
	"strings"
	"unsafe"
)

var (
	pCreatePopupMenu = user32.NewProc("CreatePopupMenu")
	pAppendMenuW     = user32.NewProc("AppendMenuW")
	pTrackPopupMenu  = user32.NewProc("TrackPopupMenu")
	pDestroyMenu     = user32.NewProc("DestroyMenu")
	pSendMessageW    = user32.NewProc("SendMessageW")
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
	ttmDelTool        = 0x0400 + 51
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
	Rect     rect
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
		pDestroyWindow.Call(p.tip.hwnd)
		p.tip = tooltipState{}
	}
	t, _, _ := pCreateWindowExW.Call(0x8 /*WS_EX_TOPMOST*/, uintptr(unsafe.Pointer(utf16("tooltips_class32"))), 0,
		wsPopup|ttsAlwaysTip|ttsNoPrefix, 0, 0, 0, 0, host, 0, inst, 0)
	if t == 0 {
		return
	}
	p.tip = tooltipState{hwnd: t, owner: h}
	ti := toolInfo{Size: uint32(unsafe.Sizeof(toolInfo{})), Flags: ttfIDIsHwnd | ttfSubclass, Hwnd: h, ID: h, Inst: inst, Text: utf16(" ")}
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
		text = "AI Usage"
	}
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\n", "\r\n")
	if text == p.tip.text {
		return
	}
	p.tip.text = text
	ti := toolInfo{Size: uint32(unsafe.Sizeof(toolInfo{})), Flags: ttfIDIsHwnd, Hwnd: p.tip.owner, ID: p.tip.owner, Text: utf16(text)}
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
		pAppendMenuW.Call(m, mfString, uintptr(id), uintptr(unsafe.Pointer(utf16(s))))
	}
	add(MenuOpen, "Open panel")
	add(MenuRefresh, "Refresh now")
	add(MenuSettings, "Settings…")
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(MenuResetPosition, "Reset position")
	pAppendMenuW.Call(m, mfSeparator, 0, 0)
	add(MenuQuit, "Quit AI Usage")

	var pt point
	pGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	// Standard dance so the menu closes when clicking elsewhere.
	pSetForegroundWindow.Call(host)
	cmd, _, _ := pTrackPopupMenu.Call(m, tpmReturnCmd|tpmRightButton|tpmBottomAlign, uintptr(pt.X), uintptr(pt.Y), 0, host, 0)
	pPostMessageW.Call(host, wmNull, 0, 0)
	return int(cmd)
}
