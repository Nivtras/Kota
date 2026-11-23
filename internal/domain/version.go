package domain

import (
	"strconv"
	"strings"
)

// NewerVersion reports whether tag (e.g. "v1.2.0") is newer than cur
// ("1.1.0"). Pre-release suffixes are ignored, so "v1.2.0-beta" is not
// newer than "1.2.0".
func NewerVersion(tag, cur string) bool {
	parse := func(s string) [3]int {
		var v [3]int
		s = strings.TrimPrefix(strings.TrimSpace(s), "v")
		s, _, _ = strings.Cut(s, "-")
		for i, part := range strings.SplitN(s, ".", 3) {
			v[i], _ = strconv.Atoi(part)
		}
		return v
	}
	a, b := parse(tag), parse(cur)
	for i := range a {
		if a[i] != b[i] {
			return a[i] > b[i]
		}
	}
	return false
}
