package oneshot

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// gitWorktree makes dir a git repo with one commit and adds a linked
// worktree beside it; it returns the worktree's root.
func gitWorktree(t *testing.T, dir string) string {
	t.Helper()
	wt := filepath.Join(t.TempDir(), "wt")
	for _, args := range [][]string{
		{"init", "-q", "-b", "main"}, {"-c", "user.email=t@t", "-c", "user.name=t", "commit", "-q", "--allow-empty", "-m", "x"},
		{"worktree", "add", "-q", wt},
	} {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	real, err := filepath.EvalSymlinks(wt)
	must(t, err)
	return real
}

// A caller in a linked worktree gets a worker that runs there: its launch
// dir, transcript and trust key are the worktree's, relative path args are
// made absolute against the caller's cwd, and --max-turns reads that
// transcript.
func TestClaudeRun_WorktreeRunsWhereCallerRan(t *testing.T) {
	w := newWorkerRun(t, stubClaude{turns: 5, noAnswer: true, noStop: true}, "claude", "-p", "say the answer",
		"--max-turns", "2", "--mcp-config", "m.json", "--add-dir", "extra")
	wt := gitWorktree(t, w.p.dir)
	w.o.Cwd = wt
	res := w.run(t)
	if res.Exit != 1 || !strings.Contains(w.p.stdout.String(), "error_max_turns") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	rf, err := w.p.store.Run(res.RunID)
	must(t, err)
	info, err := session.ReadInfo(rf)
	must(t, err)
	if info.WorkDir != wt || info.Transcript != TranscriptPath(w.cdir, wt, fixedSID) {
		t.Fatalf("info %+v, want work dir %s", info, wt)
	}
	argv := w.f.launchArgv()
	if i := slices.Index(argv, "--work-dir"); i < 0 || argv[i+1] != wt {
		t.Fatalf("the launcher is told where to run: %q", argv)
	}
	if !slices.Contains(argv, filepath.Join(wt, "m.json")) || !slices.Contains(argv, filepath.Join(wt, "extra")) || slices.Contains(argv, "m.json") {
		t.Fatalf("relative paths are made absolute: %q", argv)
	}
	var cfg struct{ Projects map[string]map[string]any }
	data, err := os.ReadFile(w.config)
	must(t, err)
	must(t, json.Unmarshal(data, &cfg))
	if cfg.Projects[wt]["hasTrustDialogAccepted"] != true || cfg.Projects[w.p.dir] != nil {
		t.Fatalf("trust is seeded for the worktree only: %s", data)
	}
}

// An inline JSON --mcp-config is not a path.
func TestClaudeRun_InlineJSONArgKept(t *testing.T) {
	inline := `{"mcpServers":{}}`
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--mcp-config", inline)
	w.run(t)
	if !slices.Contains(w.f.launchArgv(), inline) {
		t.Fatalf("argv %q", w.f.launchArgv())
	}
}

// A worker that never started is never typed to, even when SIGTERM leaves
// it running: Enter on a trust dialog would accept it.
func TestClaudeRun_UnboundNeverTyped(t *testing.T) {
	w := newWorkerRun(t, stubClaude{noBind: true, noStatus: true, ignoreTerm: true}, "claude", "-p", "say the answer")
	res := w.run(t)
	for _, c := range w.f.Calls() {
		if strings.HasPrefix(c, "send-text") || strings.HasPrefix(c, "send-keys") {
			t.Fatalf("typed into an unbound worker: %v", w.f.Calls())
		}
	}
	if r := w.p.row(res.RunID); res.Exit != 1 || r.IsOpen() || r.Exit == nil || *r.Exit != 1 {
		t.Fatalf("exit %d row %+v", res.Exit, r)
	}
	// A bound worker is typed to (the opposite case).
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	w2.run(t)
	if !slices.ContainsFunc(w2.f.Calls(), func(c string) bool { return strings.HasPrefix(c, "send-text") }) {
		t.Fatalf("calls %v", w2.f.Calls())
	}
}

// Only the worker's own session (and its /clear successor) counts: a
// nested claude's failing events in the run folder change nothing.
func TestClaudeRun_OnlyOwnSessionCounts(t *testing.T) {
	w := newWorkerRun(t, stubClaude{nested: true}, "claude", "-p", "say the answer")
	if res := w.run(t); res.Exit != 0 || strings.Contains(w.p.stdout.String(), "stop_failure") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	// After /clear the successor session is the worker's: its turn ending
	// with no result file fails the run.
	w2 := newWorkerRun(t, stubClaude{clearNoAnswer: true}, "claude", "-p", "say the answer")
	if res := w2.run(t); res.Exit != 1 || !strings.Contains(w2.p.stdout.String(), "no_result") {
		t.Fatalf("exit %d %s", res.Exit, w2.p.stdout.String())
	}
}

func TestViewEvents_OwnSessionAndClearSuccessor(t *testing.T) {
	ev := func(sid, kind, sum string) runstore.Event { return runstore.Event{SID: sid, Kind: kind, Summary: sum} }
	evs := []runstore.Event{
		ev("own", "session_start", ""), ev("own", "prompt", ""),
		ev("nested", "session_start", ""), ev("nested", "prompt", ""), ev("nested", "error", "rate_limit"),
	}
	v := viewEvents(evs, "own")
	if !v.bound || !v.running || v.failure != "" {
		t.Fatalf("nested events counted: %+v", v)
	}
	// A new session with no /clear before it is not the worker's.
	v = viewEvents(append(evs, ev("other", "stop", "")), "own")
	if v.ended {
		t.Fatalf("%+v", v)
	}
	evs = append(evs, ev("own", "session_end", "clear"), ev("next", "session_start", ""), ev("next", "prompt", ""), ev("next", "stop", ""))
	if v := viewEvents(evs, "own"); !v.ended || v.running {
		t.Fatalf("the /clear successor counts: %+v", v)
	}
}

// A failure verdict ends claude even when the input line cannot be read
// (nothing is typed then, but its own processes get SIGTERM).
func TestClaudeRun_FailureSignalsDespiteDraft(t *testing.T) {
	w := newWorkerRun(t, stubClaude{noAnswer: true, styledErr: true}, "claude", "-p", "say the answer")
	res := w.run(t)
	if res.Exit != 1 || len(w.f.signalled()) != 1 || slices.ContainsFunc(w.f.Calls(), func(c string) bool { return strings.HasPrefix(c, "send-text") }) {
		t.Fatalf("exit %d signals %v calls %v", res.Exit, w.f.signalled(), w.f.Calls())
	}
	// Done, with a possible draft: the person's line is left alone.
	w2 := newWorkerRun(t, stubClaude{styledErr: true}, "claude", "-p", "say the answer")
	if res := w2.run(t); res.Exit != 0 || len(w2.f.signalled()) != 0 {
		t.Fatalf("exit %d signals %v", res.Exit, w2.f.signalled())
	}
}

// A turn that ends waiting on the caller (a q-file, as the preamble says)
// or on a background shell is not a missing result: the run waits, says
// so, and ends done when the answer comes. A turn that just ends with no
// result file is still no_result (TestClaudeRun_ResultFromStopHook).
func TestClaudeRun_WaitsOnQuestionAndBackgroundShell(t *testing.T) {
	for _, stub := range []stubClaude{{asks: true}, {bgShell: true}} {
		w := newWorkerRun(t, stub, "claude", "-p", "say the answer")
		w.o.Worker.StartTimeout = 5 * time.Second
		res := w.run(t)
		if res.Exit != 0 || len(w.f.signalled()) != 0 || !strings.Contains(w.p.stdout.String(), "the answer") {
			t.Fatalf("%+v: exit %d signals %v\nstdout %s\nstderr %s", stub, res.Exit, w.f.signalled(), w.p.stdout.String(), w.p.stderr.String())
		}
		if stub.asks && !strings.Contains(w.p.stderr.String(), "answer it in pane") {
			t.Fatalf("the caller is told the worker asks: %s", w.p.stderr.String())
		}
	}
}

// The launcher gets the run store's project as its wipnote root with the
// work dir, so the worker's hooks write into this run folder.
func TestClaudeRun_ProjectDirPassed(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	sub := filepath.Join(w.p.dir, "sub")
	must(t, os.MkdirAll(sub, 0o755))
	w.o.Cwd = sub
	w.run(t)
	argv := w.f.launchArgv()
	i, j := slices.Index(argv, "--work-dir"), slices.Index(argv, "--project-dir")
	if i < 0 || argv[i+1] != sub || j < 0 || argv[j+1] != w.p.dir {
		t.Fatalf("argv %q", argv)
	}
}
