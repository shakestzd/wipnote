package question

import (
	"strings"
	"testing"
)

func TestFromLabels_Shapes(t *testing.T) {
	free := FromLabels("Anything else?", nil, "", "worker:w1", "tu-1", t0)
	if !free.IsFreeText() || free.Status != StatusOpen || free.ID == "" || free.SourceID != "tu-1" || free.RaisedAt == "" {
		t.Fatalf("no options must be a free-text question: %+v", free)
	}
	one := FromLabels("Go?", []string{"yes"}, "yes", "", "", t0)
	if !one.IsFreeText() || one.Recommended != "" || !strings.Contains(one.Note, "yes") {
		t.Fatalf("one option is free text, the option kept in the note: %+v", one)
	}
	two := FromLabels("Which?", []string{"main", "dev"}, "dev", "", "", t0)
	if two.IsFreeText() || len(two.Options) != 2 || two.Options[1] != (Option{Key: "2", Label: "dev"}) || two.Recommended != "2" {
		t.Fatalf("two options: %+v", two)
	}
	six := FromLabels("Which?", []string{"a", "b", "c", "d", "e", "f"}, "e", "", "", t0)
	if len(six.Options) != MaxOptions || six.Recommended != "" || !strings.Contains(six.Note, "e, f") {
		t.Fatalf("more than four keeps the first four, the rest in the note: %+v", six)
	}
	dup := FromLabels("Which?", []string{"a", "b", "a", "c"}, "a", "", "", t0)
	if len(dup.Options) != 3 || dup.Recommended != "1" || dup.Options[2].Label != "c" || dup.Options[2].Key != "3" {
		t.Fatalf("duplicate labels dedupe, recommended on the first: %+v", dup)
	}
	if blank := FromLabels("  ", []string{"a", "a"}, "", "", "", t0); blank.Text == "" || !blank.IsFreeText() {
		t.Fatalf("blank text gets a placeholder; deduped to one option is free text: %+v", blank)
	}
	if a, b := FromLabels("x", nil, "", "", "", t0), FromLabels("x", nil, "", "", "", t0); a.ID == b.ID {
		t.Fatal("ids must be fresh")
	}
}

func TestMatchOption(t *testing.T) {
	q := FromLabels("Which?", []string{"SQLite", "HTML files"}, "", "", "", t0)
	for reply, want := range map[string]string{"1": "1", " 2 ": "2", "html FILES": "2", "sqlite": "1"} {
		if got, ok := q.MatchOption(reply); !ok || got != want {
			t.Errorf("%q -> %q %v, want %q", reply, got, ok, want)
		}
	}
	for _, reply := range []string{"", "3", "0", "postgres"} {
		if _, ok := q.MatchOption(reply); ok {
			t.Errorf("%q must not match", reply)
		}
	}
	keyed := Question{Options: []Option{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}}
	if got, ok := keyed.MatchOption("2"); !ok || got != "b" {
		t.Errorf("a 1-based number picks the option: %q %v", got, ok)
	}
}

func TestRecordText_FreeTextOnly(t *testing.T) {
	free := FromLabels("Anything else?", nil, "", "", "", t0)
	if err := free.RecordAnswer("a", "", t0, false); err == nil || !strings.Contains(err.Error(), "free-text") {
		t.Fatalf("a key on a free-text question must be refused: %v", err)
	}
	if err := free.RecordText("  ship it  ", "n", t0, false); err != nil || free.Answer != "ship it" || free.Status != StatusConfirmed || free.AnsweredAt == "" || free.Note != "n" {
		t.Fatalf("text answer: %+v %v", free, err)
	}
	if err := free.RecordText("again", "", t0, false); err == nil {
		t.Fatal("re-answer without force must be refused")
	}
	if err := free.RecordText(" ", "", t0, true); err == nil {
		t.Fatal("empty text must be refused")
	}
	opt := FromLabels("Which?", []string{"a", "b"}, "", "", "", t0)
	if err := opt.RecordText("a", "", t0, false); err == nil || !strings.Contains(err.Error(), "has options") {
		t.Fatalf("text on an option question must be refused: %v", err)
	}
}

func TestRecordOther_Settles(t *testing.T) {
	q := FromLabels("Which?", []string{"a", "b"}, "b", "", "", t0)
	if err := q.Assume(); err != nil {
		t.Fatal(err)
	}
	if err := q.RecordOther("something else", t0); err != nil || q.Status != StatusOverturned || q.Answer != "something else" {
		t.Fatalf("an answer outside the options overturns an assumption: %+v %v", q, err)
	}
	fresh := FromLabels("Which?", []string{"a", "b"}, "", "", "", t0)
	if err := fresh.RecordOther("custom", t0); err != nil || fresh.Status != StatusConfirmed {
		t.Fatalf("no assumption confirms: %+v %v", fresh, err)
	}
}
