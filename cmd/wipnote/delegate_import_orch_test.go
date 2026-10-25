package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// orchCmdFixture writes a synthetic orch registry whose stored mailbox path
// is stale (the root moved) and the real mailboxes under another root, as the
// default mail root moving twice left them. Returns the project root, the
// registry path and the moved mail root.
func orchCmdFixture(t *testing.T) (string, string, string) {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "") // restores the value after the test
		os.Unsetenv(k)
	}
	project := t.TempDir()
	if err := os.MkdirAll(filepath.Join(project, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	initGitRepo(t, project)
	orch := t.TempDir()
	stale := filepath.Join(orch, "old-root")
	moved := filepath.Join(orch, "new-root")
	reg := `{"v":1,"sessions":{"w1":{"name":"w1","kind":"claude","created_at":1790000000,` +
		`"mailbox":"` + filepath.Join(stale, "w1") + `","state":"done","run_id":"0a0b0c0d","mode":"interactive"}}}`
	writeOrchFile(t, filepath.Join(orch, "registry.json"), reg)
	writeOrchFile(t, filepath.Join(moved, "w1", "events.ndjson"),
		`{"v":1,"seq":1,"ts":1790000010,"session":"w1","kind":"session_start","sid":"sess-w1"}`+"\n")
	return project, filepath.Join(orch, "registry.json"), moved
}

func writeOrchFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runImportOrch(t *testing.T, project string, extra ...string) string {
	t.Helper()
	origFlag := projectDirFlag // --project-dir sets a package global
	t.Cleanup(func() { projectDirFlag = origFlag })
	root := buildRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetErr(&out)
	root.SetArgs(append([]string{"delegate", "import-orch", "--project-dir", project}, extra...))
	if err := root.Execute(); err != nil {
		t.Fatalf("import-orch: %v\n%s", err, out.String())
	}
	return out.String()
}

// TestDelegateImportOrchCmd_MailRootFlag runs the command end to end: with
// --mail-root the moved mailbox is read, the row lands in the project's
// ledger, and a rerun adds nothing.
func TestDelegateImportOrchCmd_MailRootFlag(t *testing.T) {
	project, reg, moved := orchCmdFixture(t)
	t.Setenv("ORCH_REGISTRY", reg)
	t.Setenv("ORCH_MAIL_ROOT", t.TempDir())
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "none")
	t.Cleanup(func() { delegationledger.OnCommit = nil })

	out := runImportOrch(t, project, "--mail-root", moved)
	if !strings.Contains(out, "imported 1") {
		t.Fatalf("output %q, want one run imported", out)
	}
	recs, err := delegationledger.NewStore(filepath.Join(project, ".wipnote")).ReadAll()
	if err != nil || len(recs) != 1 {
		t.Fatalf("ledger rows %d err %v, want 1", len(recs), err)
	}
	if got := strings.Join(recs[0].WorkerSessions, " "); got != "sess-w1" {
		t.Fatalf("worker sessions %q, want sess-w1 read from --mail-root", got)
	}
	if out := runImportOrch(t, project, "--mail-root", moved); !strings.Contains(out, "imported 0") {
		t.Fatalf("rerun output %q, want nothing imported", out)
	}
}

// TestDelegationLedger_CommitSeamQueuesIntent pins the seam wiring: the first
// cmd writer of the delegations ledger must reach the commit queue, as the
// gate ledger does. Two-sided: no seam, no intent.
func TestDelegationLedger_CommitSeamQueuesIntent(t *testing.T) {
	project, reg, moved := orchCmdFixture(t)
	t.Setenv("ORCH_REGISTRY", reg)
	outbox := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(outbox, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig; delegationledger.OnCommit = nil })
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", string(workitemArtifactCommitPolicyDefer))

	runImportOrch(t, project, "--mail-root", moved)
	ob, err := openCommitOutbox(project)
	if err != nil {
		t.Fatalf("openCommitOutbox: %v", err)
	}
	pending, err := ob.Pending()
	if err != nil {
		t.Fatalf("Pending: %v", err)
	}
	found := false
	for _, in := range pending {
		for _, p := range in.RelPaths {
			found = found || p == ".wipnote/"+delegationledger.FileName
		}
	}
	if !found {
		t.Fatalf("no commit intent for the delegations ledger among %d intents", len(pending))
	}

	// Two-sided: a write with the seam removed queues nothing.
	delegationledger.OnCommit = nil
	extra := delegationledger.Record{ID: "dlg-0000000000aa", Mode: delegationledger.ModeOneshot,
		Harness: "codex", EndReason: delegationledger.EndImported}
	if _, err := delegationledger.NewStore(filepath.Join(project, ".wipnote")).Open(extra); err != nil {
		t.Fatalf("Open: %v", err)
	}
	after, _ := ob.Pending()
	if len(after) != len(pending) {
		t.Fatalf("a write without the seam queued an intent (%d -> %d)", len(pending), len(after))
	}
}
