package oneshot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
)

const testHost = "devbox-1"

// argvOf runs o with the stub recording its argv and returns that argv.
func argvOf(t *testing.T, p *testProject, o Options) ([]string, Result) {
	t.Helper()
	file := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_ARGV", file)
	res := p.run(o)
	data, _ := os.ReadFile(file)
	return strings.Split(strings.TrimRight(string(data), "\n"), "\n"), res
}

func sandboxOf(argv []string) string {
	if i := slices.Index(argv, "--sandbox"); i >= 0 && i+1 < len(argv) {
		return argv[i+1]
	}
	return ""
}

func probed(status codex.ProbeStatus, out string) func(context.Context) codex.ProbeResult {
	return func(context.Context) codex.ProbeResult { return codex.ProbeResult{Status: status, Output: out} }
}

// delegate run passes --sandbox read-only to codex exec by default and
// workspace-write with --write; claude -p gets no sandbox flag.
func TestRun_CodexSandboxLadder(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	o := p.options("codex", "exec", "x")
	o.Sandbox = SandboxOptions{Host: testHost, Preflight: probed(codex.ProbePass, "")}
	if argv, _ := argvOf(t, p, o); sandboxOf(argv) != "read-only" || argv[0] != "exec" {
		t.Fatalf("default argv %q, want exec ... --sandbox read-only", argv)
	}
	o.Sandbox.Write = true
	if argv, _ := argvOf(t, p, o); sandboxOf(argv) != "workspace-write" {
		t.Fatalf("--write argv %q", argv)
	}
	t.Setenv("STUB_STDOUT", fixture(t, "claude_print.jsonl"))
	c := p.options("claude", "-p", "x")
	if argv, _ := argvOf(t, p, c); slices.Contains(argv, "--sandbox") {
		t.Fatalf("claude argv %q carries a sandbox", argv)
	}
	c.Sandbox.Write = true
	if _, err := Run(context.Background(), c); err == nil || !strings.Contains(err.Error(), "--write") {
		t.Fatalf("--write on claude -p: err %v", err)
	}
}

// A failed bwrap probe refuses the run with the probe output before any
// ledger row or run folder is written; an opted-in host runs with
// danger-full-access and a warning instead.
func TestRun_FailedProbeRefusesUnlessOptedIn(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	const msg = "bwrap: No permissions to create new namespace"
	o := p.options("codex", "exec", "x")
	o.Sandbox = SandboxOptions{Host: testHost, Preflight: probed(codex.ProbeFail, msg)}
	_, err := Run(context.Background(), o)
	if !errors.Is(err, codex.ErrRefused) || !strings.Contains(err.Error(), msg) {
		t.Fatalf("err %v, want a refusal with the probe output", err)
	}
	if rows, _ := p.ledger.ReadAll(); len(rows) != 0 {
		t.Fatalf("a refused run wrote ledger rows: %+v", rows)
	}
	if _, err := os.Stat(filepath.Join(p.store.Root(), "runs")); !os.IsNotExist(err) {
		t.Fatalf("a refused run made a run folder: %v", err)
	}
	o.Sandbox.OptIn = []string{testHost}
	argv, res := argvOf(t, p, o)
	if sandboxOf(argv) != "danger-full-access" || res.Exit != 0 {
		t.Fatalf("opted host argv %q exit %d", argv, res.Exit)
	}
	if !strings.Contains(p.stderr.String(), "danger-full-access") || !strings.Contains(p.stderr.String(), msg) {
		t.Fatalf("no warning on stderr:\n%s", p.stderr.String())
	}
}

// A codex exec --json run records Codex's thread id as the worker session
// and the four usage fields, headless and in a pane; a log with no
// thread.started records no session.
func TestRun_LedgerGetsThreadIDAndUsage(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec_thread.jsonl"))
	headless := p.options("codex", "exec", "x")
	pane := inHerdr(p.options("codex", "exec", "x"), newFake(t))
	pane.Settings.ClosePane = "never"
	for name, o := range map[string]Options{"headless": headless, "pane": pane} {
		r := p.row(p.run(o).RunID)
		if !slices.Equal(r.WorkerSessions, []string{"0199a213-81c0-7800-8aa1-bbab2a035a53"}) ||
			r.UsageInput != 24763 || r.UsageCached != 24448 || r.UsageOutput != 122 || r.UsageReasoning != 64 {
			t.Fatalf("%s row %+v", name, r)
		}
	}
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	if r := p.row(p.run(p.options("codex", "exec", "x")).RunID); len(r.WorkerSessions) != 0 || r.UsageReasoning != 19 {
		t.Fatalf("no-thread row %+v", r)
	}
}
