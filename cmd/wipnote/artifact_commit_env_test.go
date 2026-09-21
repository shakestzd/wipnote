package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
)

func TestIsEnvironmentalOutboxError(t *testing.T) {
	pathErr := func(errno syscall.Errno) error {
		return &fs.PathError{Op: "open", Path: "/cache/commit-outbox.ndjson.lock", Err: errno}
	}
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"EACCES", pathErr(syscall.EACCES), true},
		{"EPERM", pathErr(syscall.EPERM), true},
		{"EROFS", pathErr(syscall.EROFS), true},
		{"wrapped EPERM", fmt.Errorf("artifact commit defer: record intent: commitqueue: open lock: %w", pathErr(syscall.EPERM)), true},
		{"ENOSPC", pathErr(syscall.ENOSPC), false},
		{"generic", errors.New("commitqueue: intent missing rel_paths"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isEnvironmentalOutboxError(tc.err); got != tc.want {
				t.Fatalf("isEnvironmentalOutboxError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestWorkitemArtifactCommitPolicyForEnv_None(t *testing.T) {
	for _, raw := range []string{"none", "NONE", " none "} {
		t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", raw)
		if got := workitemArtifactCommitPolicyForEnv(); got != workitemArtifactCommitPolicyNone {
			t.Fatalf("policy %q = %q, want %q", raw, got, workitemArtifactCommitPolicyNone)
		}
	}
}

// TestPersistWorkitemArtifactTransition_NoneSkipsQueueAndCommit pins the
// documented no-queue path (GH#149): policy=none neither commits nor records
// an intent, and says so on stderr.
func TestPersistWorkitemArtifactTransition_NoneSkipsQueueAndCommit(t *testing.T) {
	tmpDir, err := os.MkdirTemp("/tmp", "wipnote-policy-none-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	repoRoot := setupWorktreeGitRepoIn(t, tmpDir)
	gitMustCommitInitial(t, repoRoot)
	wipnoteDir := filepath.Join(repoRoot, ".wipnote")
	const featureID = "feat-policy-none"
	if err := os.MkdirAll(filepath.Join(wipnoteDir, "features"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wipnoteDir, "features", featureID+".html"),
		[]byte(`<article id="`+featureID+`" data-status="done"></article>`), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(tmpDir, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig })
	var errBuf bytes.Buffer
	origStderr := stderr
	stderr = &errBuf
	t.Cleanup(func() { stderr = origStderr })
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "none")

	before := gitCommitCount(t, repoRoot)
	if err := persistWorkitemArtifactTransition(wipnoteDir, "feature", featureID, "complete"); err != nil {
		t.Fatalf("policy=none must succeed, got: %v", err)
	}
	if after := gitCommitCount(t, repoRoot); after != before {
		t.Fatalf("policy=none must not commit, count %d -> %d", before, after)
	}
	ob, _ := openCommitOutbox(repoRoot)
	if depth, _ := ob.Depth(); depth != 0 {
		t.Fatalf("policy=none must not queue an intent, depth = %d", depth)
	}
	if !strings.Contains(errBuf.String(), "WIPNOTE_ARTIFACT_COMMIT_POLICY=none") {
		t.Fatalf("expected a skip notice on stderr, got %q", errBuf.String())
	}
}

// captureOSStderr redirects os.Stderr for fn and returns what was written.
func captureOSStderr(t *testing.T, fn func()) string {
	t.Helper()
	origStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	os.Stderr = w
	fn()
	_ = w.Close()
	os.Stderr = origStderr
	b, readErr := io.ReadAll(r)
	_ = r.Close()
	if readErr != nil {
		t.Fatalf("ReadAll(stderr): %v", readErr)
	}
	return string(b)
}

// TestDeferredComplete_EnvironmentalOutboxErrorKeepsItemDone is the GH#149
// regression: an EPERM from the outbox enqueue (sandboxed cache dir) must NOT
// reopen a completed item — the canonical artifact is already written. The
// command returns nil, the item stays done, and a pending-sync warning is
// printed.
func TestDeferredComplete_EnvironmentalOutboxErrorKeepsItemDone(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real git-backed work-item completion")
	}
	_, wipnoteDir, featID, sessionID, agentID := setupTransactionalCompleteRepo(t)
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "defer")

	orig := persistArtifactTransitionFn
	t.Cleanup(func() { persistArtifactTransitionFn = orig })
	persistArtifactTransitionFn = func(_, _, id, _ string) error {
		return fmt.Errorf("artifact commit defer: record intent for %s: commitqueue: open lock: %w", id,
			&fs.PathError{Op: "open", Path: "/Users/x/Library/Caches/wipnote/h/commit-outbox.ndjson.lock", Err: syscall.EPERM})
	}

	var runErr error
	stderrText := captureOSStderr(t, func() {
		runErr = wiSetStatusWithAgent("feature", featID, "done", sessionID, agentID)
	})
	if runErr != nil {
		t.Fatalf("environmental outbox failure must not fail completion, got: %v", runErr)
	}
	if got := diskStatus(t, wipnoteDir, featID); got != "done" {
		t.Fatalf("on-disk status = %q, want done (no compensating re-open)", got)
	}
	if !strings.Contains(stderrText, "commit-queue unavailable") || !strings.Contains(stderrText, "artifact persisted") {
		t.Fatalf("expected pending-sync warning on stderr, got:\n%s", stderrText)
	}
}

// TestDeferredComplete_RealOutboxErrorStillReopens keeps the compensating
// re-open for NON-environmental queue failures so GH#149 does not weaken the
// transactional guarantee for real problems.
func TestDeferredComplete_RealOutboxErrorStillReopens(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real git-backed work-item completion")
	}
	_, wipnoteDir, featID, sessionID, agentID := setupTransactionalCompleteRepo(t)
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "defer")

	orig := persistArtifactTransitionFn
	t.Cleanup(func() { persistArtifactTransitionFn = orig })
	persistArtifactTransitionFn = func(_, _, _, _ string) error {
		return errors.New("commitqueue: parse intent line: unexpected EOF")
	}

	var runErr error
	_ = captureOSStderr(t, func() {
		runErr = wiSetStatusWithAgent("feature", featID, "done", sessionID, agentID)
	})
	if runErr == nil || !strings.Contains(runErr.Error(), "completion aborted") {
		t.Fatalf("real queue failure must abort completion, got: %v", runErr)
	}
	if got := diskStatus(t, wipnoteDir, featID); got != "in-progress" {
		t.Fatalf("on-disk status = %q, want in-progress (compensating re-open)", got)
	}
}

// TestDeferredComplete_ReadOnlyCacheDirKeepsItemDone is the acceptance test
// from GH#149: the outbox directory is genuinely read-only while the repo is
// writable. It is skipped when the process can write there anyway (root).
func TestDeferredComplete_ReadOnlyCacheDirKeepsItemDone(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real git-backed work-item completion")
	}
	_, wipnoteDir, featID, sessionID, agentID := setupTransactionalCompleteRepo(t)
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "defer")

	roDir := filepath.Join(t.TempDir(), "cache")
	if err := os.MkdirAll(roDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(roDir, 0o700) })
	if f, err := os.OpenFile(filepath.Join(roDir, "probe"), os.O_CREATE|os.O_WRONLY, 0o644); err == nil {
		_ = f.Close()
		t.Skip("read-only directory is still writable (running as root)")
	}
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(roDir, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig })

	var runErr error
	stderrText := captureOSStderr(t, func() {
		runErr = wiSetStatusWithAgent("feature", featID, "done", sessionID, agentID)
	})
	if runErr != nil {
		t.Fatalf("read-only outbox dir must not fail completion, got: %v", runErr)
	}
	if got := diskStatus(t, wipnoteDir, featID); got != "done" {
		t.Fatalf("on-disk status = %q, want done", got)
	}
	if !strings.Contains(stderrText, "commit-queue unavailable") {
		t.Fatalf("expected pending-sync warning, got:\n%s", stderrText)
	}
}
