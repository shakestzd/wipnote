package main

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

func TestFileWorkerAsks_FilesOnceWithAnswerAndRecommended(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "Owner questions", testSetupTrack(t, hgDir))
	run := session.RunAsksOut{Run: "dlg-1", WorkItem: id, Asks: []session.Ask{
		{ID: "msg-aaaaaaaaaaaa-q1", Kind: session.KindQuestionFile, Text: "Which branch?", Options: []string{"main", "dev"}, Recommended: "dev"},
		{ID: "tu-1", Kind: session.KindAsk, Text: "Pick one", Options: []string{"x", "y"}, Answered: true, Answer: "y"},
		{ID: "tu-2", Kind: session.KindAsk, Text: "Free form?", Answered: true, Answer: "just do it"},
	}}
	f, err := fileWorkerAsks("w1", run)
	if err != nil || len(f.Filed) != 3 || f.Warning != "" {
		t.Fatalf("filed %+v err %v", f, err)
	}
	qs := question.FromNode(parseItem(t, hgDir, id))
	if len(qs) != 3 {
		t.Fatalf("stored %+v", qs)
	}
	if qs[0].SourceID != "msg-aaaaaaaaaaaa-q1" || qs[0].Recommended != "2" || qs[0].Options[1].Label != "dev" || qs[0].Status != question.StatusOpen || qs[0].RaisedBy != "worker:w1" {
		t.Fatalf("file ask %+v", qs[0])
	}
	if qs[1].Answer != "2" || qs[1].Status != question.StatusConfirmed {
		t.Fatalf("answered ask %+v", qs[1])
	}
	if qs[2].Answer != "just do it" || qs[2].Status != question.StatusConfirmed {
		t.Fatalf("free-text answer %+v", qs[2])
	}
	again, err := fileWorkerAsks("w1", run)
	if err != nil || len(again.Filed) != 0 || len(question.FromNode(parseItem(t, hgDir, id))) != 3 {
		t.Fatalf("re-run filed %+v err %v", again, err)
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
