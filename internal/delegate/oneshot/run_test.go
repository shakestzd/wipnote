package oneshot

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

const fiveLines = "l1\nl2\nl3\nl4\nl5"

// codexReport is the mod's codex status line for the run folder dir.
func codexReport(dir, usage string) *regexp.Regexp {
	return regexp.MustCompile("^codex exited 0 after [0-9]+s, " + regexp.QuoteMeta(usage) +
		"; raw log " + regexp.QuoteMeta(dir+"/raw.log") + ", answer file " + regexp.QuoteMeta(dir+"/last.md") + "\n")
}

// Inside herdr a codex run opens a pane, runs there and prints the mod's
// report: status line with usage, the answer capped at maxReturnLines, the
// run page. The ledger row is opened, closed with the exit and enriched.
func TestRun_CodexInPane(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	t.Setenv("STUB_ANSWER", fiveLines)
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "list the files"), f)
	o.Settings.MaxReturnLines, o.Settings.ClosePane = 3, "never"
	res := p.run(o)
	dir := p.runDir(res.RunID)
	out := p.stdout.String()
	if res.Exit != 0 || !codexReport(dir, "input 13,866 (cached 1,234,567), output 5, reasoning 19").MatchString(out) {
		t.Fatalf("exit %d, report:\n%s", res.Exit, out)
	}
	if !strings.HasSuffix(out, "\nl1\nl2\nl3\n... 2 more lines in "+dir+"/last.md\nrun page: "+dir+"/run.html\n") {
		t.Fatalf("answer not capped as the mod does:\n%s", out)
	}
	if got := strings.Join(f.Calls(), "|"); got != "split  right|run w1:p1|rename w1:p1 cdx: list the files" {
		t.Fatalf("herdr calls %q: want the split, then run, then rename with the title as one argument", got)
	}
	if h := strings.TrimSpace(p.file(res.RunID, runstore.FileHost)); h != runstore.HostHerdr {
		t.Fatalf("host %q", h)
	}
	r := p.row(res.RunID)
	if r.Mode != delegationledger.ModeOneshot || r.Harness != "codex" || r.Exit == nil || *r.Exit != 0 ||
		r.EndReason != delegationledger.EndExit || r.UsageInput != 13866 || r.UsageCached != 1234567 || r.UsageOutput != 5 {
		t.Fatalf("ledger row %+v", r)
	}
	if !strings.Contains(p.file(res.RunID, runstore.FileCleanMD), "ls -la") {
		t.Fatal("clean.md was not rendered from the raw log")
	}
}

// Outside herdr the same run is headless (codex exec, then claude -p) with
// the same report; no pane is asked for.
func TestRun_HeadlessOutsideHerdr(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_STDOUT", fixture(t, "codex_exec.jsonl"))
	t.Setenv("STUB_ANSWER", "the answer")
	res := p.run(p.options("codex", "exec", "x"))
	dir := p.runDir(res.RunID)
	out := p.stdout.String()
	if !codexReport(dir, "input 13,866 (cached 1,234,567), output 5, reasoning 19").MatchString(out) ||
		!strings.HasSuffix(out, "\nthe answer\nrun page: "+dir+"/run.html\n") {
		t.Fatalf("headless report:\n%s", out)
	}
	if h := strings.TrimSpace(p.file(res.RunID, runstore.FileHost)); h != runstore.HostNone {
		t.Fatalf("host %q, want none", h)
	}
	// a failure returns the end of clean.log instead, and the tool's exit
	p.stdout.Reset()
	t.Setenv("STUB_EXIT", "3")
	res = p.run(p.options("codex", "exec", "x"))
	if res.Exit != 3 || !strings.Contains(p.stdout.String(), "codex exited 3 after") || !strings.Contains(p.stdout.String(), "a line on stderr") {
		t.Fatalf("failed run: exit %d\n%s", res.Exit, p.stdout.String())
	}
	if r := p.row(res.RunID); r.Exit == nil || *r.Exit != 3 {
		t.Fatalf("row exit %+v", r.Exit)
	}
	// even with --no-pane inside herdr
	p.stdout.Reset()
	t.Setenv("STUB_EXIT", "0")
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "x"), f)
	o.NoPane = true
	p.run(o)
	if len(f.Calls()) != 0 {
		t.Fatalf("--no-pane asked herdr for %q", f.Calls())
	}
}

// A pane closed before the tool finished ends the run with exit 130 and
// end_reason pane_closed.
func TestRun_PaneClosed(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_SLEEP", "30")
	f := newFake(t)
	f.closeAfter = 300e6
	res := p.run(inHerdr(p.options("codex", "exec", "x"), f))
	dir := p.runDir(res.RunID)
	if res.Exit != 130 || res.EndReason != delegationledger.EndPaneClosed {
		t.Fatalf("result %+v", res)
	}
	if want := "the herdr pane w1:p1 was closed before codex finished (log: " + dir + "/raw.log)\n"; !strings.Contains(p.stdout.String(), want) {
		t.Fatalf("stdout %q, want %q", p.stdout.String(), want)
	}
	r := p.row(res.RunID)
	if r.Exit == nil || *r.Exit != 130 || r.EndReason != delegationledger.EndPaneClosed {
		t.Fatalf("row %+v", r)
	}
	if e := strings.TrimSpace(p.file(res.RunID, runstore.FileExit)); e != "130" {
		t.Fatalf("exit file %q", e)
	}
}

// claude -p runs with stream-json added and reports the result's usage and
// text the way the mod does.
func TestRun_ClaudePrintStreamJSON(t *testing.T) {
	p := newProject(t)
	argv := filepath.Join(t.TempDir(), "argv")
	t.Setenv("STUB_ARGV", argv)
	t.Setenv("STUB_STDOUT", fixture(t, "claude_print.jsonl"))
	res := p.run(p.options("claude", "-p", "what does tsconfig extend?"))
	got, _ := os.ReadFile(argv)
	if string(got) != "--output-format\nstream-json\n--verbose\n-p\nwhat does tsconfig extend?\n" {
		t.Fatalf("claude argv %q", got)
	}
	dir := p.runDir(res.RunID)
	want := "claude exited 0 after "
	out := p.stdout.String()
	if !strings.HasPrefix(out, want) || !strings.Contains(out, "s, 4 in / 111 out tokens, $0.1077; raw log "+dir+"/raw.log, answer file "+dir+"/last.md\n"+
		"`tsconfig.json` extends `./.claude-plugin/types/tsconfig.json`.\nrun page: ") {
		t.Fatalf("claude report:\n%s", out)
	}
	if r := p.row(res.RunID); r.Harness != "claude-print" || r.UsageInput != 4 || r.UsageOutput != 111 {
		t.Fatalf("row %+v", r)
	}
}

// Only an explicit --label enters the ledger; a derived label goes to the
// run folder's label file alone (q-label).
func TestRun_DerivedLabelNotInLedger(t *testing.T) {
	p := newProject(t)
	res := p.run(p.options("codex", "exec", "summarise the diff"))
	if r := p.row(res.RunID); r.Label != "" {
		t.Fatalf("derived label reached the ledger: %q", r.Label)
	}
	if l := p.file(res.RunID, runstore.FileLabel); l != "cdx: summarise the diff" {
		t.Fatalf("label file %q", l)
	}
	o := p.options("codex", "exec", "summarise the diff")
	o.Label = "nightly review"
	res = p.run(o)
	if r := p.row(res.RunID); r.Label != "nightly review" {
		t.Fatalf("explicit label not kept: %q", r.Label)
	}
	if l := p.file(res.RunID, runstore.FileLabel); l != "nightly review" {
		t.Fatalf("label file %q", l)
	}
}

// fixes.test.ts "a standalone review sends its final message to the answer
// file": review's stdout is last.md, its progress (stderr) the log.
func TestRun_CodexReviewAnswerFile(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_ANSWER", "LGTM, one nit")
	res := p.run(p.options("codex", "review"))
	if last := p.file(res.RunID, runstore.FileLastMD); last != "LGTM, one nit\n" {
		t.Fatalf("last.md %q", last)
	}
	raw := p.file(res.RunID, runstore.FileRawLog)
	if !strings.Contains(raw, "reviewing the diff") || strings.Contains(raw, "LGTM") {
		t.Fatalf("raw.log %q: want the progress only", raw)
	}
	if !strings.Contains(p.stdout.String(), "\nLGTM, one nit\n") {
		t.Fatalf("report %q", p.stdout.String())
	}
	if r := p.row(res.RunID); r.Harness != "codex-review" {
		t.Fatalf("harness %q", r.Harness)
	}
}

// The caller's own -o file is written as asked and copied to last.md.
func TestRun_CodexOwnAnswerFileCopied(t *testing.T) {
	p := newProject(t)
	t.Setenv("STUB_ANSWER", "mine")
	own := filepath.Join(t.TempDir(), "own.md")
	res := p.run(p.options("codex", "exec", "-o", own, "x"))
	if b, _ := os.ReadFile(own); string(b) != "mine" {
		t.Fatalf("own file %q", b)
	}
	if last := p.file(res.RunID, runstore.FileLastMD); last != "mine" {
		t.Fatalf("last.md %q", last)
	}
}

// A tool that is not on PATH ends as a shell would: exit 127, said in the log.
func TestRun_ToolMissing(t *testing.T) {
	p := newProject(t)
	t.Setenv("PATH", t.TempDir())
	res := p.run(p.options("codex", "exec", "x"))
	if res.Exit != 127 || !strings.Contains(p.stdout.String(), "codex exited 127") || !strings.Contains(p.stdout.String(), "not found") {
		t.Fatalf("exit %d\n%s", res.Exit, p.stdout.String())
	}
}

// Refused argv opens no row and makes no run folder.
func TestRun_RefusedOpensNothing(t *testing.T) {
	p := newProject(t)
	if _, err := Run(t.Context(), p.options("codex", "exec", "x", "|", "sort")); err == nil {
		t.Fatal("a chained command ran")
	}
	if rows, _ := p.ledger.ReadAll(); len(rows) != 0 {
		t.Fatalf("rows %v", rows)
	}
	if _, err := os.Stat(filepath.Join(p.store.Root(), "runs")); !os.IsNotExist(err) {
		t.Fatalf("a run folder was made: %v", err)
	}
}

// fixes.test.ts "old scripts, run directories and ccx claims are swept": a
// run sweeps finished run folders and ccx claims older than the keep age
// (7 days, the mod's default); live runs and fresh claims stay. There are no
// generated scripts to sweep: Go runs the argv itself.
func TestRun_SweepsOldRunsAndClaims(t *testing.T) {
	p := newProject(t)
	old := time.Now().Add(-8 * 24 * time.Hour)
	finished, err := p.store.CreateRun("dlg-000000000001")
	must(t, err)
	must(t, finished.Finish(0, old))
	live, err := p.store.CreateRun("dlg-000000000002")
	must(t, err)
	must(t, live.WriteStart(old))
	for name, at := range map[string]time.Time{"old.jsonl": old, "new.jsonl": time.Now()} {
		d := filepath.Join(claimsDir(), name)
		must(t, os.MkdirAll(d, 0o700))
		must(t, os.Chtimes(d, at, at))
	}
	p.run(p.options("codex", "exec", "x"))
	if _, err := os.Stat(finished.Dir()); !os.IsNotExist(err) {
		t.Error("an old finished run was kept")
	}
	if _, err := os.Stat(live.Dir()); err != nil {
		t.Error("a live run was swept")
	}
	if _, err := os.Stat(filepath.Join(claimsDir(), "old.jsonl")); !os.IsNotExist(err) {
		t.Error("an old ccx claim was kept")
	}
	if _, err := os.Stat(filepath.Join(claimsDir(), "new.jsonl")); err != nil {
		t.Error("a fresh ccx claim was swept")
	}
}
