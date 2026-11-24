//go:build windows

package system

import "golang.org/x/sys/windows"

var pGetUserDefaultUILanguage = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetUserDefaultUILanguage")

// UILanguage is Windows' display language as a short code ("tr", "en", …);
// only the languages Kota knows are told apart.
func UILanguage() string {
	id, _, _ := pGetUserDefaultUILanguage.Call()
	switch id & 0x3ff { // primary language ID
	case 0x1f:
		return "tr"
	}
	return "en"
}
