package main

import (
	"testing"

	"github.com/shakestzd/wipnote/core/workitem"
)

// TestKindFromPrefix_GeneratedIDs pins that the cmd-side kindFromPrefix
// names every kind GenerateID mints, including "spec-" and "plan-" (it used
// to test "spc-" and only "pln-"), and keeps the fallback for unknown ids.
func TestKindFromPrefix_GeneratedIDs(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "spike", "track", "plan", "spec"} {
		id := workitem.GenerateID(kind, "title")
		if got := kindFromPrefix(id); got != kind {
			t.Errorf("kindFromPrefix(%q) = %q, want %q", id, got, kind)
		}
	}
	if got := kindFromPrefix("zzz-1234abcd"); got != "work item" {
		t.Errorf("unknown prefix kind = %q, want fallback", got)
	}
}

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
