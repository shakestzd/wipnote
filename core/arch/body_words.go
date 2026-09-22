package arch

import (
	"strings"
	"unicode"
)

// overflowExcerptRunes caps the excerpt Validate quotes past the word limit.
const overflowExcerptRunes = 80

// bodyWords returns the tokens that count toward the MaxBodyWords budget:
// whitespace-separated words, minus inline code spans (`...`) and http(s)
// URL tokens, which are references rather than prose (GH-#170).
func bodyWords(s string) []string {
	var words []string
	for _, tok := range strings.FieldsFunc(stripCodeSpans(s), unicode.IsSpace) {
		if isURLToken(tok) {
			continue
		}
		words = append(words, tok)
	}
	return words
}

// countWords counts the prose words in s (see bodyWords).
func countWords(s string) int {
	return len(bodyWords(s))
}

// CountBodyWords is the exported form of countWords: the number of words a
// card body spends against MaxBodyWords.
func CountBodyWords(s string) int {
	return countWords(s)
}

// stripCodeSpans replaces every matched backtick span with a single space so
// its contents are neither counted nor merged into neighbouring words. An
// unmatched backtick is kept literally.
func stripCodeSpans(s string) string {
	var sb strings.Builder
	for {
		open := strings.IndexByte(s, '`')
		if open < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		closeIdx := strings.IndexByte(s[open+1:], '`')
		if closeIdx < 0 {
			sb.WriteString(s)
			return sb.String()
		}
		sb.WriteString(s[:open])
		sb.WriteByte(' ')
		s = s[open+1+closeIdx+1:]
	}
}

// isURLToken reports whether tok is an http(s) URL, ignoring any wrapping
// punctuation such as "(https://…)" or "<https://…>".
func isURLToken(tok string) bool {
	t := strings.ToLower(strings.TrimLeft(tok, "(<[\"'"))
	return strings.HasPrefix(t, "http://") || strings.HasPrefix(t, "https://")
}

// overflowExcerpt returns the counted words after the limit-th one, joined by
// spaces and cut to overflowExcerptRunes, so a rejection names the trailing
// text that pushed the body over the cap.
func overflowExcerpt(s string, limit int) string {
	words := bodyWords(s)
	if len(words) <= limit {
		return ""
	}
	excerpt := strings.Join(words[limit:], " ")
	if r := []rune(excerpt); len(r) > overflowExcerptRunes {
		excerpt = string(r[:overflowExcerptRunes]) + "…"
	}
	return excerpt
}

// TruncateBody trims body to at most MaxBodyWords counted words, cutting at
// the last sentence boundary (".", "!" or "?" followed by whitespace or the
// end of text) that fits under the cap. When no sentence boundary fits it
// cuts right after the MaxBodyWords-th word instead. The bool reports
// whether anything was removed.
func TruncateBody(body string) (string, bool) {
	if countWords(body) <= MaxBodyWords {
		return body, false
	}
	best := -1
	runes := []rune(body)
	for i, r := range runes {
		if r != '.' && r != '!' && r != '?' {
			continue
		}
		if i+1 < len(runes) && !unicode.IsSpace(runes[i+1]) {
			continue
		}
		if countWords(string(runes[:i+1])) <= MaxBodyWords {
			best = i + 1
		}
	}
	if best > 0 {
		return strings.TrimSpace(string(runes[:best])), true
	}
	return cutAfterWord(body, MaxBodyWords), true
}

// cutAfterWord returns body cut at the first whitespace after its n-th
// counted word. Whitespace inside a matched code span is never a cut point,
// so a span is kept or dropped whole.
func cutAfterWord(body string, n int) string {
	for i, r := range body {
		if !unicode.IsSpace(r) {
			continue
		}
		if strings.Count(body[:i], "`")%2 == 1 && strings.Contains(body[i:], "`") {
			continue // inside a code span
		}
		if countWords(body[:i]) >= n {
			return strings.TrimSpace(body[:i])
		}
	}
	return strings.TrimSpace(body)
}
