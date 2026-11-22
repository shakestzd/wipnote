package prose

import (
	"regexp"
	"strings"
)

// The vocab and filler patterns are ported from mods/wipnote-visuals/hooks/lint.ts.
var (
	vocabRe = regexp.MustCompile(`(?i)\b(?:delve|delves|delving|pivotal|testament|tapestry|underscore|underscores|underscoring|showcase|showcases|showcasing|vibrant|crucial|fostering|intricate|intricacies|enduring|garner|garners|garnering|resonate|resonates|resonating|invaluable|ignited|igniting|embark|embarks|embarking|realm|beacon|honing|cutting-edge|seamless|holistic|esteemed|renowned|passion|passionate|empower|empowers|empowering|transformative|comprehensive understanding|requisite skills|high-impact|make a difference in people['’]s lives|perfect blend|well-equipped|uniquely positioned|game-changer)\b` +
		`|(?:the |a |an )?(?:competitive|educational|academic|professional|digital|technological|regulatory|political|economic|healthcare|corporate|research|media|cultural)\s+landscape\b` +
		`|(?:academic|personal|professional|educational|career|life|spiritual|entrepreneurial|leadership|research|learning|growth)\s+journey\b` +
		`|journey (?:to become|towards|toward|of self-discovery|of growth|of learning)` +
		`|dynamic\s+(?:individual|leader|team|environment|person|professional|thinker|duo|place to work)\b` +
		`|leverag(?:ing|ed)\b|leverage\s+(?:my|our|your|this|that|these|those|the)\b`)
	openerRe = regexp.MustCompile(`(?i)^(?:great question|certainly!|absolutely!)`)
	fillerRe = regexp.MustCompile(`(?i)\b(?:in order to|due to the fact that|it is important to note that|in conclusion)\b`)
)

var fillerFix = map[string]string{
	"in order to":                  "to",
	"due to the fact that":         "because",
	"it is important to note that": "delete it",
	"in conclusion":                "delete it",
}

type addFn = func(rule, match, msg string, index int)

// checkTells flags AI-tell vocabulary and filler phrases.
func checkTells(t string, add addFn) {
	for _, m := range vocabRe.FindAllStringIndex(t, -1) {
		add(RuleVocab, t[m[0]:m[1]], "Word marks machine-written text: use a plainer word.", m[0])
	}
	for _, m := range fillerRe.FindAllStringIndex(t, -1) {
		w := t[m[0]:m[1]]
		add(RuleFiller, w, "Filler phrase: "+fillerFix[strings.ToLower(w)]+".", m[0])
	}
	for _, pm := range paraRe.FindAllStringIndex(t, -1) {
		par := t[pm[0]:pm[1]]
		trimmed := strings.TrimLeft(par, " \t\n")
		if openerRe.MatchString(trimmed) {
			first := strings.FieldsFunc(trimmed, func(r rune) bool { return r == '!' || r == ',' })[0]
			add(RuleVocab, first, "Opener is flattery: start with the answer.", pm[0]+len(par)-len(trimmed))
		}
	}
}

// checkSentence applies the per-sentence rules.
func checkSentence(s sentence, add addFn) {
	limit := SentenceWords
	if s.list {
		limit = StepWords
	}
	if n := wordCount(s.text); n > limit {
		first := strings.Join(strings.Fields(s.text)[:min(5, len(strings.Fields(s.text)))], " ") + "…"
		add(RuleSentenceLength, first, "Sentence has "+itoaN(n)+" words; the limit is "+itoaN(limit)+".", s.index)
	}
	toks := tokenize(s.text)
	if s.list && s.first {
		checkStepVerb(toks, s.index, add)
	}
	checkPerfect(toks, s.index, add)
	checkActive(toks, s.index, add)
	checkIng(toks, s.index, add)
}
