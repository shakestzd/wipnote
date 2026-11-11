package main

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/question"
)

// putQuestions replaces the question record on item with qs.
func putQuestions(t *testing.T, item string, qs ...question.Question) {
	t.Helper()
	p, col, rid, err := openQuestionItem(item)
	if err != nil {
		t.Fatal(err)
	}
	defer p.Close()
	if _, err := updateQuestions(col, rid, func([]question.Question) ([]question.Question, error) {
		return qs, nil
	}); err != nil {
		t.Fatal(err)
	}
}

func prefixQuestion(id string) question.Question {
	return question.Question{ID: id, Text: "Which?", Status: question.StatusOpen,
		Options: []question.Option{{Key: "a", Label: "A"}, {Key: "b", Label: "B"}}}
}

// A unique prefix of a question id resolves to that question, with or
// without the q- marker, so the owner need not type all 16 hex digits.
func TestAnswer_UniquePrefixResolves(t *testing.T) {
	for _, in := range []string{"q-aaaa11", "aaaa11"} {
		t.Run(in, func(t *testing.T) {
			_, hgDir := prepProject(t)
			id := createItem(t, hgDir, "feature", "One", testSetupTrack(t, hgDir))
			putQuestions(t, id, prefixQuestion("q-aaaa112222333344"), prefixQuestion("q-bbbb112222333344"))
			if _, err := answerQuestion(in, "a", "", false); err != nil {
				t.Fatalf("answer by prefix %q: %v", in, err)
			}
			if got := questionByID(t, hgDir, id, "q-aaaa112222333344"); got.Answer != "a" {
				t.Fatalf("prefix did not answer the matching question: %+v", got)
			}
			if got := questionByID(t, hgDir, id, "q-bbbb112222333344"); got.Answer != "" {
				t.Fatalf("prefix answered another question: %+v", got)
			}
		})
	}
}

// A prefix shared by questions on two items is refused, naming every match,
// and nothing is recorded.
func TestAnswer_AmbiguousPrefixRefused(t *testing.T) {
	_, hgDir := prepProject(t)
	track := testSetupTrack(t, hgDir)
	one := createItem(t, hgDir, "feature", "One", track)
	two := createItem(t, hgDir, "bug", "Two", track)
	putQuestions(t, one, prefixQuestion("q-cccc110000000001"))
	putQuestions(t, two, prefixQuestion("q-cccc110000000002"))
	_, err := answerQuestion("q-cccc11", "a", "", false)
	if err == nil || !strings.Contains(err.Error(), "q-cccc110000000001") || !strings.Contains(err.Error(), "q-cccc110000000002") {
		t.Fatalf("an ambiguous prefix must be refused naming both questions, got %v", err)
	}
	if got := questionByID(t, hgDir, one, "q-cccc110000000001"); got.Answer != "" {
		t.Fatalf("ambiguous prefix answered %s: %+v", one, got)
	}
}

// A prefix shorter than four hex digits is refused rather than guessed.
func TestAnswer_TooShortPrefixRefused(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "One", testSetupTrack(t, hgDir))
	putQuestions(t, id, prefixQuestion("q-dddd112222333344"))
	if _, err := answerQuestion("q-dd", "a", "", false); err == nil {
		t.Fatal("a two-digit prefix was accepted")
	}
}

// An exact id wins over a longer id it happens to prefix, so an old 8-hex
// id stays answerable next to a 16-hex one that starts the same way.
func TestAnswer_ExactIDBeatsPrefix(t *testing.T) {
	_, hgDir := prepProject(t)
	id := createItem(t, hgDir, "feature", "One", testSetupTrack(t, hgDir))
	putQuestions(t, id, prefixQuestion("q-eeee1111"), prefixQuestion("q-eeee111122223333"))
	if _, err := answerQuestion("q-eeee1111", "b", "", false); err != nil {
		t.Fatalf("exact id: %v", err)
	}
	if got := questionByID(t, hgDir, id, "q-eeee1111"); got.Answer != "b" {
		t.Fatalf("exact id not answered: %+v", got)
	}
}
