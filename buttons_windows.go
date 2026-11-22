//go:build windows

package main

// "Buttons" taskbar style: each taskbar provider gets a real taskbar button,
// handled entirely by Windows (drag to reorder, never covered by Start or
// Search). The button shows the provider logo; Windows' own progress bar
// under it shows usage (green / yellow from 60% / red from 85%), and the
// hover text gives the details.
//
// Each button is an invisible window kept minimized: a click makes Windows
// ask WM_QUERYOPEN, which we refuse and open the panel instead. Every button
// has its own AppUserModelID so Windows doesn't merge them into one.

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	pSetWindowTextW              = user32.NewProc("SetWindowTextW")
	pSendMessageW                = user32.NewProc("SendMessageW")
	pCreateIconIndirect          = user32.NewProc("CreateIconIndirect")
	pDestroyIcon                 = user32.NewProc("DestroyIcon")
	pCreateBitmap                = gdi32.NewProc("CreateBitmap")
	pSHGetPropertyStoreForWindow = shell32.NewProc("SHGetPropertyStoreForWindow")

	clsidTaskbarList = windows.GUID{Data1: 0x56FDF344, Data2: 0xFD6D, Data3: 0x11d0, Data4: [8]byte{0x95, 0x8A, 0x00, 0x60, 0x97, 0xC9, 0xA0, 0x90}}
	iidTaskbarList3  = windows.GUID{Data1: 0xea1afb91, Data2: 0x9e28, Data3: 0x4b86, Data4: [8]byte{0x90, 0xe9, 0x9e, 0x9f, 0x8a, 0x5e, 0xef, 0xaf}}
	iidPropertyStore = windows.GUID{Data1: 0x886d8eeb, Data2: 0x8cf2, Data3: 0x4446, Data4: [8]byte{0x8d, 0x02, 0xcd, 0xba, 0x1d, 0xbd, 0xcf, 0x99}}
	pkeyAppUserModel = propertyKey{FmtID: windows.GUID{Data1: 0x9F4C2855, Data2: 0x9F79, Data3: 0x4B39, Data4: [8]byte{0xA8, 0xD0, 0xE1, 0xD4, 0x2D, 0xE1, 0xD5, 0xF3}}, PID: 5}

	buttonProcCallback = syscall.NewCallback(buttonProc)
)

const (
	wmSetIcon      = 0x0080
	wmQueryOpen    = 0x0013
	wmSysCommand   = 0x0112
	scClose        = 0xF060
	iconSmall      = 0
	iconBig        = 1
	swShowMinNoAct = 7
	wsOverlappedW  = 0x00CF0000
	wsExAppWindowB = 0x00040000

	tbpfNoProgress = 0x0
	tbpfNormal     = 0x2 // green
	tbpfError      = 0x4 // red
	tbpfPaused     = 0x8 // yellow

	// ITaskbarList3 vtable: IUnknown(3) + HrInit, AddTab, DeleteTab,
	// ActivateTab, SetActiveAlt, MarkFullscreenWindow, SetProgressValue, ...
	vtHrInit           = 3
	vtSetProgressValue = 9
	vtSetProgressState = 10
	// IPropertyStore: GetCount, GetAt, GetValue, SetValue, Commit
	vtPSSetValue = 6
	vtPSCommit   = 7

	vtLPWSTR = 31
)

type propertyKey struct {
	FmtID windows.GUID
	PID   uint32
}

type propVariant struct {
	VT       uint16
	_        [3]uint16
	Ptr      uintptr
	_padding uintptr
}

type iconInfo struct {
	FIcon    int32
	XHotspot uint32
	YHotspot uint32
	HbmMask  uintptr
	HbmColor uintptr
}

type taskButton struct {
	id    string
	hwnd  uintptr
	icons [2]uintptr
	title string
	ready bool // TaskbarButtonCreated received: progress calls work now
	state uintptr
	value uint64
	appID *uint16
}

// Button state lives on the pill's window thread.
type buttonSet struct {
	buttons       map[string]*taskButton
	taskbarList   unsafe.Pointer
	buttonCreated uint32
	onClick       func(id string)
	onClose       func()
	classReady    bool
}

var theButtons = &buttonSet{buttons: map[string]*taskButton{}}

func (bs *buttonSet) init(inst uintptr) {
	if bs.classReady {
		return
	}
	cls := utf16("AIUsageWidgetButton")
	pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wndClassEx{
		Size: uint32(unsafe.Sizeof(wndClassEx{})), WndProc: buttonProcCallback, Instance: inst, ClassName: cls,
	})))
	m, _, _ := pRegisterWindowMessageW.Call(uintptr(unsafe.Pointer(utf16("TaskbarButtonCreated"))))
	bs.buttonCreated = uint32(m)
	pCoInitializeEx.Call(0, 2) // COINIT_APARTMENTTHREADED, for ITaskbarList3
	pCoCreateInstance.Call(uintptr(unsafe.Pointer(&clsidTaskbarList)), 0, clsctxInprocServer,
		uintptr(unsafe.Pointer(&iidTaskbarList3)), uintptr(unsafe.Pointer(&bs.taskbarList)))
	if bs.taskbarList != nil {
		comCall(bs.taskbarList, vtHrInit)
	}
	bs.classReady = true
}

// sync makes the set of buttons match rows (window thread). Empty rows =
// no buttons (pill style).
func (bs *buttonSet) sync(inst uintptr, rows []pillRow, logos map[string][]byte) {
	bs.init(inst)
	want := map[string]bool{}
	for _, r := range rows {
		want[r.ID] = true
		b := bs.buttons[r.ID]
		if b == nil {
			b = bs.create(inst, r, logos[r.ID])
			if b == nil {
				continue
			}
			bs.buttons[r.ID] = b
		}
		bs.update(b, r)
	}
	for id, b := range bs.buttons {
		if !want[id] {
			pDestroyWindow.Call(b.hwnd)
			for _, ic := range b.icons {
				if ic != 0 {
					pDestroyIcon.Call(ic)
				}
			}
			delete(bs.buttons, id)
		}
	}
}

func (bs *buttonSet) create(inst uintptr, r pillRow, logo []byte) *taskButton {
	b := &taskButton{id: r.ID}
	h, _, _ := pCreateWindowExW.Call(wsExAppWindowB, uintptr(unsafe.Pointer(utf16("AIUsageWidgetButton"))),
		uintptr(unsafe.Pointer(utf16(r.Name))), wsOverlappedW, 0, 0, 1, 1, 0, 0, inst, 0)
	if h == 0 {
		dockLog("button: CreateWindowEx failed for %s", r.ID)
		return nil
	}
	b.hwnd = h
	// Own app ID → own taskbar button (not grouped with the other one).
	b.appID = utf16("AIUsageWidget." + r.ID)
	var ps unsafe.Pointer
	if hr, _, _ := pSHGetPropertyStoreForWindow.Call(h, uintptr(unsafe.Pointer(&iidPropertyStore)), uintptr(unsafe.Pointer(&ps))); int32(hr) >= 0 && ps != nil {
		pv := propVariant{VT: vtLPWSTR, Ptr: uintptr(unsafe.Pointer(b.appID))}
		comCall(ps, vtPSSetValue, uintptr(unsafe.Pointer(&pkeyAppUserModel)), uintptr(unsafe.Pointer(&pv)))
		comCall(ps, vtPSCommit)
		comRelease(ps)
	}
	if logo != nil {
		if img, err := png.Decode(bytes.NewReader(logo)); err == nil {
			b.icons[iconSmall] = makeIcon(img, 16)
			b.icons[iconBig] = makeIcon(img, 32)
		}
	}
	if b.icons[iconSmall] == 0 { // fall back to the app icon
		if exe, err := os.Executable(); err == nil {
			ic, _, _ := pExtractIconW.Call(0, uintptr(unsafe.Pointer(utf16(exe))), 0)
			b.icons[iconSmall], b.icons[iconBig] = ic, ic
		}
	}
	pSendMessageW.Call(h, wmSetIcon, iconSmall, b.icons[iconSmall])
	pSendMessageW.Call(h, wmSetIcon, iconBig, b.icons[iconBig])
	pShowWindow.Call(h, swShowMinNoAct)
	return b
}

func (bs *buttonSet) update(b *taskButton, r pillRow) {
	title := r.Name
	state, value := uintptr(tbpfNoProgress), uint64(0)
	if r.HasBar {
		title = fmt.Sprintf("%s · %d%% %s", r.Name, r.Value, r.Word)
		if r.Reset != "" {
			title += " · resets " + r.Reset
		}
		used := r.Fill
		if r.Word == "left" {
			used = 100 - r.Fill
		}
		switch {
		case used >= 85:
			state = tbpfError
		case used >= 60:
			state = tbpfPaused
		default:
			state = tbpfNormal
		}
		value = uint64(r.Fill + 0.5)
		if value == 0 {
			value = 1 // keep a sliver visible so the button reads as a meter
		}
	} else if r.NA != "" {
		title = r.Name + " · " + r.NA
	}
	if title != b.title {
		pSetWindowTextW.Call(b.hwnd, uintptr(unsafe.Pointer(utf16(title))))
		b.title = title
	}
	b.state, b.value = state, value
	bs.applyProgress(b)
}

func (bs *buttonSet) applyProgress(b *taskButton) {
	if !b.ready || bs.taskbarList == nil {
		return
	}
	comCall(bs.taskbarList, vtSetProgressState, b.hwnd, b.state)
	if b.state != tbpfNoProgress {
		comCall(bs.taskbarList, vtSetProgressValue, b.hwnd, uintptr(b.value), 100)
	}
}

func (bs *buttonSet) byHwnd(h uintptr) *taskButton {
	for _, b := range bs.buttons {
		if b.hwnd == h {
			return b
		}
	}
	return nil
}

func buttonProc(hwnd, m, wp, lp uintptr) uintptr {
	bs := theButtons
	switch {
	case bs.buttonCreated != 0 && uint32(m) == bs.buttonCreated:
		// (Re)created button, e.g. after an Explorer restart: progress again.
		if b := bs.byHwnd(hwnd); b != nil {
			b.ready = true
			bs.applyProgress(b)
		}
		return 0
	case m == wmQueryOpen:
		// The user clicked the button: stay minimized, open the panel.
		if b := bs.byHwnd(hwnd); b != nil && bs.onClick != nil {
			go bs.onClick(b.id)
		}
		return 0
	case m == wmSysCommand && wp&0xFFF0 == scClose:
		// "Close window" from the button's menu quits the widget.
		if bs.onClose != nil {
			go bs.onClose()
		}
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, m, wp, lp)
	return r
}

// makeIcon builds an HICON (straight alpha) from an image at size×size.
func makeIcon(img image.Image, size int) uintptr {
	bi := bitmapInfoHeader{Size: uint32(unsafe.Sizeof(bitmapInfoHeader{})), Width: int32(size), Height: -int32(size), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	color, _, _ := pCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bi)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if color == 0 || bits == nil {
		return 0
	}
	buf := unsafe.Slice((*byte)(bits), size*size*4)
	b := img.Bounds()
	sw, sh := float64(b.Dx())/float64(size), float64(b.Dy())/float64(size)
	for dy := 0; dy < size; dy++ {
		for dx := 0; dx < size; dx++ {
			x0, x1 := b.Min.X+int(float64(dx)*sw), b.Min.X+max(int(float64(dx+1)*sw), int(float64(dx)*sw)+1)
			y0, y1 := b.Min.Y+int(float64(dy)*sh), b.Min.Y+max(int(float64(dy+1)*sh), int(float64(dy)*sh)+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					cr, cg, cb, ca := img.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			if n == 0 || a == 0 {
				continue
			}
			i := (dy*size + dx) * 4
			// RGBA() is premultiplied; icons want straight alpha.
			buf[i+0] = byte(min(255, bl*255/a))
			buf[i+1] = byte(min(255, g*255/a))
			buf[i+2] = byte(min(255, r*255/a))
			buf[i+3] = byte(a / n >> 8)
		}
	}
	mask, _, _ := pCreateBitmap.Call(uintptr(size), uintptr(size), 1, 1, 0)
	ii := iconInfo{FIcon: 1, HbmMask: mask, HbmColor: color}
	icon, _, _ := pCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	pDeleteObject.Call(mask)
	pDeleteObject.Call(color)
	return icon
}
