package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// clearHookEnv removes what wipnote's hooks would have exported
// (SessionStart through CLAUDE_ENV_FILE) and the harness session ids, as in
// a Claude project thread without the plugin.
func clearHookEnv(t *testing.T) {
	t.Helper()
	for _, kv := range os.Environ() {
		name, _, _ := strings.Cut(kv, "=")
		if strings.HasPrefix(name, "WIPNOTE_") || strings.HasPrefix(name, "CLAUDE_") || strings.HasPrefix(name, "CODEX_") {
			t.Setenv(name, "")
			os.Unsetenv(name)
		}
	}
}

// slice 23: in a project thread no hook runs. delegate run still records
// the run in the ledger, writes the result and exit files, prints the
// report, and delegate view reads it back from the run folder.
func TestRun_NoHooksStillReports(t *testing.T) {
	fixtures, _ := filepath.Abs(filepath.Join("..", "..", "internal", "delegate", "oneshot", "testdata"))
	dir := delegateRunProject(t)
	clearHookEnv(t)
	t.Setenv("STUB_STDOUT", filepath.Join(fixtures, "claude_print.jsonl"))
	if _, err := os.Stat(filepath.Join(dir, ".wipnote", ".active-session")); err == nil {
		t.Fatal("the temp project has an active session: a hook ran")
	}
	out, code := runDelegate(t, "--", "claude", "-p", "what does tsconfig extend?")
	if code != 0 || !strings.HasPrefix(out, "claude exited 0 after ") || !strings.Contains(out, "\nrun page: ") {
		t.Fatalf("exit %d\n%s", code, out)
	}
	r := onlyRow(t, dir)
	if r.CallerSession != "" || r.Exit == nil || *r.Exit != 0 || r.EndReason != delegationledger.EndExit || r.Ended.IsZero() {
		t.Fatalf("ledger row %+v", r)
	}
	store, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	rf, err := store.Run(r.ID)
	if err != nil {
		t.Fatal(err)
	}
	runDir := rf.Dir()
	for _, f := range []string{runstore.FileExit, runstore.FileLastMD} {
		if _, err := os.Stat(filepath.Join(runDir, f)); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
	}
	var view bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&view)
	cmd.SetErr(&view)
	cmd.SetArgs([]string{"view", r.ID})
	if err := cmd.Execute(); err != nil || !strings.Contains(view.String(), "tsconfig.json") {
		t.Fatalf("view: err %v\n%s", err, view.String())
	}
}
