package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
)

// assumedOnDone returns a done feature with an assumed question on it.
func assumedOnDone(t *testing.T) (hgDir, id string, q *question.Question) {
	t.Helper()
	_, hgDir = prepProject(t)
	id = createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	q = addQuestionWith(t, id, "b", false)
	if _, err := runQuestionAssume(q.ID); err != nil {
		t.Fatal(err)
	}
	forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
	forceStatus(t, hgDir, "feature", id, models.StatusDone)
	return hgDir, id, q
}

// Finding 1: an overturning answer reopens a done item in the same locked
// write that records it. A start and complete that land after that write
// stand: nothing rolls the newer completion back to in-progress.
func TestAnswerOverturn_ConcurrentCompleteIsNotRolledBack(t *testing.T) {
	hgDir, id, q := assumedOnDone(t)
	questionAfterSaveHook = func() {
		questionAfterSaveHook = nil
		forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
		forceStatus(t, hgDir, "feature", id, models.StatusDone)
	}
	t.Cleanup(func() { questionAfterSaveHook = nil })

	o, err := answerQuestion(q.ID, "a", "", false)
	if err != nil {
		t.Fatal(err)
	}
	if !o.Reopened {
		t.Fatalf("the answer did not reopen the done item: %+v", o)
	}
	if st := parseItem(t, hgDir, id).Status; st != models.StatusDone {
		t.Fatalf("a completion newer than the reopen was rolled back to %q", st)
	}
	if got := questionByID(t, hgDir, id, q.ID); got.Status != question.StatusOverturned {
		t.Fatalf("answer not kept: %+v", got)
	}
}

// Finding 1: the answer and the reopen are one write, so the item is never
// left done with an overturned answer on it.
func TestAnswerOverturn_ReopensInTheSameWrite(t *testing.T) {
	hgDir, id, q := assumedOnDone(t)
	var seen models.NodeStatus
	questionAfterSaveHook = func() { seen = parseItem(t, hgDir, id).Status }
	t.Cleanup(func() { questionAfterSaveHook = nil })
	if _, err := answerQuestion(q.ID, "a", "", false); err != nil {
		t.Fatal(err)
	}
	if seen != models.StatusInProgress {
		t.Fatalf("after the answer's write the item was %q, want in-progress", seen)
	}
}

// Finding 4: a start whose item was completed before the assumptions were
// written records none of them; the questions stay open.
func TestAssumeOnStart_SkipsAnItemNoLongerInProgress(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	q := addQuestionWith(t, id, "b", false)
	forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
	forceStatus(t, hgDir, "feature", id, models.StatusDone) // a concurrent complete won the race

	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	var buf bytes.Buffer
	assumeQuestionsOnStart(&buf, p.Features.Collection, id)
	if got := questionByID(t, hgDir, id, q.ID); got.Status != question.StatusOpen || got.Assumed != "" {
		t.Fatalf("an assumption landed on a done item: %+v", got)
	}
	if !strings.Contains(buf.String(), "no longer in progress") {
		t.Fatalf("start did not say why nothing was assumed: %q", buf.String())
	}
}

// Finding 6: an id stored on two items is refused, naming both.
func TestAnswer_RefusesAnAmbiguousID(t *testing.T) {
	_, hgDir := prepProject(t)
	track := testSetupTrack(t, hgDir)
	one := createItem(t, hgDir, "feature", "One", track)
	two := createItem(t, hgDir, "bug", "Two", track)
	dup := question.Question{ID: "q-1234abcd", Text: "Which?", Status: question.StatusOpen,
		Options: []question.Option{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}}
	for _, id := range []string{one, two} {
		p, col, rid, err := openQuestionItem(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := updateQuestions(col, rid, func([]question.Question) ([]question.Question, error) {
			return []question.Question{dup}, nil
		}); err != nil {
			t.Fatal(err)
		}
		p.Close()
	}
	_, err := answerQuestion(dup.ID, "a", "", false)
	if err == nil || !strings.Contains(err.Error(), one) || !strings.Contains(err.Error(), two) {
		t.Fatalf("an ambiguous id must be refused naming both items, got %v", err)
	}
	for _, id := range []string{one, two} {
		if got := questionByID(t, hgDir, id, dup.ID); got.Answer != "" {
			t.Fatalf("%s was answered: %+v", id, got)
		}
	}
}

// Finding 6: an id minted with 32 bits still resolves.
func TestAnswer_OldShortIDResolves(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Old", testSetupTrack(t, hgDir))
	old := question.Question{ID: "q-0badf00d", Text: "Which?", Status: question.StatusOpen,
		Options: []question.Option{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}}
	p, col, rid, err := openQuestionItem(id)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := updateQuestions(col, rid, func([]question.Question) ([]question.Question, error) {
		return []question.Question{old}, nil
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()
	if _, err := answerQuestion(old.ID, "b", "", false); err != nil {
		t.Fatal(err)
	}
	if got := questionByID(t, hgDir, id, old.ID); got.Answer != "b" {
		t.Fatalf("old id not answered: %+v", got)
	}
}

// Finding 7: a questions property that cannot be read is never overwritten.
func TestQuestionAdd_RefusesAnUnreadableProperty(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Corrupt", testSetupTrack(t, hgDir))
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	const corrupt = `[{"id":"q-keep","text":`
	if _, err := p.Features.Collection.Update(id, func(n *models.Node) error {
		n.Properties[question.PropQuestions] = corrupt
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	p.Close()
	_, err = runQuestionAdd(id, "New?", []string{"a:A", "b:B"}, "", false, "agent", "")
	if err == nil || !strings.Contains(err.Error(), id) || !strings.Contains(err.Error(), "cannot be read") {
		t.Fatalf("add over an unreadable property must be refused naming the item, got %v", err)
	}
	if got := parseItem(t, hgDir, id).Properties[question.PropQuestions]; got != corrupt {
		t.Fatalf("the unreadable property was changed: %#v", got)
	}
}
