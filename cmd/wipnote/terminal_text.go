package main

import (
	"strings"
	"unicode"
)

// stripTerminalControls removes what a terminal would act on from untrusted
// text (a work item title, a worker's question, an option, a note or an
// answer) before it is printed: ESC sequences (CSI, OSC, DCS, SOS, PM, APC
// and two-character escapes), their C1 forms, and every other C0 or C1
// control and DEL. Tab and newline are kept. cleanTitle builds on it.
func stripTerminalControls(s string) string {
	if !strings.ContainsFunc(s, unicode.IsControl) {
		return s
	}
	rs := []rune(s)
	var b strings.Builder
	for i := 0; i < len(rs); i++ {
		switch r := rs[i]; {
		case r == '\t' || r == '\n':
			b.WriteRune(r)
		case r == 0x1b:
			i = skipEscape(rs, i)
		case r == 0x9b: // C1 CSI
			i = skipCSI(rs, i+1)
		case r == 0x90 || r == 0x98 || r == 0x9d || r == 0x9e || r == 0x9f: // C1 DCS, SOS, OSC, PM, APC
			i = skipControlString(rs, i+1)
		case unicode.IsControl(r): // any other C0, DEL or C1: dropped
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// skipEscape returns the index of the last rune of the escape sequence that
// starts with the ESC at rs[i].
func skipEscape(rs []rune, i int) int {
	if i+1 >= len(rs) {
		return i
	}
	switch rs[i+1] {
	case '[':
		return skipCSI(rs, i+2)
	case ']', 'P', 'X', '^', '_':
		return skipControlString(rs, i+2)
	}
	return i + 1
}

// skipCSI returns the index of a CSI's final byte (0x40-0x7E), searched from
// j, or the last index when the sequence is unterminated.
func skipCSI(rs []rune, j int) int {
	for ; j < len(rs); j++ {
		if rs[j] >= 0x40 && rs[j] <= 0x7e {
			return j
		}
	}
	return len(rs) - 1
}

// skipControlString returns the index of the terminator of an OSC, DCS, SOS,
// PM or APC body (BEL, ESC \ or C1 ST), searched from j, or the last index
// when it is unterminated.
func skipControlString(rs []rune, j int) int {
	for ; j < len(rs); j++ {
		switch {
		case rs[j] == 0x07 || rs[j] == 0x9c:
			return j
		case rs[j] == 0x1b && j+1 < len(rs) && rs[j+1] == '\\':
			return j + 1
		}
	}
	return len(rs) - 1
}
