package main

import (
	"testing"

	"github.com/shakestzd/wipnote/core/models"
)

func TestReopen_BugAndSpike(t *testing.T) {
	for _, typeName := range []string{"bug", "spike"} {
		t.Run(typeName, func(t *testing.T) {
			_, hgDir := prepProject(t)
			trackID := testSetupTrack(t, hgDir)
			id := createItem(t, hgDir, typeName, "Reopen me", trackID)

			if err := executeReopen(typeName, id); err == nil {
				t.Fatalf("reopening a %s that is not done must fail", typeName)
			}
			forceStatus(t, hgDir, typeName, id, models.StatusInProgress)
			forceStatus(t, hgDir, typeName, id, models.StatusDone)
			if err := executeReopen(typeName, id); err != nil {
				t.Fatalf("reopen %s: %v", typeName, err)
			}
			if st := parseItem(t, hgDir, id).Status; st != models.StatusInProgress {
				t.Fatalf("status after reopen = %q, want in-progress", st)
			}
		})
	}
}

func TestReopen_RefusesWrongType(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "bug", "A bug", trackID)
	if err := executeReopen("spike", id); err == nil {
		t.Fatal("spike reopen on a bug id must be refused")
	}
}

func TestReopen_VerbsRegistered(t *testing.T) {
	root := buildRoot()
	for _, typeName := range []string{"feature", "bug", "spike"} {
		cmd, _, err := root.Find([]string{typeName, "reopen"})
		if err != nil || cmd == nil || cmd.Name() != "reopen" {
			t.Errorf("%s reopen not registered: %v", typeName, err)
		}
	}
}
