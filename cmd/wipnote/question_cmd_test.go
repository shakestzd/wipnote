package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/question"
)

func addTestQuestion(t *testing.T, itemID string, blocking bool) *question.Question {
	t.Helper()
	q, err := runQuestionAdd(itemID, "Which store?", []string{"a:SQLite:needs a file", "b:HTML"}, "b", blocking, "agent", "")
	if err != nil {
		t.Fatalf("add: %v", err)
	}
	return q
}

func TestQuestionAdd_StoresOpenRecord(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Owner questions", trackID)

	q := addTestQuestion(t, id, true)
	qs := question.FromNode(parseItem(t, hgDir, id))
	if len(qs) != 1 || qs[0].ID != q.ID || qs[0].Status != question.StatusOpen || !qs[0].Blocking {
		t.Fatalf("stored %+v", qs)
	}
	if qs[0].Options[0].Tradeoff != "needs a file" || qs[0].Recommended != "b" {
		t.Fatalf("options not stored: %+v", qs[0])
	}
	addTestQuestion(t, id, false)
	if n := len(question.FromNode(parseItem(t, hgDir, id))); n != 2 {
		t.Fatalf("second add must append, have %d", n)
	}
}

func TestQuestionAdd_Refusals(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "Broken thing", trackID)

	cases := map[string]struct {
		item string
		opts []string
		rec  string
	}{
		"one option":   {id, []string{"a:A"}, ""},
		"five options": {id, []string{"a:A", "b:B", "c:C", "d:D", "e:E"}, ""},
		"unknown rec":  {id, []string{"a:A", "b:B"}, "z"},
		"bad option":   {id, []string{"a", "b:B"}, ""},
		"track item":   {trackID, []string{"a:A", "b:B"}, ""},
	}
	for name, c := range cases {
		if _, err := runQuestionAdd(c.item, "x?", c.opts, c.rec, false, "", ""); err == nil {
			t.Errorf("%s: want refusal", name)
		}
	}
	if len(question.FromNode(parseItem(t, hgDir, id))) != 0 {
		t.Fatal("refused adds must store nothing")
	}
}

func TestQuestionAnswer_RecordsAndGuards(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "spike", "Look into it", trackID)
	q := addTestQuestion(t, id, false)

	if _, err := runQuestionAnswer(q.ID, "z", "", false); err == nil {
		t.Fatal("unknown key must be refused")
	}
	if _, err := runQuestionAnswer("q-00000000", "a", "", false); err == nil {
		t.Fatal("unknown question must be refused")
	}
	itemID, err := runQuestionAnswer(q.ID, "a", "owner prefers files", false)
	if err != nil || itemID != id {
		t.Fatalf("answer: %v item=%s", err, itemID)
	}
	got := question.FromNode(parseItem(t, hgDir, id))[0]
	if got.Answer != "a" || got.Note != "owner prefers files" || got.AnsweredAt == "" || got.Status != question.StatusConfirmed {
		t.Fatalf("answer not stored: %+v", got)
	}
	if _, err := runQuestionAnswer(q.ID, "b", "", false); err == nil {
		t.Fatal("re-answer without --force must be refused")
	}
	if _, err := runQuestionAnswer(q.ID, "b", "", true); err != nil {
		t.Fatalf("forced re-answer: %v", err)
	}
}

func TestQuestionList_SpansTypesAndFilters(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	feat := createItem(t, hgDir, "feature", "Feature with question", trackID)
	bug := createItem(t, hgDir, "bug", "Bug with question", trackID)
	qf := addTestQuestion(t, feat, true)
	qb := addTestQuestion(t, bug, false)
	if _, err := runQuestionAnswer(qb.ID, "a", "", false); err != nil {
		t.Fatal(err)
	}

	all, err := collectQuestionRows(hgDir, "", false, time.Now())
	if err != nil || len(all) != 2 {
		t.Fatalf("all rows: %v %+v", err, all)
	}
	open, _ := collectQuestionRows(hgDir, "", true, time.Now())
	if len(open) != 1 || open[0].QuestionID != qf.ID || open[0].ItemTitle != "Feature with question" {
		t.Fatalf("open rows: %+v", open)
	}
	one, _ := collectQuestionRows(hgDir, bug, false, time.Now())
	if len(one) != 1 || one[0].ItemID != bug || one[0].Answer != "a" || one[0].Status != question.StatusConfirmed {
		t.Fatalf("--item rows: %+v", one)
	}
	gone, err := collectQuestionRows(hgDir, "feat-deadbeef", false, time.Now())
	if err != nil || len(gone) != 0 {
		t.Fatalf("unresolved item must be skipped: %v %+v", err, gone)
	}
}

func TestQuestionList_JSONShape(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	feat := createItem(t, hgDir, "feature", "Feature with question", trackID)
	addTestQuestion(t, feat, true)

	rows, err := collectQuestionRows(hgDir, "", false, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := renderQuestionRows(&buf, rows, "json"); err != nil {
		t.Fatal(err)
	}
	var decoded []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &decoded); err != nil || len(decoded) != 1 {
		t.Fatalf("json: %v %s", err, buf.String())
	}
	for _, key := range []string{"item_id", "item_title", "question_id", "text", "options", "recommended",
		"blocking", "status", "assumed", "raised_by", "age", "answer"} {
		if _, ok := decoded[0][key]; !ok {
			t.Errorf("json row missing %q: %s", key, buf.String())
		}
	}
	buf.Reset()
	if err := renderQuestionRows(&buf, nil, "json"); err != nil || strings.TrimSpace(buf.String()) != "[]" {
		t.Fatalf("empty json must be [], got %q", buf.String())
	}
}

func TestQuestionAge(t *testing.T) {
	now := time.Date(2026, 11, 10, 12, 0, 0, 0, time.UTC)
	for raised, want := range map[string]string{
		"2026-11-10T11:59:30Z": "0m",
		"2026-11-10T11:15:00Z": "45m",
		"2026-11-10T09:00:00Z": "3h",
		"2026-11-08T12:00:00Z": "2d",
		"":                     "",
	} {
		if got := questionAge(raised, now); got != want {
			t.Errorf("questionAge(%q) = %q, want %q", raised, got, want)
		}
	}
}
