package workitem

import "testing"

func TestCollectionDirForID_Spec(t *testing.T) {
	if got := collectionDirForID("spec-12345678"); got != "specs" {
		t.Fatalf("collectionDirForID(spec-...) = %q, want specs", got)
	}
}

func TestKindFromPrefix_Spec(t *testing.T) {
	if got := kindFromPrefix("spec-12345678"); got != "spec" {
		t.Fatalf("kindFromPrefix(spec-...) = %q, want spec", got)
	}
}

// TestKindFromPrefix_GeneratedIDs pins that kindFromPrefix names every kind
// GenerateID mints. Plan ids are "plan-…" (it only tested the legacy "pln-"),
// so a missing plan was reported as a "work item".
func TestKindFromPrefix_GeneratedIDs(t *testing.T) {
	for _, kind := range []string{"feature", "bug", "spike", "track", "plan", "spec"} {
		id := GenerateID(kind, "title")
		if got := kindFromPrefix(id); got != kind {
			t.Errorf("kindFromPrefix(%q) = %q, want %q", id, got, kind)
		}
	}
	if got := kindFromPrefix("zzz-1234abcd"); got != "work item" {
		t.Errorf("unknown prefix kind = %q, want fallback", got)
	}
}
