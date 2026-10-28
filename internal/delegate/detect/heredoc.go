package detect

import "strings"

// removeHeredocs drops every terminated heredoc, from its << to the end of its
// closing word; the newline after the closing word stays. It is the mod's
//
//	/<<-?\s*(['"]?)(\w+)\1[^\n]*\n[\s\S]*?\n\s*\2(?=\n|$)/g
//
// written out, since RE2 has no backreference: the opening may be quoted, the
// closing word may be indented, and as in the regex a shorter opening word is
// tried when the whole one never closes.
func removeHeredocs(s string) string {
	if !strings.Contains(s, "<<") {
		return s
	}
	var b strings.Builder
	i := 0
	for {
		j := strings.Index(s[i:], "<<")
		if j < 0 {
			b.WriteString(s[i:])
			return b.String()
		}
		j += i
		if end, ok := heredocEnd(s, j); ok {
			b.WriteString(s[i:j])
			i = end
			continue
		}
		b.WriteString(s[i : j+1])
		i = j + 1
	}
}

// heredocEnd is the end of the heredoc whose << is at s[at:], if it closes.
func heredocEnd(s string, at int) (int, bool) {
	k := at + 2
	if k < len(s) && s[k] == '-' {
		k++
	}
	k = skipSpace(s, k)
	var quote byte
	if k < len(s) && (s[k] == '\'' || s[k] == '"') {
		quote = s[k]
		k++
	}
	n := wordLen(s, k)
	for ; n > 0; n-- {
		word, after := s[k:k+n], k+n
		if quote != 0 {
			if after >= len(s) || s[after] != quote {
				return 0, false // only the whole word can meet the closing quote
			}
			after++
		}
		if end, ok := closing(s, after, word); ok {
			return end, true
		}
	}
	return 0, false
}

// closing finds the end of the first line after the opening line (which runs
// from `from` to its newline) that holds, behind whitespace, exactly word.
func closing(s string, from int, word string) (int, bool) {
	nl := strings.IndexByte(s[from:], '\n')
	if nl < 0 {
		return 0, false
	}
	for t := from + nl + 1; ; {
		next := strings.IndexByte(s[t:], '\n')
		if next < 0 {
			return 0, false
		}
		t += next
		w := skipSpace(s, t+1)
		if strings.HasPrefix(s[w:], word) && (w+len(word) == len(s) || s[w+len(word)] == '\n') {
			return w + len(word), true
		}
		t++
	}
}

// skipSpace skips JavaScript-\s-style whitespace (newlines included).
func skipSpace(s string, i int) int {
	for i < len(s) && strings.IndexByte(" \t\n\r\f\v", s[i]) >= 0 {
		i++
	}
	return i
}

// wordLen is the length of the \w run at s[i:].
func wordLen(s string, i int) int {
	n := 0
	for i+n < len(s) && isWordByte(s[i+n]) {
		n++
	}
	return n
}

func isWordByte(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}
