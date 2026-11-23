//go:build windows

package main

// Native UI Automation (COM) lookup of the Windows 11 Start button. The Win11
// taskbar is XAML, so Start has no HWND; UI Automation is the supported way
// to find its on-screen rectangle. Vtable indices follow UIAutomationClient.h.

import (
	"errors"
	"kota/internal/infrastructure/win32"
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	oleaut32           = windows.NewLazySystemDLL("oleaut32.dll")
	pSysAllocString    = oleaut32.NewProc("SysAllocString")
	pSysFreeString     = oleaut32.NewProc("SysFreeString")
	clsidCUIAutomation = windows.GUID{Data1: 0xff48dba4, Data2: 0x60ef, Data3: 0x4201, Data4: [8]byte{0xaa, 0x87, 0x54, 0x10, 0x3e, 0xef, 0x59, 0x4e}}
	iidIUIAutomation   = windows.GUID{Data1: 0x30cbe57d, Data2: 0xd9d0, Data3: 0x452a, Data4: [8]byte{0xab, 0x13, 0x7a, 0xc5, 0xac, 0x48, 0x25, 0xee}}
	errStartNotFound   = errors.New("start button not found")
)

const (
	uiaAutomationIDProperty = 30011
	treeScopeDescendants    = 4
	vtBSTR                  = 8

	// IUIAutomation
	vtElementFromHandle       = 6
	vtCreatePropertyCondition = 23
	// IUIAutomationElement
	vtFindFirst                   = 5
	vtGetCurrentBoundingRectangle = 43
	vtRelease                     = 2
)

type variant struct {
	vt       uint16
	reserved [3]uint16
	val      uintptr
	_        uintptr
}

// uiaWorker owns one COM apartment on a locked OS thread.
type uiaWorker struct{ req chan uiaRequest }

type uiaRequest struct {
	tb    uintptr // taskbar window to search (primary or secondary)
	reply chan uiaResult
}

type uiaResult struct {
	left int32
	err  error
}

func newUIAWorker() *uiaWorker {
	w := &uiaWorker{req: make(chan uiaRequest)}
	go func() {
		runtime.LockOSThread()
		win32.CoInitializeEx.Call(0, 0) // COINIT_MULTITHREADED
		var automation unsafe.Pointer
		hr, _, _ := win32.CoCreateInstance.Call(
			uintptr(unsafe.Pointer(&clsidCUIAutomation)), 0, win32.ClsctxInprocServer,
			uintptr(unsafe.Pointer(&iidIUIAutomation)), uintptr(unsafe.Pointer(&automation)))
		for r := range w.req {
			if int32(hr) < 0 || automation == nil {
				r.reply <- uiaResult{err: errors.New("UI Automation unavailable")}
				continue
			}
			l, err := findStartLeft(automation, r.tb)
			r.reply <- uiaResult{l, err}
		}
	}()
	return w
}

// startLeft returns the left edge (physical px) of the Start button on the
// given taskbar; tb 0 = the primary taskbar.
func (w *uiaWorker) startLeft(tb uintptr) (int32, error) {
	reply := make(chan uiaResult, 1)
	w.req <- uiaRequest{tb, reply}
	r := <-reply
	return r.left, r.err
}

func findStartLeft(automation unsafe.Pointer, tb uintptr) (int32, error) {
	if tb == 0 {
		tb = win32.FindWindow("Shell_TrayWnd", "")
	}
	if tb == 0 {
		return 0, errStartNotFound
	}
	var root unsafe.Pointer
	if hr := win32.ComCall(automation, vtElementFromHandle, tb, uintptr(unsafe.Pointer(&root))); int32(hr) < 0 || root == nil {
		return 0, errStartNotFound
	}
	defer win32.ComRelease(root)

	name, _, _ := pSysAllocString.Call(uintptr(unsafe.Pointer(win32.UTF16Ptr("StartButton"))))
	defer pSysFreeString.Call(name)
	v := variant{vt: vtBSTR, val: name}
	var cond unsafe.Pointer
	// VARIANT is larger than 8 bytes, so the x64 ABI passes it by reference.
	if hr := win32.ComCall(automation, vtCreatePropertyCondition, uiaAutomationIDProperty, uintptr(unsafe.Pointer(&v)), uintptr(unsafe.Pointer(&cond))); int32(hr) < 0 || cond == nil {
		return 0, errStartNotFound
	}
	defer win32.ComRelease(cond)

	var el unsafe.Pointer
	if hr := win32.ComCall(root, vtFindFirst, treeScopeDescendants, uintptr(cond), uintptr(unsafe.Pointer(&el))); int32(hr) < 0 || el == nil {
		return 0, errStartNotFound
	}
	defer win32.ComRelease(el)

	var r win32.Rect
	if hr := win32.ComCall(el, vtGetCurrentBoundingRectangle, uintptr(unsafe.Pointer(&r))); int32(hr) < 0 {
		return 0, errStartNotFound
	}
	if r.R <= r.L {
		return 0, errStartNotFound
	}
	return r.L, nil
}
