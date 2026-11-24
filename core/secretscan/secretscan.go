// Package secretscan holds the one list of credential patterns in wipnote.
// The delegate run store masks matches in free text (Redact). Scratch capture
// refuses to copy a file that holds one (Match).
package secretscan

import "regexp"

// Mask replaces a matched credential in Redact's output.
const Mask = "***"

// patterns find credentials in free text. They run on Go's RE2 engine, which
// is linear in the input: orch's backtracking regexes took seconds on 8000
// characters (orch-inventory section 7, issue R).
var patterns = []struct {
	re   *regexp.Regexp
	repl string
}{
	// key=value, key: value and "key": "value" for secret-sounding keys
	{regexp.MustCompile(`(?i)\b((?:api[_-]?)?key|token|secret|passw(?:or)?d|pwd|credentials?|auth(?:orization)?)("?\s*[:=]\s*"?)[^\s"',;&]+`), "${1}${2}" + Mask},
	{regexp.MustCompile(`(?i)\b(bearer|basic)\s+[A-Za-z0-9._~+/=-]{8,}`), "${1} " + Mask},
	{regexp.MustCompile(`\bsk-[A-Za-z0-9_-]{8,}`), Mask},
	{regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`), Mask},
	{regexp.MustCompile(`\bgithub_pat_[A-Za-z0-9_]{20,}`), Mask},
	{regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`), Mask},
	{regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`), Mask},
	{regexp.MustCompile(`\beyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{4,}`), Mask},
}

// Redact masks credentials in s. It is linear in len(s).
func Redact(s string) string {
	for _, p := range patterns {
		s = p.re.ReplaceAllString(s, p.repl)
	}
	return s
}

// Match reports whether b holds text that any pattern matches. It is linear
// in len(b) and never changes b.
func Match(b []byte) bool {
	for _, p := range patterns {
		if p.re.Match(b) {
			return true
		}
	}
	return false
}
