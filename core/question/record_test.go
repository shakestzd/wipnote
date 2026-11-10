package question

import (
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

var t0 = time.Date(2026, 11, 10, 6, 0, 0, 0, time.UTC)

func twoOpts() []Option {
	return []Option{{Key: "a", Label: "Alpha"}, {Key: "b", Label: "Beta", Tradeoff: "slower"}}
}

func TestParseOption(t *testing.T) {
	o, err := ParseOption("a:Use cache:costs memory: but fast")
	if err != nil {
		t.Fatal(err)
	}
	if o.Key != "a" || o.Label != "Use cache" || o.Tradeoff != "costs memory: but fast" {
		t.Fatalf("got %+v", o)
	}
	if o, err = ParseOption("b:Plain"); err != nil || o.Tradeoff != "" || o.Label != "Plain" {
		t.Fatalf("got %+v %v", o, err)
	}
	for _, bad := range []string{"", "nolabel", ":x", "k:", " : "} {
		if _, err := ParseOption(bad); err == nil {
			t.Errorf("ParseOption(%q) must fail", bad)
		}
	}
}

func TestNew_ValidatesOptions(t *testing.T) {
	q, err := New("Which store?", twoOpts(), "b", true, "agent", "msg-1", t0)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(q.ID, "q-") || q.Status != StatusOpen || !q.Blocking || q.Recommended != "b" {
		t.Fatalf("bad record %+v", q)
	}
	if q.RaisedAt != t0.Format(time.RFC3339) || q.RaisedBy != "agent" || q.SourceID != "msg-1" {
		t.Fatalf("bad provenance %+v", q)
	}
	five := append(twoOpts(), Option{Key: "c", Label: "C"}, Option{Key: "d", Label: "D"}, Option{Key: "e", Label: "E"})
	cases := map[string]struct {
		text string
		opts []Option
		rec  string
	}{
		"one option":    {"x", twoOpts()[:1], ""},
		"five options":  {"x", five, ""},
		"unknown rec":   {"x", twoOpts(), "z"},
		"empty text":    {" ", twoOpts(), ""},
		"duplicate key": {"x", []Option{{Key: "a", Label: "1"}, {Key: "a", Label: "2"}}, ""},
	}
	for name, c := range cases {
		if _, err := New(c.text, c.opts, c.rec, false, "", "", t0); err == nil {
			t.Errorf("%s: want error", name)
		}
	}
	if _, err := New("x", five[:4], "", false, "", "", t0); err != nil {
		t.Errorf("four options must pass: %v", err)
	}
}

func TestRecordAnswer(t *testing.T) {
	q, _ := New("Which?", twoOpts(), "", false, "", "", t0)
	if err := q.RecordAnswer("z", "", t0, false); err == nil {
		t.Fatal("unknown key must be refused")
	}
	if err := q.RecordAnswer("a", "go", t0, false); err != nil {
		t.Fatal(err)
	}
	if q.Answer != "a" || q.Note != "go" || q.Status != StatusConfirmed || q.AnsweredAt == "" {
		t.Fatalf("bad answer %+v", q)
	}
	if err := q.RecordAnswer("b", "", t0, false); err == nil {
		t.Fatal("re-answer without force must be refused")
	}
	if err := q.RecordAnswer("b", "", t0, true); err != nil || q.Answer != "b" {
		t.Fatalf("forced re-answer: %v %+v", err, q)
	}
}

func TestFromNodeRoundTripAndMerge(t *testing.T) {
	if FromNode(nil) != nil || FromNode(&models.Node{}) != nil {
		t.Fatal("no questions expected")
	}
	q1, _ := New("One?", twoOpts(), "", false, "", "", t0)
	q2, _ := New("Two?", twoOpts(), "", false, "", "", t0)
	n := &models.Node{Properties: map[string]any{PropQuestions: Property([]Question{*q1})}}
	got := FromNode(n)
	if len(got) != 1 || got[0].ID != q1.ID || len(got[0].Options) != 2 || got[0].Options[1].Tradeoff != "slower" {
		t.Fatalf("round trip: %+v", got)
	}
	changed := *q1
	changed.Answer = "a"
	merged := Merge(got, []Question{changed, *q2})
	if len(merged) != 2 || merged[0].Answer != "a" || merged[1].ID != q2.ID {
		t.Fatalf("merge: %+v", merged)
	}
	if i := Index(merged, q2.ID); i != 1 {
		t.Fatalf("Index = %d", i)
	}
}

func TestIsOpen(t *testing.T) {
	for s, want := range map[string]bool{StatusOpen: true, StatusAssumed: true, StatusConfirmed: false, StatusOverturned: false} {
		if (&Question{Status: s}).IsOpen() != want {
			t.Errorf("IsOpen(%s) != %v", s, want)
		}
	}
}
