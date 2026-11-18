package prose

import (
	"regexp"
	"strconv"
	"strings"
)

func itoaN(n int) string { return strconv.Itoa(n) }

// tok is one alphabetic word with its byte offset in the sentence and the
// byte that follows it (0 at the end), so a caller sees punctuation breaks.
type tok struct {
	w     string // lower case
	raw   string
	off   int
	end   int
	after byte
}

var tokRe = regexp.MustCompile(`[A-Za-z][A-Za-z'’\-]*`)

func tokenize(s string) []tok {
	var out []tok
	for _, m := range tokRe.FindAllStringIndex(s, -1) {
		t := tok{raw: s[m[0]:m[1]], off: m[0], end: m[1]}
		t.w = strings.ToLower(t.raw)
		if m[1] < len(s) {
			t.after = s[m[1]]
		}
		out = append(out, t)
	}
	return out
}

func set(words string) map[string]bool {
	m := map[string]bool{}
	for _, w := range strings.Fields(words) {
		m[w] = true
	}
	return m
}

var irregularParticiples = set(`been done made taken given seen gone written shown become found built run begun known left kept held brought thought told sent read set put got gotten lost met paid said sold stood understood won broken chosen driven fallen forgotten hidden spoken stolen thrown worn`)

func isParticiple(w string) bool {
	if irregularParticiples[w] {
		return true
	}
	return len(w) > 4 && strings.HasSuffix(w, "ed") && !strings.HasSuffix(w, "eed")
}

var perfectAdverbs = set(`not never also already just still now only always`)

// checkPerfect flags has/have/had + participle.
func checkPerfect(toks []tok, base int, add addFn) {
	for i, t := range toks {
		if t.w != "has" && t.w != "have" && t.w != "had" {
			continue
		}
		j := i + 1
		for j < len(toks) && perfectAdverbs[toks[j].w] && toks[j-1].after == ' ' {
			j++
		}
		if j < len(toks) && toks[j-1].after == ' ' && isParticiple(toks[j].w) {
			add(RulePerfectTense, t.raw+" "+toks[j].raw, "Perfect tense: use simple past or present.", base+t.off)
		}
	}
}

var beForms = set(`is are was were be been being am`)

// Transitive action verbs whose participle after "be" is a real passive.
// Adjectives such as "required" or "based" are left out on purpose.
var actionParticiples = set(`created written removed deleted added called returned stored read generated parsed passed sent used checked updated changed fixed merged rejected applied saved blocked printed shown loaded built found made taken given done run set put sorted copied moved renamed replaced recorded logged raised thrown emitted rendered resolved triggered handled written dropped skipped marked ignored ran`)

// checkActive flags be + participle when the participle is a known action verb
// or an agent follows with "by".
func checkActive(toks []tok, base int, add addFn) {
	for i, t := range toks {
		if !beForms[t.w] {
			continue
		}
		j := i + 1
		for j < len(toks) && perfectAdverbs[toks[j].w] && toks[j-1].after == ' ' {
			j++
		}
		if j >= len(toks) || toks[j-1].after != ' ' || !isParticiple(toks[j].w) {
			continue
		}
		by := j+1 < len(toks) && toks[j+1].w == "by" && toks[j].after == ' '
		if by || actionParticiples[toks[j].w] {
			add(RuleActiveVoice, t.raw+" "+toks[j].raw, "Passive voice: name who acts.", base+t.off)
		}
	}
}

var ingDeterminers = set(`the a an this that these those its their our your my no any each every`)
var ingFollowers = set(`of is are was were will can must should fails runs happens works`)

// checkIng flags "the parsing of" and "the handling is": a determiner, an -ing
// word, then "of" or a verb. Adjectives such as "the following" never match.
func checkIng(toks []tok, base int, add addFn) {
	for i := 0; i+2 < len(toks); i++ {
		d, w, f := toks[i], toks[i+1], toks[i+2]
		if !ingDeterminers[d.w] || d.after != ' ' || w.after != ' ' || len(w.w) < 6 || !strings.HasSuffix(w.w, "ing") {
			continue
		}
		if ingFollowers[f.w] {
			add(RuleIngWord, d.raw+" "+w.raw, "-ing word used as a noun: use a verb or a plain noun.", base+d.off)
		}
	}
}

var stepStarters = set(`the a an this that these those it its we they there all each every some no my our your their he she you i when if after before because so then also and but or not now new more only both once while`)
var nounSuffixes = []string{"tion", "sion", "ness", "ity", "ance", "ence"}

// checkStepVerb flags a step whose first word cannot be a command.
func checkStepVerb(toks []tok, base int, add addFn) {
	if len(toks) == 0 {
		return
	}
	t := toks[0]
	bad := stepStarters[t.w]
	if len(t.w) > 4 && (strings.HasSuffix(t.w, "ing") || isParticiple(t.w) && !irregularParticiples[t.w]) {
		bad = true
	}
	for _, s := range nounSuffixes {
		if len(t.w) > 6 && strings.HasSuffix(t.w, s) {
			bad = true
		}
	}
	if bad {
		add(RuleStepVerb, t.raw, "A step starts with a verb: write a command.", base+t.off)
	}
}
