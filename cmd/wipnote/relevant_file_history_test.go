package main

import (
	"path/filepath"
	"testing"
)

// TestSearchViaGitFileHistory_AttributesSubjectRef pins that a file query
// credits a commit to the work item named inline in its subject, the way
// wipnote commits name items ("fix(x): … (feat-…)"). The log format used to
// take only %b, the body without the subject, so such commits were never
// cited, while the SHA query (%B) cited them.
func TestSearchViaGitFileHistory_AttributesSubjectRef(t *testing.T) {
	dir := t.TempDir()
	initGitRepoWithMessage(t, dir, map[string]string{"a.go": "package a\n"},
		"fix(a): stop the leak (feat-abc12345)")
	scores := map[string]*relevantResult{}
	if err := searchViaGitFileHistory(dir, filepath.Join(dir, ".wipnote"), "a.go", scores); err != nil {
		t.Fatal(err)
	}
	r, ok := scores["feat-abc12345"]
	if !ok || len(r.Citations) != 1 {
		t.Fatalf("subject ref not attributed exactly once: %+v", scores)
	}
	if len(scores) != 1 {
		t.Errorf("attributed unrelated items: %+v", scores)
	}
}
