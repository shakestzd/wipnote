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
