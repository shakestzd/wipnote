package runstore

import (
	"strings"

	"github.com/shakestzd/wipnote/core/secretscan"
)

// shortMax is the longest event summary, in runes (orch's short rule).
const shortMax = 200

// Redact masks credentials in s. It is linear in len(s). The patterns live in
// core/secretscan, which scratch capture shares.
func Redact(s string) string {
	return secretscan.Redact(s)
}

// Short is the one rule for free text stored in an event: the first line,
// credentials masked, clipped to 200 runes.
func Short(s string) string {
	if i := strings.IndexAny(s, "\r\n"); i >= 0 {
		s = s[:i]
	}
	s = strings.TrimSpace(Redact(s))
	if r := []rune(s); len(r) > shortMax {
		s = string(r[:shortMax-1]) + "…"
	}
	return s
}
