// Package prose checks plain-writing rules on text wipnote takes in.
// It is pure Go and uses the standard library only. Notes never block.
package prose

import "strconv"

// Limits shared by the rules and the table below, so no number is typed twice.
const (
	SentenceWords = 25 // longest sentence of prose
	StepWords     = 20 // longest list step
)

// noun-cluster and decision-question are not implemented: see doc.go.
// Rule names are stable: they appear in notes, JSON and the rules table.
const (
	RuleSentenceLength = "sentence-length"
	RuleStepVerb       = "step-verb"
	RuleSemicolon      = "semicolon"
	RuleIngWord        = "ing-word"
	RulePerfectTense   = "perfect-tense"
	RuleActiveVoice    = "active-voice"
	RuleVocab          = "vocab"
	RuleFiller         = "filler"
)

// RuleInfo is one row of the rule table.
type RuleInfo struct {
	Name        string `json:"name"`
	Limit       string `json:"limit"`
	Description string `json:"description"`
}

// Rules is the single table of rule names, limits and descriptions.
func Rules() []RuleInfo {
	n := strconv.Itoa
	return []RuleInfo{
		{RuleSentenceLength, n(SentenceWords) + " words (" + n(StepWords) + " for a step)", "Keep sentences short."},
		{RuleStepVerb, "first word", "A step starts with a verb."},
		{RuleSemicolon, "0", "Use two sentences, not a semicolon."},
		{RuleIngWord, "0 as noun", "Do not use an -ing word as a noun."},
		{RulePerfectTense, "0", "Use simple past or present, not has/have/had plus a participle."},
		{RuleActiveVoice, "0", "Name the actor: write active voice."},
		{RuleVocab, "0", "Avoid words that mark machine-written text."},
		{RuleFiller, "0", "Cut filler phrases such as \"in order to\"."},
	}
}
