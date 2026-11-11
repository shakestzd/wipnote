package main

import (
	"testing"
)

func TestExecutePreview_ListsOpenBlockingQuestions(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	featID := createItem(t, hgDir, "feature", "Owner questions", trackID)
	bugID := createItem(t, hgDir, "bug", "Broken", trackID)
	blocking := addQuestionWith(t, featID, "a", true)
	answered := addQuestionWith(t, bugID, "a", true)
	addQuestionWith(t, bugID, "b", false) // non-blocking: not listed
	if _, err := answerQuestion(answered.ID, "a", "", false); err != nil {
		t.Fatal(err)
	}

	preview, err := buildExecutePreview(hgDir, trackID)
	if err != nil {
		t.Fatalf("buildExecutePreview: %v", err)
	}
	if len(preview.Features) == 0 || len(preview.Bugs) == 0 {
		t.Fatalf("fixture items not linked to the track: %+v", preview)
	}
	if len(preview.Questions) != 1 {
		t.Fatalf("want one open blocking question, got %+v", preview.Questions)
	}
	got := preview.Questions[0]
	if got.ItemID != featID || got.QuestionID != blocking.ID || !got.Blocking || got.Status != "open" {
		t.Fatalf("listed %+v", got)
	}
}

func TestExecutePreview_QuestionsIsEmptyListNotNull(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	createItem(t, hgDir, "feature", "No questions", trackID)
	preview, err := buildExecutePreview(hgDir, trackID)
	if err != nil {
		t.Fatal(err)
	}
	if preview.Questions == nil {
		t.Fatal("questions must be an empty list so JSON readers can rely on it")
	}
}
