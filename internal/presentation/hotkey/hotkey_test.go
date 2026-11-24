//go:build windows

package hotkey

import "testing"

func TestParse(t *testing.T) {
	for _, c := range []struct {
		spec     string
		mods, vk uint32
		ok       bool
	}{
		{"Win+Shift+K", modWin | modShift, 'K', true},
		{"ctrl+alt+space", modControl | modAlt, 0x20, true},
		{"Alt+1", modAlt, '1', true},
		{"K", 0, 0, false},
		{"Hyper+K", 0, 0, false},
		{"Ctrl+F13", 0, 0, false},
	} {
		m, v, err := Parse(c.spec)
		if (err == nil) != c.ok || (c.ok && (m != c.mods || v != c.vk)) {
			t.Errorf("Parse(%q) = %x %x %v", c.spec, m, v, err)
		}
	}
}
