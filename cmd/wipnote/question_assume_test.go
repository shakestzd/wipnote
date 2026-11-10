package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
)

// addQuestionWith stores a question with the given recommended key.
func addQuestionWith(t *testing.T, itemID, rec string, blocking bool) *question.Question {
	t.Helper()
	q, err := runQuestionAdd(itemID, "Which store?", []string{"a:SQLite", "b:HTML"}, rec, blocking, "agent", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return q
}

// questionByID reads one question back from the item's record.
func questionByID(t *testing.T, hgDir, itemID, qid string) question.Question {
	t.Helper()
	qs := question.FromNode(parseItem(t, hgDir, itemID))
	i := question.Index(qs, qid)
	if i < 0 {
		t.Fatalf("question %s not on %s", qid, itemID)
	}
	return qs[i]
}

// captureStderr swaps the package stderr for a buffer for the test.
func captureQuestionStderr(t *testing.T) *bytes.Buffer {
	t.Helper()
	var buf bytes.Buffer
	orig := stderr
	stderr = &buf
	t.Cleanup(func() { stderr = orig })
	return &buf
}

// forceStatus moves an item straight to status, skipping the completion gates.
func forceStatus(t *testing.T, hgDir, typeName, id string, status models.NodeStatus) {
	t.Helper()
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	col := collectionFor(p, typeName)
	if status == models.StatusDone {
		_, err = col.Complete(id)
	} else {
		_, err = col.Start(id)
	}
	if err != nil {
		t.Fatalf("set %s %s: %v", id, status, err)
	}
}

func TestStart_AssumesNonBlockingAndWarnsOnBlocking(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Owner questions", trackID)
	assumable := addQuestionWith(t, id, "b", false)
	noRec := addQuestionWith(t, id, "", false)
	blocking := addQuestionWith(t, id, "a", true)
	errBuf := captureQuestionStderr(t)

	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start must not fail on questions: %v", err)
	}
	if q := questionByID(t, hgDir, id, assumable.ID); q.Status != question.StatusAssumed || q.Assumed != "b" {
		t.Fatalf("non-blocking question not assumed: %+v", q)
	}
	if q := questionByID(t, hgDir, id, noRec.ID); q.Status != question.StatusOpen || q.Assumed != "" {
		t.Fatalf("question with no recommended option must stay open: %+v", q)
	}
	if q := questionByID(t, hgDir, id, blocking.ID); q.Status != question.StatusOpen || q.Assumed != "" {
		t.Fatalf("blocking question must not be assumed: %+v", q)
	}
	out := errBuf.String()
	if !strings.Contains(out, blocking.ID) || !strings.Contains(out, "blocking") {
		t.Fatalf("start must warn on the blocking question, stderr:\n%s", out)
	}
	if !strings.Contains(out, assumable.ID) {
		t.Fatalf("start must name the assumed question, stderr:\n%s", out)
	}
}

func TestQuestionAssume_Command(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Broken", trackID)
	q := addQuestionWith(t, id, "a", false)
	blocking := addQuestionWith(t, id, "a", true)
	noRec := addQuestionWith(t, id, "", false)

	itemID, err := runQuestionAssume(q.ID)
	if err != nil || itemID != id {
		t.Fatalf("assume: %q %v", itemID, err)
	}
	if got := questionByID(t, hgDir, id, q.ID); got.Status != question.StatusAssumed || got.Assumed != "a" {
		t.Fatalf("not assumed: %+v", got)
	}
	for _, refused := range []string{blocking.ID, noRec.ID, q.ID, "q-00000000"} {
		if _, err := runQuestionAssume(refused); err == nil {
			t.Errorf("assume %s must be refused", refused)
		}
	}
}

func TestWarnOpenQuestionsOnComplete(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "spike", "Look", trackID)
	open := addQuestionWith(t, id, "", false)
	assumed := addQuestionWith(t, id, "b", false)
	answered := addQuestionWith(t, id, "a", false)
	if _, err := runQuestionAssume(assumed.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runQuestionAnswer(answered.ID, "a", "", false); err != nil {
		t.Fatal(err)
	}
	p, _ := workitem.Open(hgDir, "claude-code")
	defer p.Close()
	var buf bytes.Buffer
	warnOpenQuestionsOnComplete(&buf, collectionFor(p, "spike"), id)
	out := buf.String()
	if !strings.Contains(out, open.ID) || !strings.Contains(out, assumed.ID) || strings.Contains(out, answered.ID) {
		t.Fatalf("complete warning must list open and assumed questions only:\n%s", out)
	}
}

func TestQuestionAnswer_ConfirmsOrOverturns(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Owner questions", trackID)
	same := addQuestionWith(t, id, "b", false)
	differ := addQuestionWith(t, id, "b", false)
	forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
	if _, err := runQuestionAssume(same.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := runQuestionAssume(differ.ID); err != nil {
		t.Fatal(err)
	}

	o, err := answerQuestion(same.ID, "b", "", false)
	if err != nil || o.Status != question.StatusConfirmed || o.Reopened {
		t.Fatalf("same answer must confirm: %+v %v", o, err)
	}
	o, err = answerQuestion(differ.ID, "a", "", false)
	if err != nil || o.Status != question.StatusOverturned || o.Reopened || o.ItemStatus != models.StatusInProgress {
		t.Fatalf("different answer on an in-progress item must overturn and name it: %+v %v", o, err)
	}
	if msg := o.message(); !strings.Contains(msg, id) || !strings.Contains(msg, "overturned") {
		t.Fatalf("message must name the in-progress item: %q", msg)
	}
}

func TestQuestionAnswer_OverturnReopensDoneItems(t *testing.T) {
	for _, typeName := range []string{"feature", "bug", "spike"} {
		t.Run(typeName, func(t *testing.T) {
			_, hgDir := prepProject(t)
			trackID := testSetupTrack(t, hgDir)
			id := createItem(t, hgDir, typeName, "Owner questions", trackID)
			q := addQuestionWith(t, id, "b", false)
			if _, err := runQuestionAssume(q.ID); err != nil {
				t.Fatal(err)
			}
			forceStatus(t, hgDir, typeName, id, models.StatusInProgress)
			forceStatus(t, hgDir, typeName, id, models.StatusDone)

			o, err := answerQuestion(q.ID, "a", "", false)
			if err != nil || o.Status != question.StatusOverturned || !o.Reopened {
				t.Fatalf("overturn on a done %s must reopen: %+v %v", typeName, o, err)
			}
			if st := parseItem(t, hgDir, id).Status; st != models.StatusInProgress {
				t.Fatalf("%s status after overturn = %q, want in-progress", typeName, st)
			}
			// A forced re-answer equal to the assumption confirms.
			o, err = answerQuestion(q.ID, "b", "", true)
			if err != nil || o.Status != question.StatusConfirmed || o.Reopened {
				t.Fatalf("forced re-answer must confirm: %+v %v", o, err)
			}
		})
	}
}
