package oneshot

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// logCommits records both processes' ledger writes in one file.
func logCommits(t *testing.T) string {
	t.Helper()
	log := filepath.Join(t.TempDir(), "commits.log")
	t.Setenv(commitLogEnv, log)
	prev := delegationledger.OnCommit
	delegationledger.OnCommit = commitLogger(log, "caller")
	t.Cleanup(func() { delegationledger.OnCommit = prev })
	return log
}

func commitLines(t *testing.T, log, action string) []string {
	t.Helper()
	data, _ := os.ReadFile(log)
	var out []string
	for _, l := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if strings.HasSuffix(l, " "+action) {
			out = append(out, l)
		}
	}
	return out
}

// onlyRun is the single ledger row a test's run opened.
func (p *testProject) onlyRun() delegationledger.Record {
	p.t.Helper()
	recs, err := p.ledger.ReadAll()
	if err != nil || len(recs) != 1 {
		p.t.Fatalf("ledger rows %d: %v", len(recs), err)
	}
	return recs[0]
}

// Caller interrupted: delegate run returns while the tool runs on in its
// pane, so it never closes the row. The pane worker closes it with the
// tool's exit and usage when the tool ends.
func TestRunPane_ClosesRowWhenCallerGone(t *testing.T) {
	log := logCommits(t)
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	t.Setenv("STUB_SLEEP", "1")
	t.Setenv("STUB_EXIT", "3")
	o := inHerdr(p.options("codex", "exec", "x"), newFake(t))
	o.Settings.ClosePane = "never"
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, cancel)
	if _, err := Run(ctx, o); err == nil || !strings.Contains(err.Error(), "interrupted") {
		t.Fatalf("Run: %v, want interrupted", err)
	}
	if r := p.onlyRun(); !r.IsOpen() {
		t.Fatalf("the caller closed the row on interrupt: %+v", r)
	}
	waitFor(t, "the pane worker to close and enrich the row", func() bool {
		r := p.onlyRun()
		return !r.IsOpen() && r.UsageInput != 0
	})
	r := p.onlyRun()
	if r.Exit == nil || *r.Exit != 3 || r.EndReason != delegationledger.EndExit || r.UsageInput != 13866 {
		t.Fatalf("row %+v, want exit 3, reason exit, usage", r)
	}
	if got := commitLines(t, log, "delegation close"); len(got) != 1 || got[0] != "worker delegation close" {
		t.Fatalf("closes %q, want one by the worker", got)
	}
}

// Caller present: the worker and the caller both try to close; the row is
// closed once, and the second close and enrich write nothing.
func TestRunPane_CallerPresentClosesOnce(t *testing.T) {
	log := logCommits(t)
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	o := inHerdr(p.options("codex", "exec", "x"), newFake(t))
	o.Settings.ClosePane = "never"
	res := p.run(o)
	waitFor(t, "the pane worker to end", func() bool { return len(commitLines(t, log, "delegation close")) > 0 })
	time.Sleep(300 * time.Millisecond) // a late second close would land here
	if got := commitLines(t, log, "delegation close"); len(got) != 1 {
		t.Fatalf("closes %q, want exactly one", got)
	}
	if got := commitLines(t, log, "delegation enrich"); len(got) != 1 {
		t.Fatalf("enriches %q, want exactly one", got)
	}
	if r := p.row(res.RunID); r.Exit == nil || *r.Exit != 0 || r.EndReason != delegationledger.EndExit || r.UsageInput != 13866 {
		t.Fatalf("row %+v", r)
	}
}

// A row closed while the tool runs (a stop) stays as it was: the pane
// worker never reopens or changes a closed row (rule F-1).
func TestRunPane_LeavesClosedRowAlone(t *testing.T) {
	log := logCommits(t)
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	t.Setenv("STUB_SLEEP", "1")
	t.Setenv("STUB_EXIT", "3")
	o := inHerdr(p.options("codex", "exec", "x"), newFake(t))
	o.Settings.ClosePane = "never"
	ctx, cancel := context.WithCancel(context.Background())
	time.AfterFunc(300*time.Millisecond, func() {
		_ = p.ledger.Close(p.onlyRun().ID, delegationledger.CloseInfo{Reason: delegationledger.EndStop})
		cancel()
	})
	_, _ = Run(ctx, o)
	waitFor(t, "the tool to exit", func() bool {
		info, err := p.store.Run(p.onlyRun().ID)
		if err != nil {
			return false
		}
		ri, _ := info.Read()
		return ri.Finished
	})
	time.Sleep(500 * time.Millisecond) // the worker's close would land here
	r := p.onlyRun()
	if r.EndReason != delegationledger.EndStop || r.Exit != nil || r.UsageInput != 0 {
		t.Fatalf("a stopped row changed: %+v", r)
	}
	if got := commitLines(t, log, "delegation close"); len(got) != 1 || got[0] != "caller delegation close" {
		t.Fatalf("closes %q, want only the stop", got)
	}
	data, _ := os.ReadFile(log)
	if strings.Contains(string(data), "worker ") {
		t.Fatalf("the pane worker wrote a closed row:\n%s", data)
	}
}
