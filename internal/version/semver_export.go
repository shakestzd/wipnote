package version

import (
	"strconv"
	"strings"
)

// IsNewer reports whether latest is strictly newer than current.
func IsNewer(latest, current string) bool { return isNewer(latest, current) }

// Parseable reports whether v is a numeric major.minor.patch, optionally
// with a leading "v" and a "-" or "+" suffix on the patch.
func Parseable(v string) bool {
	parts := strings.SplitN(strings.TrimPrefix(strings.TrimSpace(v), "v"), ".", 3)
	if len(parts) < 3 {
		return false
	}
	patch, _, _ := strings.Cut(parts[2], "-")
	patch, _, _ = strings.Cut(patch, "+")
	for _, p := range []string{parts[0], parts[1], patch} {
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}
