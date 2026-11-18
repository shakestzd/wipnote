package main

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/gateledger"
	"github.com/shakestzd/wipnote/core/models"
)

const diffRemovesGoTest = `diff --git a/pkg/a_test.go b/pkg/a_test.go
index 111..222 100644
--- a/pkg/a_test.go
+++ b/pkg/a_test.go
@@ -1,9 +1,3 @@
 package pkg
-
-func TestOld(t *testing.T) {
-	if 1 != 1 {
-		t.Fatal("x")
-	}
-}
-
 func TestKept(t *testing.T) {}
`

// A removed test counts once (slice 10 done-when 1).
func TestWeakenedTests_CountsRemovedGoTest(t *testing.T) {
	ev := parseEvidenceDiff(diffRemovesGoTest)
	if ev.RemovedTests != 1 {
		t.Errorf("RemovedTests = %d, want 1", ev.RemovedTests)
	}
	if ev.AddedSkips != 0 {
		t.Errorf("AddedSkips = %d, want 0", ev.AddedSkips)
	}
}

// A test whose signature changes is removed and re-added: it is not weakened.
func TestWeakenedTests_RenamedSignatureIsNotRemoval(t *testing.T) {
	diff := "--- a/a_test.go\n+++ b/a_test.go\n@@ -1 +1 @@\n-func TestX(t *testing.T) {\n+func TestX(t *testing.T) { // changed\n"
	if got := parseEvidenceDiff(diff).RemovedTests; got != 0 {
		t.Errorf("RemovedTests = %d, want 0", got)
	}
}

// An added t.Skip counts once (done-when 2). JS and Python skips count too.
func TestWeakenedTests_CountsAddedSkip(t *testing.T) {
	goDiff := "--- a/a_test.go\n+++ b/a_test.go\n@@ -1,2 +1,3 @@\n func TestA(t *testing.T) {\n+\tt.Skip(\"flaky\")\n }\n"
	if got := parseEvidenceDiff(goDiff).AddedSkips; got != 1 {
		t.Errorf("go AddedSkips = %d, want 1", got)
	}
	jsDiff := "--- a/a.test.ts\n+++ b/a.test.ts\n@@ -1 +1 @@\n-it('works', () => {})\n+it.skip('works', () => {})\n"
	js := parseEvidenceDiff(jsDiff)
	if js.AddedSkips != 1 || js.RemovedTests != 0 {
		t.Errorf("js = %+v, want 1 skip and 0 removed", js)
	}
	pyDiff := "--- a/test_a.py\n+++ b/test_a.py\n@@ -1 +1,2 @@\n+@pytest.mark.skip(reason=\"x\")\n def test_a():\n"
	if got := parseEvidenceDiff(pyDiff).AddedSkips; got != 1 {
		t.Errorf("py AddedSkips = %d, want 1", got)
	}
}

// A skip on a removed line, or in a language with no detector, is not counted.
func TestWeakenedTests_IgnoresOtherLines(t *testing.T) {
	diff := "--- a/a_test.go\n+++ b/a_test.go\n@@ -1 +1 @@\n-\tt.Skip(\"old\")\n--- a/x.rb\n+++ b/x.rb\n@@ -1 +1 @@\n+skip 'x'\n-def test_gone\n"
	ev := parseEvidenceDiff(diff)
	if ev.AddedSkips != 0 || ev.RemovedTests != 0 {
		t.Errorf("got %+v, want zero counts", ev)
	}
}

// CI workflow, lint and guard config edits are listed by path (done-when 3).
func TestWeakenedTests_ListsConfigEdits(t *testing.T) {
	diff := "--- a/.github/workflows/ci.yml\n+++ b/.github/workflows/ci.yml\n@@ -1 +1 @@\n-a\n+b\n" +
		"--- a/.golangci.yml\n+++ b/.golangci.yml\n@@ -1 +1 @@\n-a\n+b\n" +
		"--- a/.wipnote/guard-profile.yaml\n+++ b/.wipnote/guard-profile.yaml\n@@ -1 +1 @@\n-a\n+b\n" +
		"--- a/main.go\n+++ b/main.go\n@@ -1 +1 @@\n-a\n+b\n"
	want := []string{".github/workflows/ci.yml", ".golangci.yml", ".wipnote/guard-profile.yaml"}
	if got := parseEvidenceDiff(diff).ConfigEdits; !reflect.DeepEqual(got, want) {
		t.Errorf("ConfigEdits = %v, want %v", got, want)
	}
}

func TestWeakenedTests_WriteEvidenceText(t *testing.T) {
	var buf bytes.Buffer
	writeEvidence(&buf, evidenceBlock{RemovedTests: 1, AddedSkips: 2, ConfigEdits: []string{".github/workflows/ci.yml"}, AddedLines: 7})
	out := buf.String()
	for _, want := range []string{"removed tests: 1", "added skips: 2", ".github/workflows/ci.yml", "added lines: 7"} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
}

// writeFeatureArtifact stores a minimal work item with the description layout
// `wipnote feature create` writes: Why and Acceptance Criteria after the text.
func writeFeatureArtifact(t *testing.T, root, id string) {
	t.Helper()
	body := `<!DOCTYPE html><html><body>
<article id="` + id + `" data-type="feature" data-status="in-progress" data-priority="medium" data-created="2026-11-18T20:07:15" data-updated="2026-11-18T20:07:15">
<header><h1>Sample</h1></header>
<section data-content><h3>Description</h3><p>Do the thing.
Why
Because tests can vanish.
Acceptance Criteria
Count removed tests; List config edits</p></section>
</article></body></html>`
	writeRepoFile(t, root, ".wipnote/features/"+id+".html", body)
}

// The review opens with why, done-when, last gate, linked commits and added
// lines (done-when 4).
func TestReview_EvidenceHeader(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{"a.go": "package p\n"})
	const id = "feat-aaaa1111"
	writeFeatureArtifact(t, dir, id)
	at := time.Date(2026, 11, 18, 12, 0, 0, 0, time.UTC)
	if _, err := gateledger.StoreForProject(dir).Append(gateledger.Record{
		SessionID: "s1", WorkItemID: id, Harness: "claude-code", ProjectType: "go",
		GateCommand: "go test", Status: gateledger.StatusPass, CheckedAt: at, Source: "check",
	}); err != nil {
		t.Fatal(err)
	}
	orig := linkedCommitsFn
	linkedCommitsFn = func(string, string, *models.Node) []string { return []string{"abc1234def5678"} }
	t.Cleanup(func() { linkedCommitsFn = orig })

	h := buildReviewHeader(dir, id, evidenceBlock{AddedLines: 42})
	var buf bytes.Buffer
	writeReviewHeader(&buf, h)
	out := buf.String()
	for _, want := range []string{
		id, "why: Because tests can vanish.", "done when: Count removed tests; List config edits",
		"last gate: pass", "abc1234", "added lines: 42",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("header missing %q:\n%s", want, out)
		}
	}
}

// An item with no id prints no header at all.
func TestReview_EvidenceHeaderEmptyWithoutItem(t *testing.T) {
	var buf bytes.Buffer
	writeReviewHeader(&buf, buildReviewHeader(t.TempDir(), "", evidenceBlock{}))
	if buf.Len() != 0 {
		t.Errorf("want no output, got %q", buf.String())
	}
}

// Evidence lines print and return nothing: the gate cannot fail on them
// (done-when 5). The diff here removes a test and adds a skip.
func TestCheckGate_EvidenceDoesNotFail(t *testing.T) {
	dir := t.TempDir()
	setupTempGitRepo(t, dir, map[string]string{
		"a_test.go": "package p\n\nfunc TestGone(t *testing.T) {}\n\nfunc TestStay(t *testing.T) {}\n",
	})
	writeRepoFile(t, dir, "a_test.go", "package p\n\nfunc TestStay(t *testing.T) { t.Skip(\"x\") }\n")
	var buf bytes.Buffer
	writeGateEvidence(&buf, dir) // no return value: it cannot change the exit code
	out := buf.String()
	if !strings.Contains(out, "removed tests: 1") || !strings.Contains(out, "added skips: 1") {
		t.Errorf("gate evidence missing counts:\n%s", out)
	}
}
