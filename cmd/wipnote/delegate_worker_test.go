package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// The status line command records cost and tokens into the run folder and
// prints the usage line (slice 22).
func TestDelegateStatusLine_RecordsCost(t *testing.T) {
	dir := delegateRunProject(t)
	store, err := runstore.New(dir)
	if err != nil {
		t.Fatal(err)
	}
	run := "dlg-0123456789ab"
	if _, err := store.CreateRun(run); err != nil {
		t.Fatal(err)
	}
	out := statusLine(t, run, `{"cost":{"total_cost_usd":0.25},"context_window":{"total_input_tokens":1000,"total_output_tokens":20}}`)
	if out != "delegate 1000 in / 20 out tokens, $0.25\n" {
		t.Fatalf("out %q", out)
	}
	usage, err := os.ReadFile(filepath.Join(store.Root(), "runs", run, runstore.FileUsage))
	if err != nil || strings.TrimSpace(string(usage)) != "1000 in / 20 out tokens, $0.25" {
		t.Fatalf("usage %q err %v", usage, err)
	}
	// Bad input or an unknown run prints nothing and never fails.
	if out := statusLine(t, run, `not json`); out != "" {
		t.Fatalf("out %q", out)
	}
	if out := statusLine(t, "dlg-ffffffffffff", `{"cost":{"total_cost_usd":1}}`); out != "" {
		t.Fatalf("out %q", out)
	}
}

func statusLine(t *testing.T, run, stdin string) string {
	t.Helper()
	var out bytes.Buffer
	cmd := delegateCmd()
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetIn(strings.NewReader(stdin))
	cmd.SetArgs([]string{"status-line", run})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("status-line: %v", err)
	}
	return out.String()
}

func TestDelegateStatusLineCommand_Quoted(t *testing.T) {
	got := delegateStatusLineCommand("/opt/my bin/wipnote", "/p/x", "dlg-0123456789ab")
	if got != `'/opt/my bin/wipnote' delegate status-line --project-dir '/p/x' 'dlg-0123456789ab'` {
		t.Fatalf("%s", got)
	}
}

// A delegate worker's launcher runs claude in the run's own dir
// (--work-dir), not the project's main root (slice 22 review).
func TestClaudeLaunchRoot_WorkDir(t *testing.T) {
	dir := delegateRunProject(t)
	wt := t.TempDir()
	if got, err := claudeLaunchRoot(wt); err != nil || got != wt {
		t.Fatalf("got %q err %v, want %s", got, err, wt)
	}
	if got, err := claudeLaunchRoot(""); err != nil || got != dir {
		t.Fatalf("without --work-dir the project root: got %q err %v want %s", got, err, dir)
	}
	if _, err := claudeLaunchRoot(filepath.Join(wt, "missing")); err == nil {
		t.Fatal("a missing work dir is refused")
	}
	if f := claudeCmd().Flags().Lookup("work-dir"); f == nil || !f.Hidden {
		t.Fatal("claude has a hidden --work-dir flag")
	}
}
