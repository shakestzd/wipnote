package render

import (
	"regexp"
	"strings"
)

// A small Markdown subset as HTML for run.html (the mod's runhtml.ts):
// headings, fences, quotes, lists, bold, italic and inline code, enough for
// what the filters and an answer write. Everything from the run is escaped
// first, so raw HTML in it is shown as text.

var (
	htmlEsc   = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", `"`, "&quot;")
	strongRe  = regexp.MustCompile(`\*\*([^*]+)\*\*`)
	emRe      = regexp.MustCompile(`(^|[^*])\*([^*\t\n\v\f\r \x{a0}\x{feff}\p{Z}][^*]*)\*`)
	headingRe = regexp.MustCompile(`^(#{1,6}) (.*)$`)
	listRe    = regexp.MustCompile(`^[-*] `)
	blockRe   = regexp.MustCompile(`^(#{1,6} |> |[-*] )`)
)

func escHTML(s string) string { return htmlEsc.Replace(s) }

// emphasis is text outside code spans: escaped, then bold and italic.
func emphasis(t string) string {
	return emRe.ReplaceAllString(strongRe.ReplaceAllString(escHTML(t), "<strong>$1</strong>"), "$1<em>$2</em>")
}

// codeSpans finds CommonMark code spans as runhtml.ts's CODE_SPAN regex
// does: a backtick run, then the first later run of exactly that length,
// with something between. Run starts are indexed by length, so the scan is
// linear but for a binary search per backtick.
func codeSpans(t string) [][2]int {
	type run struct{ at, n int }
	var runs []run
	for i := 0; i < len(t); {
		if t[i] != '`' {
			i++
			continue
		}
		j := i
		for j < len(t) && t[j] == '`' {
			j++
		}
		runs = append(runs, run{i, j - i})
		i = j
	}
	byLen := map[int][]int{}
	for _, r := range runs {
		byLen[r.n] = append(byLen[r.n], r.at)
	}
	var spans [][2]int
	from := 0
	for _, r := range runs {
		end := r.at + r.n
		for at := max(r.at, from); at < end; at++ {
			if k := firstAfter(byLen[end-at], end); k >= 0 {
				spans = append(spans, [2]int{at, k + end - at})
				from = k + end - at
				break
			}
		}
	}
	return spans
}

// firstAfter is the first start in sorted that is after pos, or -1.
func firstAfter(sorted []int, pos int) int {
	lo, hi := 0, len(sorted)
	for lo < hi {
		if mid := (lo + hi) / 2; sorted[mid] <= pos {
			lo = mid + 1
		} else {
			hi = mid
		}
	}
	if lo == len(sorted) {
		return -1
	}
	return sorted[lo]
}

// inline renders one line or paragraph: code spans cut out first, so `*`
// inside them stays as written.
func inline(t string) string {
	var b strings.Builder
	at := 0
	for _, s := range codeSpans(t) {
		if s[0] < at {
			continue
		}
		n := 0
		for s[0]+n < len(t) && t[s[0]+n] == '`' {
			n++
		}
		inner := t[s[0]+n : s[1]-n]
		if trimSpace(inner) != "" {
			inner = trimSpace(inner)
		}
		b.WriteString(emphasis(t[at:s[0]]) + "<code>" + escHTML(inner) + "</code>")
		at = s[1]
	}
	return b.String() + emphasis(t[at:])
}
