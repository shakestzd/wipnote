package oneshot

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// fixes.test.ts "the plugin root reaches the pane script shell-quoted": the
// line herdr types into the pane holds the wipnote binary and the project
// folder, so each is shell-quoted and $, backticks and quotes stay literal.
func TestPaneLine_QuotesEveryWord(t *testing.T) {
	odd := filepath.Join(t.TempDir(), "my $HOME `id` it's")
	must(t, os.MkdirAll(odd, 0o755))
	exe := filepath.Join(odd, "wipnote")
	must(t, os.WriteFile(exe, []byte("#!/usr/bin/env bash\nprintf '[%s]' \"$@\"\n"), 0o755))
	line := PaneLine(exe, []string{"delegate", "run-pane"}, odd, "dlg-0123456789ab")
	if !strings.Contains(line, `'`+strings.ReplaceAll(exe, `'`, `'\''`)+`'`) || strings.Contains(line, `"`+odd) {
		t.Fatalf("not shell-quoted: %s", line)
	}
	out, err := exec.Command("bash", "-c", line).Output()
	must(t, err)
	want := "[delegate][run-pane][--project-dir][" + odd + "][dlg-0123456789ab]"
	if string(out) != want {
		t.Fatalf("the shell read %q, want %q", out, want)
	}
}

// fixes.test.ts "the pane sets its own title and clears the typed command
// before the log": the pane's first output is the title escape and a clear.
func TestRunPane_SetsTitleAndClears(t *testing.T) {
	p := newProject(t)
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "two words"), f)
	o.Settings.ClosePane = "never"
	p.run(o)
	waitFor(t, "the pane worker to end", func() bool { return strings.Contains(f.paneOutput("w1:p1"), "answer") })
	if out := f.paneOutput("w1:p1"); !strings.HasPrefix(out, "\x1b]0;cdx: two words\x07\x1b[H\x1b[2J") {
		t.Fatalf("pane output starts %q", out[:min(60, len(out))])
	}
}

// closePane always closes the pane after the run (the pane worker asks herdr
// itself, after closeDelay); never leaves it.
func TestRunPane_ClosePolicy(t *testing.T) {
	log := installFakeHerdr(t)
	p := newProject(t)
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "x"), f)
	o.Settings.ClosePane = "always"
	p.run(o)
	waitFor(t, "pane close", func() bool {
		calls, _ := herdr.ReadFakeCalls(log)
		return len(calls) == 1 && strings.Join(calls[0].Argv, " ") == "pane close w1:p1"
	})
	o.Settings.ClosePane = "success"
	t.Setenv("STUB_EXIT", "1")
	p.run(o)
	o.Settings.ClosePane = "never"
	t.Setenv("STUB_EXIT", "0")
	p.run(o)
	waitFor(t, "both pane workers to end", func() bool {
		return strings.Contains(f.paneOutput("w1:p2"), "exit 1") && strings.Contains(f.paneOutput("w1:p3"), "finished")
	})
	if calls, _ := herdr.ReadFakeCalls(log); len(calls) != 1 {
		t.Fatalf("success on a failure, or never, closed a pane: %+v", calls)
	}
}

// Tile layout: each run's pane is split from the caller's live one-shot
// panes as the mod's grid does; at maxPanes the run goes headless and says so.
func TestRun_TileLayoutAndCap(t *testing.T) {
	p := newProject(t)
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "x"), f)
	o.Settings.ClosePane, o.Settings.MaxPanes = "never", 2
	p.run(o)
	p.run(o)
	if got := strings.Join(f.Calls(), "|"); !strings.HasPrefix(got, "split  right|") || !strings.Contains(got, "|split w1:p1 down|") {
		t.Fatalf("tile splits %q", got)
	}
	p.stdout.Reset()
	res := p.run(o)
	if !strings.HasPrefix(p.stdout.String(), "herdr pane limit reached (2 delegation panes open, layout limit 2): ran without a pane\ncodex exited 0") {
		t.Fatalf("cap note:\n%s", p.stdout.String())
	}
	if h := strings.TrimSpace(p.file(res.RunID, runstore.FileHost)); h != runstore.HostNone {
		t.Fatalf("capped run host %q", h)
	}
	// a pane herdr says is gone frees its place
	_ = f.Close(t.Context(), "w1:p1")
	p.run(o)
	if calls := f.Calls(); calls[len(calls)-3] != "split w1:p2 down" {
		t.Fatalf("after a close: %q", calls)
	}
}

// layout down (or auto with the legacy direction down) always splits the
// caller's pane down and keeps no tile registry.
func TestRun_FixedLayout(t *testing.T) {
	p := newProject(t)
	f := newFake(t)
	o := inHerdr(p.options("codex", "exec", "x"), f)
	o.Settings.ClosePane, o.Settings.Layout, o.Settings.Direction = "never", "auto", "down"
	p.run(o)
	p.run(o)
	if got := strings.Join(f.Calls(), "|"); strings.Count(got, "split  down") != 2 {
		t.Fatalf("fixed splits %q", got)
	}
	if _, err := os.Stat(filepath.Join(p.store.Root(), paneRegistryDir)); !os.IsNotExist(err) {
		t.Fatalf("fixed layout wrote a tile registry: %v", err)
	}
}

// A split herdr refuses runs the call headless with the same report.
func TestRun_SplitFailsRunsHeadless(t *testing.T) {
	p := newProject(t)
	f := newFake(t)
	f.splitFails = true
	res := p.run(inHerdr(p.options("codex", "exec", "x"), f))
	if res.Exit != 0 || !strings.HasPrefix(p.stdout.String(), "codex exited 0") {
		t.Fatalf("exit %d\n%s", res.Exit, p.stdout.String())
	}
}
