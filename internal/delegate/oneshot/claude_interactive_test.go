package oneshot

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// Slice 22: inside herdr a claude -p run is an interactive worker (spawn,
// the brief as its first prompt, wait for the result file, stop); outside
// herdr the claude -p path stays. The fake herdr runs a stub claude.

const fixedSID = "0f6c2a1e-1d2b-4c3d-8e9f-0123456789ab"

type workerRun struct {
	p      *testProject
	f      *workerHerdr
	o      Options
	config string // the claude global config the run pre-seeds
	cdir   string // the claude data dir transcripts live under
}

func newWorkerRun(t *testing.T, stub stubClaude, argv ...string) *workerRun {
	t.Helper()
	p := newProject(t)
	f := newWorkerHerdr(t, p.store, stub)
	home := t.TempDir()
	w := &workerRun{p: p, f: f, config: filepath.Join(home, ".claude.json"), cdir: filepath.Join(home, ".claude")}
	o := inHerdr(p.options(argv...), f)
	o.WorkItem, o.WorkTitle, o.CallerSession, o.Root = "feat-0a1b2c3d", "the item", "sess-caller", p.dir
	o.Worker = &WorkerOptions{
		Deps:         session.Deps{ProcStart: f.procStart},
		BootLine:     func(run string) string { return session.BootLine("/bin/wipnote", p.dir, run, "sess-caller") },
		StatusLine:   func(run string) string { return "/bin/wipnote delegate status-line " + run },
		ClaudeConfig: w.config, ClaudeDir: w.cdir,
		StartTimeout: 400 * time.Millisecond, ExitWait: 300 * time.Millisecond,
		NewSessionID: func() string { return fixedSID }, Signal: f.signal,
	}
	w.o = o
	return w
}

func (w *workerRun) run(t *testing.T) Result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	res, err := Run(ctx, w.o)
	if err != nil {
		t.Fatalf("Run: %v\nstdout: %s\nstderr: %s", err, w.p.stdout.String(), w.p.stderr.String())
	}
	return res
}

var reportHead = regexp.MustCompile(`^claude exited (\d+) after \d+s(, [^;]+)?; raw log \S+/raw\.log, answer file \S+/last\.md\n`)

func TestClaudeRun_InteractiveInHerdr(t *testing.T) {
	w := newWorkerRun(t, stubClaude{cost: 0.05}, "claude", "-p", "say the answer")
	res := w.run(t)
	out := w.p.stdout.String()
	if res.Exit != 0 || !reportHead.MatchString(out) || !strings.Contains(out, "\nthe answer\n") || !strings.Contains(out, "run page: ") {
		t.Fatalf("exit %d report:\n%s\nstderr: %s", res.Exit, out, w.p.stderr.String())
	}
	r := w.p.row(res.RunID)
	if r.Mode != delegationledger.ModeInteractive || r.IsOpen() || r.EndReason != delegationledger.EndExit || r.Exit == nil || *r.Exit != 0 {
		t.Fatalf("row %+v", r)
	}
	calls := w.f.Calls()
	if !slices.ContainsFunc(calls, func(c string) bool { return strings.HasSuffix(c, " /exit") }) || !slices.ContainsFunc(calls, func(c string) bool { return strings.HasPrefix(c, "close ") }) {
		t.Fatalf("the worker is ended with /exit and its pane closed: %v", calls)
	}
	if len(w.f.signalled()) != 0 {
		t.Fatalf("a worker that exits on /exit is never signalled: %v", w.f.signalled())
	}
	if got := w.p.file(res.RunID, runstore.FileLastMD); got != "the answer\n" {
		t.Fatalf("last.md %q", got)
	}
}

func TestClaudeRun_HeadlessOutsideHerdr(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	w.o.Herdr, w.o.HerdrEnv = nil, herdr.Env{} // outside herdr
	res := w.run(t)
	r := w.p.row(res.RunID)
	if r.Mode != delegationledger.ModeOneshot || r.Harness != string(KindClaude) || len(w.f.Calls()) != 0 {
		t.Fatalf("outside herdr the claude -p path runs: row %+v calls %v", r, w.f.Calls())
	}
	if !reportHead.MatchString(w.p.stdout.String()) {
		t.Fatalf("report %s", w.p.stdout.String())
	}
	// --no-pane inside herdr is headless too.
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	w2.o.NoPane = true
	if res := w2.run(t); w2.p.row(res.RunID).Mode != delegationledger.ModeOneshot {
		t.Fatal("--no-pane must stay headless")
	}
}

func TestClaudeRun_PreseedsTrust(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	must(t, os.WriteFile(w.config, []byte(`{"userID":"keep","projects":{"/other":{"allowedTools":["x"]}}}`), 0o600))
	w.run(t)
	var cfg struct {
		UserID     string `json:"userID"`
		Onboarding bool   `json:"hasCompletedOnboarding"`
		Projects   map[string]map[string]any
	}
	data, err := os.ReadFile(w.config)
	must(t, err)
	must(t, json.Unmarshal(data, &cfg))
	if !cfg.Onboarding || cfg.UserID != "keep" || cfg.Projects[w.p.dir]["hasTrustDialogAccepted"] != true || cfg.Projects["/other"]["allowedTools"] == nil {
		t.Fatalf("config %s", data)
	}
	// Outside herdr nothing is pre-seeded.
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	w2.o.Herdr, w2.o.HerdrEnv = nil, herdr.Env{}
	w2.run(t)
	if _, err := os.Stat(w2.config); !os.IsNotExist(err) {
		t.Fatalf("headless run touched the claude config: %v", err)
	}
}

func TestClaudeRun_SessionIDKnown(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	res := w.run(t)
	argv := w.f.launchArgv()
	if i := slices.Index(argv, "--session-id"); i < 0 || argv[i+1] != fixedSID {
		t.Fatalf("argv %q", argv)
	}
	rf, err := w.p.store.Run(res.RunID)
	must(t, err)
	info, err := session.ReadInfo(rf)
	must(t, err)
	want := TranscriptPath(w.cdir, w.p.dir, fixedSID)
	if info.SessionID != fixedSID || info.Transcript != want || !strings.HasSuffix(want, "/"+fixedSID+".jsonl") {
		t.Fatalf("info %+v want transcript %s", info, want)
	}
	// The caller's own --session-id is the known one; none is added.
	own := "11111111-2222-4333-8444-555555555555"
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--session-id", own)
	w2.run(t)
	argv = w2.f.launchArgv()
	if n := strings.Count(strings.Join(argv, " "), "--session-id"); n != 1 || argv[slices.Index(argv, "--session-id")+1] != own {
		t.Fatalf("argv %q", argv)
	}
}

func TestClaudeRun_ResultFromStopHook(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer")
	if res := w.run(t); res.Exit != 0 || !strings.Contains(w.p.stdout.String(), "the answer") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	// A turn that ends with no result file fails, named.
	w2 := newWorkerRun(t, stubClaude{noAnswer: true}, "claude", "-p", "say the answer")
	res := w2.run(t)
	out := w2.p.stdout.String()
	if res.Exit != 1 || !strings.HasPrefix(out, "claude exited 1 ") || !strings.Contains(out, "no_result") {
		t.Fatalf("exit %d report %s", res.Exit, out)
	}
	if r := w2.p.row(res.RunID); r.Exit == nil || *r.Exit != 1 {
		t.Fatalf("row %+v", r)
	}
}

func TestClaudeRun_ErrorFromStopFailure(t *testing.T) {
	w := newWorkerRun(t, stubClaude{noAnswer: true, stopFailure: "rate_limit"}, "claude", "-p", "say the answer")
	res := w.run(t)
	out := w.p.stdout.String()
	if res.Exit != 1 || !strings.Contains(out, "stop_failure") || !strings.Contains(out, "rate_limit") {
		t.Fatalf("exit %d report %s", res.Exit, out)
	}
}

func TestClaudeRun_CostFromStatusLine(t *testing.T) {
	w := newWorkerRun(t, stubClaude{cost: 0.0512}, "claude", "-p", "say the answer")
	res := w.run(t)
	if !strings.Contains(strings.SplitN(w.p.stdout.String(), "\n", 2)[0], ", 120 in / 30 out tokens, $0.0512;") {
		t.Fatalf("status line %s", w.p.stdout.String())
	}
	r := w.p.row(res.RunID)
	if r.UsageInput != 120 || r.UsageOutput != 30 {
		t.Fatalf("row usage %+v", r)
	}
	settings := w.p.file(res.RunID, runstore.FileClaudeSettings)
	if !strings.Contains(settings, `"statusLine"`) || !strings.Contains(settings, "status-line "+res.RunID) {
		t.Fatalf("settings %s", settings)
	}
	// No status line feed: no usage, no cost.
	w2 := newWorkerRun(t, stubClaude{noStatus: true}, "claude", "-p", "say the answer")
	w2.run(t)
	if head := strings.SplitN(w2.p.stdout.String(), "\n", 2)[0]; strings.Contains(head, "tokens") {
		t.Fatalf("status line %s", head)
	}
}

func TestClaudeRun_TurnCapEnforced(t *testing.T) {
	w := newWorkerRun(t, stubClaude{turns: 5, noAnswer: true, noStop: true}, "claude", "-p", "say the answer", "--max-turns", "2")
	res := w.run(t)
	if res.Exit != 1 || !strings.Contains(w.p.stdout.String(), "error_max_turns") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	if slices.Contains(w.f.launchArgv(), "--max-turns") || !slices.ContainsFunc(w.f.Calls(), func(c string) bool { return strings.HasSuffix(c, " escape") }) {
		t.Fatalf("argv %q calls %v", w.f.launchArgv(), w.f.Calls())
	}
	w2 := newWorkerRun(t, stubClaude{turns: 2}, "claude", "-p", "say the answer", "--max-turns", "10")
	if res := w2.run(t); res.Exit != 0 {
		t.Fatalf("under the cap: exit %d %s", res.Exit, w2.p.stdout.String())
	}
}

func TestClaudeRun_BudgetCapEnforced(t *testing.T) {
	w := newWorkerRun(t, stubClaude{cost: 0.05, noAnswer: true, noStop: true}, "claude", "-p", "say the answer", "--max-budget-usd", "0.01")
	res := w.run(t)
	if res.Exit != 1 || !strings.Contains(w.p.stdout.String(), "error_max_budget_usd") || slices.Contains(w.f.launchArgv(), "--max-budget-usd") {
		t.Fatalf("exit %d %s argv %q", res.Exit, w.p.stdout.String(), w.f.launchArgv())
	}
	w2 := newWorkerRun(t, stubClaude{cost: 0.05}, "claude", "-p", "say the answer", "--max-budget-usd=1")
	if res := w2.run(t); res.Exit != 0 {
		t.Fatalf("under budget: exit %d %s", res.Exit, w2.p.stdout.String())
	}
}

func TestClaudeRun_SessionStartTimeout(t *testing.T) {
	w := newWorkerRun(t, stubClaude{noBind: true, noStatus: true}, "claude", "-p", "say the answer")
	res := w.run(t)
	if res.Exit != 1 || !strings.Contains(w.p.stdout.String(), "session_start_timeout") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	if len(w.f.signalled()) != 1 || slices.ContainsFunc(w.f.Calls(), func(c string) bool { return strings.HasPrefix(c, "send-text") }) {
		t.Fatalf("an unbound worker is signalled, never typed to: signals %v calls %v", w.f.signalled(), w.f.Calls())
	}
	if r := w.p.row(res.RunID); r.IsOpen() {
		t.Fatalf("row %+v", r)
	}
}

func TestClaudeRun_SigtermWhenExitIgnored(t *testing.T) {
	w := newWorkerRun(t, stubClaude{ignoreExit: true}, "claude", "-p", "say the answer")
	res := w.run(t)
	calls := w.f.Calls()
	if res.Exit != 0 || len(w.f.signalled()) != 1 || !slices.ContainsFunc(calls, func(c string) bool { return strings.HasSuffix(c, " /exit") }) {
		t.Fatalf("exit %d signals %v calls %v", res.Exit, w.f.signalled(), calls)
	}
	if r := w.p.row(res.RunID); r.IsOpen() || r.EndReason != delegationledger.EndExit {
		t.Fatalf("row %+v", r)
	}
}

func TestClaudeRun_PluginNotLoadedFails(t *testing.T) {
	// The status line ran (claude is up) but the plugin's SessionStart never did.
	w := newWorkerRun(t, stubClaude{noBind: true}, "claude", "-p", "say the answer")
	res := w.run(t)
	if res.Exit != 1 || !strings.Contains(w.p.stdout.String(), "plugin_not_loaded") {
		t.Fatalf("exit %d %s", res.Exit, w.p.stdout.String())
	}
	// A --plugin-dir that does not exist is refused before anything starts.
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--plugin-dir", filepath.Join(t.TempDir(), "missing"))
	_, err := Run(context.Background(), w2.o)
	if err == nil || !strings.Contains(err.Error(), "plugin_not_loaded") || len(w2.f.Calls()) != 0 {
		t.Fatalf("err %v calls %v", err, w2.f.Calls())
	}
	// An existing one passes through.
	dir := t.TempDir()
	w3 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--plugin-dir", dir)
	if res := w3.run(t); res.Exit != 0 || !slices.Contains(w3.f.launchArgv(), dir) {
		t.Fatalf("exit %d argv %q", res.Exit, w3.f.launchArgv())
	}
}

func TestClaudeRun_PromptBeforeVariadicFlags(t *testing.T) {
	for _, argv := range [][]string{
		{"claude", "-p", "--mcp-config", "m.json", "reply ok now"},
		{"claude", "-p", "reply ok now", "--mcp-config", "m.json"},
		{"claude", "-p", "--mcp-config", "m.json", "--", "reply ok now"},
	} {
		w := newWorkerRun(t, stubClaude{}, argv...)
		w.run(t)
		got := w.f.launchArgv()
		dash, mcp := slices.Index(got, "--"), slices.Index(got, "--mcp-config")
		prompt := slices.IndexFunc(got, func(s string) bool { return strings.HasSuffix(strings.TrimSpace(s), "reply ok now") })
		if dash < 0 || prompt != dash+1 || mcp < prompt || got[mcp+1] != filepath.Join(w.o.Cwd, "m.json") || slices.Contains(got, "-p") {
			t.Fatalf("%q -> %q", argv, got)
		}
	}
}

func TestClaudeRun_PlanModeModelWarning(t *testing.T) {
	w := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--permission-mode", "plan", "--model", "haiku")
	w.run(t)
	if !strings.Contains(w.p.stderr.String(), "plan mode") {
		t.Fatalf("stderr %s", w.p.stderr.String())
	}
	w2 := newWorkerRun(t, stubClaude{}, "claude", "-p", "say the answer", "--model", "haiku")
	w2.run(t)
	if strings.Contains(w2.p.stderr.String(), "plan mode") {
		t.Fatalf("no plan mode, no warning: %s", w2.p.stderr.String())
	}
}
