package main

import (
	"testing"

	"github.com/shakestzd/wipnote/core/workitem"
)

// TestIsWorkItemPrefix_GeneratedIDs pins that `plan generate <id>` treats
// every work-item id GenerateID mints as an id (retroactive mode), including
// spec ids, which are "spec-…", not "spc-…"; free text stays a topic.
func TestIsWorkItemPrefix_GeneratedIDs(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "spike", "track", "spec"} {
		if id := workitem.GenerateID(kind, "title"); !isWorkItemPrefix(id) {
			t.Errorf("isWorkItemPrefix(%q) = false, want true", id)
		}
	}
	if isWorkItemPrefix("speculative caching") {
		t.Error("free-text topic treated as a work-item id")
	}
}
