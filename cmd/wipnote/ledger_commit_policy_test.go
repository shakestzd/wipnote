package main

import (
	"os"
	"path/filepath"
	"testing"
)

// ledgerPersistCases names every ledger OnCommit seam that turns a ledger write
// into a git commit or a queued commit intent.
var ledgerPersistCases = []struct {
	name    string
	persist func(wipnoteDir, relPath, action string)
	relPath string
}{
	{"run", persistRunLedgerWrite, ".wipnote/runs/sess-a.ndjson"},
	{"claim", persistClaimLedgerWrite, ".wipnote/claims/sess-a.html"},
	{"session", persistSessionLedgerWrite, ".wipnote/sessions-ledger.html"},
	{"delegation", persistDelegationLedgerWrite, ".wipnote/delegations-ledger.html"},
	{"gate", persistGateLedgerWrite, ".wipnote/gate-ledger.html"},
}

// ledgerPolicyRepo builds a real git repo outside any test-scratch path guard,
// writes relPath into it and points the commit outbox at a private file.
func ledgerPolicyRepo(t *testing.T, relPath string) (repoRoot, wipnoteDir string) {
	t.Helper()
	tmpDir, err := os.MkdirTemp("/tmp", "wipnote-ledger-policy-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpDir) })
	repoRoot = setupWorktreeGitRepoIn(t, tmpDir)
	gitMustCommitInitial(t, repoRoot)
	abs := filepath.Join(repoRoot, relPath)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(abs, []byte("row\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) {
		return filepath.Join(tmpDir, "commit-outbox.ndjson"), nil
	}
	t.Cleanup(func() { commitOutboxPath = orig })
	// The SessionEnd hook sets light mode process-wide; a hook test run in this
	// binary leaves it on, so pin it off unless a test turns it on itself.
	wasLight := commitIntentLight.Swap(false)
	t.Cleanup(func() { commitIntentLight.Store(wasLight) })
	return repoRoot, filepath.Join(repoRoot, ".wipnote")
}

func pendingIntentCount(t *testing.T, repoRoot string) int {
	t.Helper()
	ob, err := openCommitOutbox(repoRoot)
	if err != nil {
		t.Fatalf("openCommitOutbox: %v", err)
	}
	pending, err := ob.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	return len(pending)
}

// TestPersistLedgerWrites_HonourArtifactCommitPolicy (bug-37c5cac2): "none"
// neither commits nor queues, "defer" queues without committing, and the legacy
// "separate" policy commits directly — for every ledger seam.
func TestPersistLedgerWrites_HonourArtifactCommitPolicy(t *testing.T) {
	policies := []struct {
		policy                   string
		wantCommits, wantIntents int
	}{
		{"none", 0, 0},
		{"defer", 0, 1},
		{"separate", 1, 0},
	}
	for _, lc := range ledgerPersistCases {
		for _, p := range policies {
			t.Run(lc.name+"/"+p.policy, func(t *testing.T) {
				repoRoot, wipnoteDir := ledgerPolicyRepo(t, lc.relPath)
				t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", p.policy)
				before := gitCommitCount(t, repoRoot)

				lc.persist(wipnoteDir, lc.relPath, "test write")

				if got := gitCommitCount(t, repoRoot) - before; got != p.wantCommits {
					t.Errorf("commits = %d, want %d", got, p.wantCommits)
				}
				if got := pendingIntentCount(t, repoRoot); got != p.wantIntents {
					t.Errorf("queued intents = %d, want %d", got, p.wantIntents)
				}
			})
		}
	}
}

// Light mode (the SessionEnd hook) must not resurrect queueing under "none".
func TestPersistLedgerWrites_NonePolicySkipsQueueInLightMode(t *testing.T) {
	for _, lc := range ledgerPersistCases {
		t.Run(lc.name, func(t *testing.T) {
			repoRoot, wipnoteDir := ledgerPolicyRepo(t, lc.relPath)
			t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "none")
			commitIntentLight.Store(true)
			t.Cleanup(func() { commitIntentLight.Store(false) })
			before := gitCommitCount(t, repoRoot)

			lc.persist(wipnoteDir, lc.relPath, "test write")

			if got := gitCommitCount(t, repoRoot) - before; got != 0 {
				t.Errorf("commits = %d, want 0", got)
			}
			if got := pendingIntentCount(t, repoRoot); got != 0 {
				t.Errorf("queued intents = %d, want 0", got)
			}
		})
	}
}
