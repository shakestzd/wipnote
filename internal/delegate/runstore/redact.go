package runstore

import (
	"regexp"
	"strings"
)

// shortMax is the longest event summary, in runes (orch's short rule).
const shortMax = 200

const mask = "***"

// secretPatterns mask credentials in free text. They run on Go's RE2 engine,
// which is linear in the input: orch's backtracking regexes took seconds on
// 8000 characters (orch-inventory section 7, issue R).
var secretPatterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// key=value, key: value and "key": "value" for secret-sounding keys
	{regexp.MustCompile(`(?i)\b((?:api[_-]?)?key|token|secret|passw(?:or)?d|pwd|credentials?|auth(?:orization)?)("?\s*[:=]\s*"?)[^\s"',;&]+`), "${1}${2}" + mask},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "${1} " + mask},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), mask},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`), mask},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), mask},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), mask},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), mask},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}`), mask},
}

// Redact masks credentials in s. It is linear in len(s).
func Redact(s string) string {
	for _, p := range secretPatterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
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
