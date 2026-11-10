package main

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// fileFreeText files a worker ask with no options and returns its id.
func fileFreeText(t *testing.T, itemID string) string {
	t.Helper()
	f, err := fileWorkerAsks("w1", session.RunAsksOut{WorkItem: itemID, Asks: []session.Ask{
		{ID: "tu-free", Kind: session.KindAsk, Text: "Anything else?"}}})
	if err != nil || len(f.Filed) != 1 {
		t.Fatalf("file: %+v %v", f, err)
	}
	return f.Filed[0]
}

// setItemStatus writes status straight onto the item.
func setItemStatus(t *testing.T, hgDir, typeName, id string, status models.NodeStatus) {
	t.Helper()
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if err := collectionFor(p, typeName).Edit(id).SetStatus(string(status)).Save(); err != nil {
		t.Fatal(err)
	}
}

// Finding 1: a question with no options is answered with --text; a key on it,
// or text on an option question, is refused naming the right form.
func TestQuestionAnswer_FreeText(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	qid := fileFreeText(t, id)

	if _, err := answerQuestion(qid, "a", "", false); err == nil || !strings.Contains(err.Error(), "wipnote question answer "+qid+` --text "<reply>"`) {
		t.Fatalf("a key on a free-text question must name --text: %v", err)
	}
	o, err := answerQuestionText(qid, "ship it", "", false)
	if err != nil || o.Status != question.StatusConfirmed || o.ItemID != id {
		t.Fatalf("text answer: %+v %v", o, err)
	}
	if got := questionByID(t, hgDir, id, qid); got.Answer != "ship it" || got.Status != question.StatusConfirmed {
		t.Fatalf("stored %+v", got)
	}
	opt := addTestQuestion(t, id, false)
	if _, err := answerQuestionText(opt.ID, "SQLite", "", false); err == nil || !strings.Contains(err.Error(), "wipnote question answer "+opt.ID+" <key>") {
		t.Fatalf("text on an option question must name the key form: %v", err)
	}

	cmd := questionAnswerCmd()
	cmd.SetArgs([]string{opt.ID, "a", "--text", "x"})
	cmd.SetOut(&bytes.Buffer{})
	cmd.SetErr(&bytes.Buffer{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("a key and --text together must be refused")
	}

	rows, _ := collectQuestionRows(hgDir, id, false, time.Now())
	var buf bytes.Buffer
	if err := renderQuestionRows(&buf, rows, "text"); err != nil || !strings.Contains(buf.String(), "(free text)") {
		t.Fatalf("list must show a free-text question as such: %q %v", buf.String(), err)
	}
	buf.Reset()
	_ = renderQuestionRows(&buf, rows, "json")
	if !strings.Contains(buf.String(), `"free_text": true`) {
		t.Fatalf("json row lacks free_text: %s", buf.String())
	}
}

// Finding 5: a save merges into the item's current questions, so a write
// that lands between another writer's read and its save is kept.
func TestQuestionSave_KeepsInterleavedWrites(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	a := addTestQuestion(t, id, false)
	b := addTestQuestion(t, id, false)
	var added *question.Question
	questionSaveHook = func() {
		questionSaveHook = nil // the second writer runs once, between the first's read and save
		added = addTestQuestion(t, id, false)
		if _, err := answerQuestion(b.ID, "b", "", false); err != nil {
			t.Errorf("second writer: %v", err)
		}
	}
	t.Cleanup(func() { questionSaveHook = nil })

	if _, err := answerQuestion(a.ID, "a", "", false); err != nil {
		t.Fatal(err)
	}
	qs := question.FromNode(parseItem(t, hgDir, id))
	if len(qs) != 3 || added == nil || question.Index(qs, added.ID) < 0 {
		t.Fatalf("the concurrent add was dropped: %+v", qs)
	}
	if got := qs[question.Index(qs, a.ID)]; got.Answer != "a" {
		t.Fatalf("first writer's answer lost: %+v", got)
	}
	if got := qs[question.Index(qs, b.ID)]; got.Answer != "b" {
		t.Fatalf("the concurrent answer was dropped: %+v", got)
	}
}

// Finding 6: an overturn always warns with a redo note naming the item, the
// assumed answer and the new one; only a done item is reopened.
func TestQuestionAnswer_OverturnWarnsOnEveryStatus(t *testing.T) {
	for _, st := range []models.NodeStatus{models.StatusBlocked, models.StatusTodo, "review", models.StatusInProgress, models.StatusDone} {
		t.Run(string(st), func(t *testing.T) {
			_, hgDir := prepProject(t)
			id := createItem(t, hgDir, "bug", "Owner questions", testSetupTrack(t, hgDir))
			q := addQuestionWith(t, id, "b", false)
			if _, err := runQuestionAssume(q.ID); err != nil {
				t.Fatal(err)
			}
			if st == models.StatusDone {
				forceStatus(t, hgDir, "bug", id, models.StatusInProgress)
				forceStatus(t, hgDir, "bug", id, models.StatusDone)
			} else {
				setItemStatus(t, hgDir, "bug", id, st)
			}
			o, err := answerQuestion(q.ID, "a", "", false)
			if err != nil || o.Status != question.StatusOverturned || o.Reopened != (st == models.StatusDone) {
				t.Fatalf("%+v %v", o, err)
			}
			msg := o.message()
			for _, want := range []string{"overturned", id, `"b" (HTML)`, `"a" (SQLite)`, "redo"} {
				if !strings.Contains(msg, want) {
					t.Errorf("message lacks %q: %s", want, msg)
				}
			}
			if st != models.StatusDone && st != models.StatusInProgress && !strings.Contains(msg, "when it resumes") {
				t.Errorf("a paused item's message must say to redo the work when it resumes: %s", msg)
			}
		})
	}
}
