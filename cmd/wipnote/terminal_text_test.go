package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

func TestStripTerminalControls(t *testing.T) {
	cases := map[string][2]string{
		"csi colour":          {"a\x1b[31mred\x1b[0m b", "ared b"},
		"osc title, bel":      {"x\x1b]0;pwned\x07y", "xy"},
		"osc link, st":        {"x\x1b]8;;http://e\x1b\\link\x1b]8;;\x1b\\y", "xlinky"},
		"dcs":                 {"x\x1bPq#0\x1b\\y", "xy"},
		"two-char escape":     {"x\x1bcy", "xy"},
		"c1 csi":              {"x\u009b2Jy", "xy"},
		"c1 osc":              {"x\u009d0;t\u009cy", "xy"},
		"other c1":            {"x\u0085y", "xy"},
		"c0 and del":          {"a\x00b\x07c\rd\x7fe\x08f", "abcdef"},
		"keeps tab and lf":    {"a\tb\nc", "a\tb\nc"},
		"plain unicode":       {"Which store? ünïcode ✓", "Which store? ünïcode ✓"},
		"unterminated csi":    {"x\x1b[31", "x"},
		"lone esc at the end": {"x\x1b", "x"},
		"unterminated osc":    {"x\x1b]0;t", "x"},
	}
	for name, c := range cases {
		if got := stripTerminalControls(c[0]); got != c[1] {
			t.Errorf("%s: got %q want %q", name, got, c[1])
		}
	}
	if got := cleanTitle("a\x1b[1mb\nc"); got != "ab c" {
		t.Errorf("cleanTitle: %q", got)
	}
}

// hostile is worker text carrying a colour, a title-setting OSC and a C1 CSI.
const hostile = "Which\x1b[31m store\x1b]0;pwned\x07?\u009b2J"

func assertNoControls(t *testing.T, where, s string) {
	t.Helper()
	if strings.ContainsAny(s, "\x1b\x07\u009b\u009d") {
		t.Errorf("%s prints a terminal control: %q", where, s)
	}
}

// Finding 10: question text, options, notes and answers are printed without
// terminal controls in list, the start and complete warnings and the
// overturn message.
func TestQuestionOutput_StripsTerminalControls(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	q, err := runQuestionAdd(id, hostile, []string{"a:SQL\x1b[1mite:fast\x1b]0;x\x07", "b:HT\u009b2JML"}, "b", false, "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	blocking, err := runQuestionAdd(id, hostile, []string{"a:A\x1b[1m", "b:B"}, "a", true, "agent", "")
	if err != nil {
		t.Fatal(err)
	}
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	col := p.Features.Collection
	forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
	var start bytes.Buffer
	assumeQuestionsOnStart(&start, col, id)
	assertNoControls(t, "start", start.String())
	if !strings.Contains(start.String(), blocking.ID) {
		t.Fatalf("start output lacks the blocking question: %q", start.String())
	}

	assertListStripsControls(t, hgDir, id)
	var done bytes.Buffer
	warnOpenQuestionsOnComplete(&done, col, id)
	assertNoControls(t, "complete", done.String())

	o, err := answerQuestionText(q.ID, "x", "", false)
	if err == nil {
		t.Fatalf("an option question took --text: %+v", o)
	}
	o, err = answerQuestion(q.ID, "a", "note\x1b[2J", false)
	if err != nil {
		t.Fatal(err)
	}
	assertNoControls(t, "overturn", o.message())
	if !strings.Contains(o.message(), "overturned") {
		t.Fatalf("no overturn: %q", o.message())
	}
	assertNoControls(t, "answer line", answeredLine(o))
}

// assertListStripsControls checks `question list` text and JSON for item id
// print no terminal controls and keep the question text.
func assertListStripsControls(t *testing.T, hgDir, id string) {
	t.Helper()
	rows, _ := collectQuestionRows(hgDir, id, false, time.Now())
	for _, format := range []string{"text", "json"} {
		var list bytes.Buffer
		if err := renderQuestionRows(&list, rows, format); err != nil {
			t.Fatal(err)
		}
		assertNoControls(t, "list "+format, list.String())
		if format == "text" && !strings.Contains(list.String(), "Which store?") {
			t.Fatalf("list lost the text: %q", list.String())
		}
	}
}
