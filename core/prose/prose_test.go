package prose

import (
	"strings"
	"testing"
)

func rulesOf(notes []Note) []string {
	var r []string
	for _, n := range notes {
		r = append(r, n.Rule)
	}
	return r
}

func has(notes []Note, rule string) bool {
	for _, n := range notes {
		if n.Rule == rule {
			return true
		}
	}
	return false
}

func TestRuleTable(t *testing.T) {
	cases := []struct {
		rule       string
		pass, fail string
		opts       Options
	}{
		{RuleSentenceLength, "Run the tests.", strings.Repeat("word ", 26) + "end.", Options{}},
		{RuleSentenceLength, "Run the tests.", strings.Repeat("word ", 21) + "end.", Options{Step: true}},
		{RuleSemicolon, "Run it. Then stop.", "Run it; then stop.", Options{}},
		{RulePerfectTense, "The hook failed.", "The hook has failed.", Options{}},
		{RulePerfectTense, "We run the gate.", "We have not run it yet and had created one.", Options{}},
		{RuleActiveVoice, "The hook writes the file.", "The file is written by the hook.", Options{}},
		{RuleActiveVoice, "The flag is required.", "The row was deleted.", Options{}},
		{RuleStepVerb, "- Run the gate.\n", "1. The gate runs.\n", Options{}},
		{RuleStepVerb, "Add a flag.", "Adding a flag.", Options{Step: true}},
		{RuleIngWord, "The parser reads the file.", "The parsing of the file is slow.", Options{}},
		{RuleVocab, "The tool is plain.", "The tool is seamless.", Options{}},
		{RuleFiller, "Use it to start.", "Use it in order to start.", Options{}},
	}
	for _, c := range cases {
		if got := Check(c.pass, c.opts); has(got, c.rule) {
			t.Errorf("%s: passing text %q got %v", c.rule, c.pass, rulesOf(got))
		}
		if got := Check(c.fail, c.opts); !has(got, c.rule) {
			t.Errorf("%s: failing text %q got %v", c.rule, c.fail, rulesOf(got))
		}
	}
}

func TestSkipCases(t *testing.T) {
	skips := []string{
		"```\nRun it; then the file has been written by us.\n```\n",
		"Use `a; b` here.",
		"See https://example.com/a;b?x=1 for more.",
		"| a; b | seamless |\n| --- | --- |\n",
		"Edit docs/seamless/pivotal.md now.",
		"Use and/or here.",
	}
	for _, s := range skips {
		if got := Check(s, Options{}); len(got) != 0 {
			t.Errorf("%q: want no notes, got %+v", s, got)
		}
	}
}

func TestPosition(t *testing.T) {
	got := Check("Fine line.\nBad; line.", Options{})
	if len(got) != 1 || got[0].Line != 2 || got[0].Col != 4 || got[0].Rule != RuleSemicolon {
		t.Fatalf("got %+v", got)
	}
}

func TestListStepLimitAndLabel(t *testing.T) {
	long := strings.Repeat("word ", 21)
	if !has(Check("- Run "+long+"now.", Options{}), RuleSentenceLength) {
		t.Error("list step over 20 words should note")
	}
	if has(Check("- **Label.** Run "+long+"now.", Options{}), RuleStepVerb) {
		t.Error("bold-label item is prose, not a step")
	}
	if has(Check("Run "+long+"now.", Options{}), RuleSentenceLength) {
		t.Error("prose sentence under 25 words should pass")
	}
}

func TestEmptyAndRulesTable(t *testing.T) {
	if Check("  \n", Options{}) != nil {
		t.Error("blank text has no notes")
	}
	if len(Rules()) != 8 {
		t.Errorf("rules: %d", len(Rules()))
	}
}

func TestStepVerb_NumberedOnly(t *testing.T) {
	has := func(text string) bool {
		for _, n := range Check(text, Options{}) {
			if n.Rule == RuleStepVerb {
				return true
			}
		}
		return false
	}
	if has("- The limit is in one table.\n- The notes never block.") {
		t.Error("step-verb must not fire on descriptive bullets")
	}
	if !has("1. The tests are run.\n2. Run the gate.") {
		t.Error("step-verb must fire on a numbered step that does not start with a verb")
	}
}
