package main

import (
	"reflect"
	"testing"
)

func TestExtractCommitItems_RefsAndInlineID(t *testing.T) {
	body := "fix: thing (feat-abc12345)\n\nbody\n\nRefs: bug-def45678\n"
	got := extractCommitItems(body)
	want := []string{"feat-abc12345", "bug-def45678"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extractCommitItems = %v, want %v", got, want)
	}
}
