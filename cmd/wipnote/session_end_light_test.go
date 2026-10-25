package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// stubGitOnPath puts a git first on PATH that logs each call and succeeds.
func stubGitOnPath(t *testing.T) (logPath string) {
	t.Helper()
	bin := t.TempDir()
	logPath = filepath.Join(bin, "git.log")
	script := "#!/bin/sh\necho \"$*\" >> " + logPath + "\nexit 0\n"
	if err := os.WriteFile(filepath.Join(bin, "git"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	return logPath
}

func gitLogLines(logPath string) []string {
	data, err := os.ReadFile(logPath)
	if err != nil || len(strings.TrimSpace(string(data))) == 0 {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// In the SessionEnd hook process the ledger commit intents are enqueued with no
// git call at all (bug-1daed6d6): `commit-queue flush` does the git work. The
// same call in any other process keeps its isGitRepo and check-ignore guards,
// so the guards are not silently gone.
func TestEnqueueLedgerCommitIntent_LightModeSpawnsNoGit(t *testing.T) {
	project := t.TempDir()
	wipnoteDir := filepath.Join(project, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outbox := filepath.Join(t.TempDir(), "commit-outbox.ndjson")
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return outbox, nil }
	t.Cleanup(func() { commitOutboxPath = orig })
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "")
	gitLog := stubGitOnPath(t)
	const rel = ".wipnote/claims/sess-x.html"

	t.Run("default mode probes git", func(t *testing.T) {
		commitIntentLight.Store(false)
		if err := enqueueLedgerCommitIntent(wipnoteDir, rel, "msg", "claim", "claim ledger commit defer"); err != nil {
			t.Fatal(err)
		}
		if len(gitLogLines(gitLog)) == 0 {
			t.Fatal("default mode no longer guards with git; the light-mode test proves nothing")
		}
	})
	t.Run("light mode spawns none and still enqueues", func(t *testing.T) {
		_ = os.Remove(gitLog)
		_ = os.Remove(outbox)
		commitIntentLight.Store(true)
		t.Cleanup(func() { commitIntentLight.Store(false) })
		if err := enqueueLedgerCommitIntent(wipnoteDir, rel, "msg", "claim", "claim ledger commit defer"); err != nil {
			t.Fatal(err)
		}
		if calls := gitLogLines(gitLog); len(calls) != 0 {
			t.Fatalf("light mode spawned git: %v", calls)
		}
		data, err := os.ReadFile(outbox)
		if err != nil || !strings.Contains(string(data), rel) {
			t.Fatalf("intent not enqueued: err=%v outbox=%q", err, data)
		}
	})
}

// The persist entry points that the ledger OnCommit seams call must honour light
// mode even under the legacy "separate" policy, which would otherwise commit
// directly (two git calls). The cancel test drives the real hook command.
func TestPersistLedgerWrites_UseLightModeWhenSet(t *testing.T) {
	project := t.TempDir()
	wipnoteDir := filepath.Join(project, ".wipnote")
	if err := os.MkdirAll(wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	outbox := filepath.Join(t.TempDir(), "commit-outbox.ndjson")
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return outbox, nil }
	t.Cleanup(func() { commitOutboxPath = orig })
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "separate") // legacy: would commit directly
	gitLog := stubGitOnPath(t)
	commitIntentLight.Store(true)
	t.Cleanup(func() { commitIntentLight.Store(false) })

	persistClaimLedgerWrite(wipnoteDir, ".wipnote/claims/sess-y.html", "release")
	persistSessionLedgerWrite(wipnoteDir, ".wipnote/sessions-ledger.html", "session end")

	if calls := gitLogLines(gitLog); len(calls) != 0 {
		t.Fatalf("SessionEnd ledger writes spawned git: %v", calls)
	}
	data, _ := os.ReadFile(outbox)
	if !strings.Contains(string(data), "sess-y.html") || !strings.Contains(string(data), "sessions-ledger.html") {
		t.Fatalf("both ledger intents should be enqueued, outbox=%q", data)
	}
}
