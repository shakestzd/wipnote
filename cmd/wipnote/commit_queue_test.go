package main

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// TestRecordCommitIntentAppendsAfterCanonicalWrite verifies the producer half
// of the outbox model: recording an intent appends to the outbox without
// touching git. The outbox path seam is redirected to a temp dir.
func TestRecordCommitIntentAppendsAfterCanonicalWrite(t *testing.T) {
	tmp := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) {
		return filepath.Join(tmp, "commit-outbox.ndjson"), nil
	}
	t.Cleanup(func() { commitOutboxPath = orig })

	if err := recordCommitIntent("/repo",
		[]string{".wipnote/features/feat-1.html"},
		"wipnote: complete feat-1", "feat-1", "complete"); err != nil {
		t.Fatalf("recordCommitIntent: %v", err)
	}

	ob, err := openCommitOutbox("/repo")
	if err != nil {
		t.Fatalf("openCommitOutbox: %v", err)
	}
	pending, err := ob.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	if len(pending) != 1 {
		t.Fatalf("expected 1 pending intent, got %d", len(pending))
	}
	if pending[0].WorkItemID != "feat-1" || pending[0].Message != "wipnote: complete feat-1" {
		t.Fatalf("intent fields wrong: %+v", pending[0])
	}
}

// TestOutboxCommitterIsIdempotentForNonGit ensures the production committer
// treats a non-git repo root as a benign no-op (success) so it does not poison
// the queue.
func TestOutboxCommitterIsIdempotentForNonGit(t *testing.T) {
	// t.TempDir() is not a git repo.
	intent := commitqueue.Intent{
		RepoRoot: t.TempDir(),
		RelPaths: []string{".wipnote/features/feat-1.html"},
		Message:  "wipnote: complete feat-1",
	}
	if err := outboxCommitter(intent); err != nil {
		t.Fatalf("outboxCommitter on non-git root should be a no-op, got: %v", err)
	}
}

func TestOutboxCommitterRejectsInvalidIntentBeforeGit(t *testing.T) {
	intent := commitqueue.Intent{
		RepoRoot: t.TempDir(),
		RelPaths: []string{"."},
		Message:  "wipnote: complete feat-1",
	}
	if err := outboxCommitter(intent); err == nil {
		t.Fatal("outboxCommitter should reject invalid intents before constructing git pathspecs")
	}
}

// runCommitQueueFlushCmd executes `commit-queue flush` with the given args
// and returns captured stdout plus any error.
func runCommitQueueFlushCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := commitQueueFlushCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// runCommitQueueStatusCmd executes `commit-queue status` with the given args
// and returns captured stdout plus any error.
func runCommitQueueStatusCmd(t *testing.T, args ...string) (string, error) {
	t.Helper()
	var buf bytes.Buffer
	cmd := commitQueueStatusCmd()
	cmd.SetOut(&buf)
	cmd.SetArgs(args)
	err := cmd.Execute()
	return buf.String(), err
}

// TestCommitQueueFlush_PrintsPerIntentFailure pins GH#174 step 2: an intent
// that fails but stays under MaxAttempts (so `flush` reports failed=1, not
// dead-lettered=1) must still print WHY, not just the bare count.
func TestCommitQueueFlush_PrintsPerIntentFailure(t *testing.T) {
	repoRoot := setupWorktreeGitRepo(t)
	tmp := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) {
		return filepath.Join(tmp, "commit-outbox.ndjson"), nil
	}
	t.Cleanup(func() { commitOutboxPath = orig })
	origProjectDir := projectDirFlag
	projectDirFlag = repoRoot
	t.Cleanup(func() { projectDirFlag = origProjectDir })

	ob, err := openCommitOutbox(repoRoot)
	if err != nil {
		t.Fatalf("openCommitOutbox: %v", err)
	}
	// A missing artifact file: git add fails, the intent stays under
	// MaxAttempts (default 5), so it is "failed" this pass, not dead-lettered.
	if err := ob.Append(commitqueue.Intent{
		RepoRoot:   repoRoot,
		RelPaths:   []string{".wipnote/features/feat-missing.html"},
		Message:    "wipnote: complete feat-missing",
		WorkItemID: "feat-missing",
		Action:     "complete",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	out, err := runCommitQueueFlushCmd(t)
	if err != nil {
		t.Fatalf("commit-queue flush: %v", err)
	}
	if !strings.Contains(out, "failed=1") {
		t.Fatalf("expected failed=1 in summary, got: %q", out)
	}
	if !strings.Contains(out, "failed (attempt 1): feat-missing:") {
		t.Fatalf("expected a per-intent failure line naming feat-missing, got: %q", out)
	}
}

// TestCommitQueueStatusVerbose_ListsAttemptsAndLastError pins GH#174 step 3:
// `status --verbose` must list each pending intent with its attempt count and
// last recorded error, not just the bare depth `status` already prints.
func TestCommitQueueStatusVerbose_ListsAttemptsAndLastError(t *testing.T) {
	tmp := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) {
		return filepath.Join(tmp, "commit-outbox.ndjson"), nil
	}
	t.Cleanup(func() { commitOutboxPath = orig })

	repoRoot := t.TempDir()
	ob, err := openCommitOutbox(repoRoot)
	if err != nil {
		t.Fatalf("openCommitOutbox: %v", err)
	}
	_ = ob.Append(commitqueue.Intent{
		RepoRoot:   repoRoot,
		RelPaths:   []string{".wipnote/features/feat-flaky.html"},
		Message:    "wipnote: complete feat-flaky",
		WorkItemID: "feat-flaky",
	})
	if _, err := ob.Flush(func(commitqueue.Intent) error {
		return fmt.Errorf("index locked")
	}, 5); err != nil {
		t.Fatalf("seed flush: %v", err)
	}

	// Plain status: no per-intent detail.
	plain, err := runCommitQueueStatusCmd(t)
	if err != nil {
		t.Fatalf("commit-queue status: %v", err)
	}
	if strings.Contains(plain, "index locked") {
		t.Fatalf("plain status must not leak per-intent detail, got: %q", plain)
	}

	verbose, err := runCommitQueueStatusCmd(t, "--verbose")
	if err != nil {
		t.Fatalf("commit-queue status --verbose: %v", err)
	}
	if !strings.Contains(verbose, "feat-flaky: attempts=1") || !strings.Contains(verbose, "index locked") {
		t.Fatalf("expected verbose status to show attempts and last error, got: %q", verbose)
	}
}

// TestIsNothingToCommit covers the idempotency string match used to treat an
// already-committed artifact as success.
func TestIsNothingToCommit(t *testing.T) {
	cases := map[string]bool{
		"nothing to commit, working tree clean": true,
		"no changes added to commit":            true,
		"fatal: index locked":                   false,
	}
	for out, want := range cases {
		if got := isNothingToCommit(out); got != want {
			t.Fatalf("isNothingToCommit(%q) = %v, want %v", out, got, want)
		}
	}
}
