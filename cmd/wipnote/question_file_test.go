package main

import (
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// storedBySource reads back the question filed from source on itemID.
func storedBySource(t *testing.T, hgDir, itemID, source string) question.Question {
	t.Helper()
	for _, q := range question.FromNode(parseItem(t, hgDir, itemID)) {
		if q.SourceID == source {
			return q
		}
	}
	t.Fatalf("no question from %s on %s", source, itemID)
	return question.Question{}
}

func TestFileWorkerAsks_FilesOnceWithAnswerAndRecommended(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	run := session.RunAsksOut{Run: "dlg-1", WorkItem: id, Asks: []session.Ask{
		{ID: "msg-aaaaaaaaaaaa-q1", Kind: session.KindQuestionFile, Text: "Which branch?", Options: []string{"main", "dev"}, Recommended: "dev"},
		{ID: "tu-1", Kind: session.KindAsk, Text: "Pick one", Options: []string{"x", "y"}, Answered: true, Answer: "Y"},
		{ID: "tu-2", Kind: session.KindAsk, Text: "Free form?", Answered: true, Answer: "just do it"},
		{ID: "tu-3", Kind: session.KindAsk, Text: "Number?", Options: []string{"x", "y"}, Answered: true, Answer: "1"},
	}}
	f, err := fileWorkerAsks("w1", run)
	if err != nil || len(f.Filed) != 4 || f.Warning != "" {
		t.Fatalf("filed %+v err %v", f, err)
	}
	if q := storedBySource(t, hgDir, id, "msg-aaaaaaaaaaaa-q1"); q.Recommended != "2" || q.Options[1].Label != "dev" || q.Status != question.StatusOpen || q.RaisedBy != "worker:w1" {
		t.Fatalf("file ask %+v", q)
	}
	if q := storedBySource(t, hgDir, id, "tu-1"); q.Answer != "2" || q.Status != question.StatusConfirmed {
		t.Fatalf("label answer (any case) %+v", q)
	}
	if q := storedBySource(t, hgDir, id, "tu-2"); !q.IsFreeText() || q.Answer != "just do it" || q.Status != question.StatusConfirmed {
		t.Fatalf("free-text answer %+v", q)
	}
	if q := storedBySource(t, hgDir, id, "tu-3"); q.Answer != "1" || q.Status != question.StatusConfirmed {
		t.Fatalf("number answer %+v", q)
	}
	again, err := fileWorkerAsks("w1", run)
	if err != nil || len(again.Filed) != 0 || len(again.Answered) != 0 || len(question.FromNode(parseItem(t, hgDir, id))) != 4 {
		t.Fatalf("re-run filed %+v err %v", again, err)
	}
}

// Finding 8: a caller reply that names no option leaves the question open
// for the owner, with the reply in the note and a warning naming the id.
func TestFileWorkerAsks_UnmatchedReplyStaysOpen(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	run := session.RunAsksOut{WorkItem: id, Asks: []session.Ask{
		{ID: "tu-1", Kind: session.KindAsk, Text: "Which?", Options: []string{"x", "y"}, Answered: true, Answer: "neither, use z"}}}
	f, err := fileWorkerAsks("w1", run)
	if err != nil || len(f.Filed) != 1 {
		t.Fatalf("%+v %v", f, err)
	}
	q := storedBySource(t, hgDir, id, "tu-1")
	if q.Status != question.StatusOpen || q.Answer != "" || !strings.Contains(q.Note, "neither, use z") {
		t.Fatalf("unmatched reply %+v", q)
	}
	if !strings.Contains(f.Warning, q.ID) {
		t.Fatalf("warning must name %s: %q", q.ID, f.Warning)
	}
	if again, _ := fileWorkerAsks("w1", run); again.Warning != "" || len(again.Answered) != 0 {
		t.Fatalf("a re-run must not warn again: %+v", again)
	}
}

// Finding 2: an ask answered in the worker's pane is filed confirmed with
// the pane's answer, never open.
func TestFileWorkerAsks_PaneAnswersAreConfirmed(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	_, err := fileWorkerAsks("w1", session.RunAsksOut{WorkItem: id, Asks: []session.Ask{
		{ID: "tu-1", Kind: session.KindAsk, Text: "Which?", Options: []string{"SQLite", "HTML"}, Answered: true, Pane: true, Answer: "HTML"},
		{ID: "tu-2", Kind: session.KindAsk, Text: "Which?", Options: []string{"a", "b"}, Answered: true, Pane: true, Answer: "a, b"},
		{ID: "tu-3", Kind: session.KindAsk, Text: "Old ask", Answered: true, Pane: true},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if q := storedBySource(t, hgDir, id, "tu-1"); q.Answer != "2" || q.Status != question.StatusConfirmed {
		t.Fatalf("pane option %+v", q)
	}
	if q := storedBySource(t, hgDir, id, "tu-2"); q.Answer != "a, b" || q.Status != question.StatusConfirmed {
		t.Fatalf("pane answer outside one option %+v", q)
	}
	if q := storedBySource(t, hgDir, id, "tu-3"); q.Status != question.StatusConfirmed || q.Answer == "" {
		t.Fatalf("pane answer with no captured text %+v", q)
	}
}

// Finding 3: an ask filed open whose answer arrives later is answered on the
// next run, through the settle path: overturning an assumption reopens a
// done item. No record is duplicated.
func TestFileWorkerAsks_LateAnswerIsRecorded(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	ask := session.Ask{ID: "msg-aaaaaaaaaaaa-q1", Kind: session.KindQuestionFile, Text: "Which branch?", Options: []string{"main", "dev"}, Recommended: "dev"}
	run := session.RunAsksOut{WorkItem: id, Asks: []session.Ask{ask}}
	f, err := fileWorkerAsks("w1", run)
	if err != nil || len(f.Filed) != 1 {
		t.Fatalf("%+v %v", f, err)
	}
	if _, err := runQuestionAssume(f.Filed[0]); err != nil {
		t.Fatal(err)
	}
	forceStatus(t, hgDir, "feature", id, models.StatusInProgress)
	forceStatus(t, hgDir, "feature", id, models.StatusDone)

	ask.Answered, ask.Answer = true, "main"
	run.Asks = []session.Ask{ask}
	late, err := fileWorkerAsks("w1", run)
	if err != nil || len(late.Filed) != 0 || len(late.Answered) != 1 || late.Answered[0] != f.Filed[0] {
		t.Fatalf("late answer %+v %v", late, err)
	}
	q := storedBySource(t, hgDir, id, ask.ID)
	if q.Answer != "1" || q.Status != question.StatusOverturned || len(question.FromNode(parseItem(t, hgDir, id))) != 1 {
		t.Fatalf("late answer must settle against the assumption: %+v", q)
	}
	if st := parseItem(t, hgDir, id).Status; st != models.StatusInProgress || !strings.Contains(late.Warning, "Reopened") {
		t.Fatalf("overturn on a done item must reopen it and say so: %s %q", st, late.Warning)
	}
	if again, _ := fileWorkerAsks("w1", run); len(again.Answered) != 0 {
		t.Fatalf("an answered record is not answered again: %+v", again)
	}
}

// Finding 9: worker records are built directly, whatever the option count.
func TestAskRecord_Shapes(t *testing.T) {
	testNow := time.Now()
	six := askRecord("w1", session.Ask{ID: "tu", Text: "Which?", Options: []string{"a", "b", "c", "d", "e", "f"}}, testNow)
	if len(six.Options) != question.MaxOptions || !strings.Contains(six.Note, "e, f") || six.ID == "" {
		t.Fatalf("six options %+v", six)
	}
	dup := askRecord("w1", session.Ask{ID: "tu", Text: "Which?", Options: []string{"a", "a", "b"}, Recommended: "a"}, testNow)
	if len(dup.Options) != 2 || dup.Recommended != "1" {
		t.Fatalf("duplicate labels %+v", dup)
	}
	free := askRecord("w1", session.Ask{ID: "tu", Text: " "}, testNow)
	if !free.IsFreeText() || free.Text != question.NoTextPlaceholder || free.Status != question.StatusOpen {
		t.Fatalf("no options and no text %+v", free)
	}
}

func TestFileWorkerAsks_NoItemWarnsNamingQuestions(t *testing.T) {
	f, err := fileWorkerAsks("w1", session.RunAsksOut{Asks: []session.Ask{{ID: "tu-1", Text: "Which store?"}}})
	if err != nil || len(f.Filed) != 0 || !strings.Contains(f.Warning, "Which store?") || !strings.Contains(f.Warning, "w1") {
		t.Fatalf("%+v %v", f, err)
	}
	if f, _ := fileWorkerAsks("w1", session.RunAsksOut{}); f.Warning != "" {
		t.Fatalf("no asks must not warn: %+v", f)
	}
}
