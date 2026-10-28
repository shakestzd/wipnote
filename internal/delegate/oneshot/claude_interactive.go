package oneshot

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// A Claude one-shot run inside herdr is an interactive worker (plan-28b4a7f6
// slice 22, owner decision after spike spk-87139e0e): spawn (slice 10) with
// the spike's wrapper and the prompt as the brief, wait for the brief's
// result file (decision R), then wipnote decides done or failed and ends the
// session with /exit, then SIGTERM. The report is the claude -p report.
// Outside herdr, slice 8's claude -p path runs instead.

// Defaults. Both are defaults chosen here, not given by the spec.
const (
	// DefaultStartTimeout bounds the wait for the worker's SessionStart: the
	// boot, the launcher and claude's own startup.
	DefaultStartTimeout = 90 * time.Second
	// DefaultWorkerExitWait bounds each end phase (/exit, then SIGTERM).
	DefaultWorkerExitWait = 10 * time.Second
)

// WorkerOptions turn a claude -p run inside herdr into an interactive worker.
type WorkerOptions struct {
	// Deps are the session package's; Store, Ledger and Herdr are the run's
	// own.
	Deps     session.Deps
	BootLine func(runID string) string // the line spawn types (session.BootLine)
	// StatusLine is the status line command for a run (wipnote delegate
	// status-line ...), written into the worker's --settings.
	StatusLine   func(runID string) string
	ClaudeConfig string // the config to pre-seed; ClaudeConfigPath() when empty
	ClaudeDir    string // where transcripts live; ClaudeDataDir() when empty
	StartTimeout time.Duration
	ExitWait     time.Duration
	NewSessionID func() string       // a random UUID when nil
	Signal       func(pid int) error // SIGTERM to one pid; syscall.Kill when nil
}

func (w *WorkerOptions) startTimeout() time.Duration {
	if w.StartTimeout > 0 {
		return w.StartTimeout
	}
	return DefaultStartTimeout
}

func (w *WorkerOptions) exitWait() time.Duration {
	if w.ExitWait > 0 {
		return w.ExitWait
	}
	return DefaultWorkerExitWait
}

func (w *WorkerOptions) signal(pid int) error {
	if w.Signal != nil {
		return w.Signal(pid)
	}
	return syscall.Kill(pid, syscall.SIGTERM)
}

// worker is one interactive Claude run in flight.
type worker struct {
	o    Options
	w    *WorkerOptions
	deps session.Deps
	plan claudePlan
	sp   session.Spawned
	rf   *runstore.RunFolder
	info session.Info
}

// runClaudeWorker runs r as an interactive worker when it can. ok false
// means the claude -p path runs instead (a note says why); an error refuses
// the run before anything started.
func runClaudeWorker(ctx context.Context, o Options, r Recipe) (Result, bool, error) {
	if o.Worker == nil || r.Kind != KindClaude || o.Container || !o.inHerdr() {
		return Result{}, false, nil
	}
	plan, why := planClaude(r.Argv)
	if why == "" && o.WorkItem == "" {
		why = "the run has no work item (a worker needs one)"
	}
	if why != "" {
		fmt.Fprintf(o.Stderr, "wipnote: claude runs as claude -p here: %s\n", why)
		return Result{}, false, nil
	}
	if d := plan.missingPluginDir(o.Cwd); d != "" {
		return Result{}, false, fmt.Errorf("delegate run: plugin_not_loaded: claude --plugin-dir %s is not a directory; claude would start without it and say nothing", d)
	}
	warnPlan(o.Stderr, plan)
	wk, err := spawnWorker(ctx, o, plan)
	if err != nil {
		fmt.Fprintf(o.Stderr, "wipnote: claude runs as claude -p here: the worker did not start: %v\n", err)
		return Result{}, false, nil
	}
	v := wk.watch(ctx)
	wk.end(ctx, v)
	return wk.report(r, v), true, nil
}

// warnPlan names what the interactive launch changes.
func warnPlan(stderr io.Writer, p claudePlan) {
	if p.PlanMode {
		fmt.Fprintln(stderr, "wipnote: warning: plan mode is set; a probe saw plan mode run the turn on another model than --model, while the header still names the one asked for")
	}
	if len(p.Dropped) > 0 {
		fmt.Fprintf(stderr, "wipnote: note: %s are print-only and have no effect on an interactive worker; they were dropped\n", strings.Join(p.Dropped, ", "))
	}
}

// spawnWorker pre-seeds trust and onboarding, then spawns the worker with
// the prompt as its brief and the launch args after it.
func spawnWorker(ctx context.Context, o Options, plan claudePlan) (*worker, error) {
	w := o.Worker
	deps := w.Deps
	deps.Store, deps.Ledger, deps.Herdr = o.Store, o.Ledger, o.Herdr
	sid := plan.SessionID
	if sid == "" {
		sid = newSessionID(w)
	}
	// The worker runs where the caller ran, as the claude -p path does: a
	// linked worktree stays the worktree (the launcher is told by
	// --work-dir), and trust and the transcript are keyed on that dir.
	dir := filepath.Clean(o.Cwd)
	if err := preseedClaude(firstNonEmpty(w.ClaudeConfig, ClaudeConfigPath()), dir); err != nil {
		fmt.Fprintf(o.Stderr, "wipnote: warning: workspace trust was not pre-seeded: %v\n", err)
	}
	so := session.SpawnOptions{
		Harness: session.HarnessClaude, WorkItem: o.WorkItem, WorkTitle: o.WorkTitle, CallerSession: o.CallerSession,
		CallerPane: o.HerdrEnv.CallerPane, Brief: plan.Prompt, Cwd: o.Cwd, Root: o.Root, BootLine: w.BootLine,
		SessionID: sid, Transcript: TranscriptPath(firstNonEmpty(w.ClaudeDir, ClaudeDataDir()), dir, sid), WorkDir: dir,
		LaunchArgs: func(rf *runstore.RunFolder) ([]string, error) { return launchArgs(rf, o, plan, sid) },
		Poll:       o.poll(),
	}
	sp, err := session.Spawn(ctx, deps, so)
	if err != nil {
		return nil, err
	}
	rf, err := o.Store.Run(sp.Run)
	if err != nil {
		return nil, err
	}
	info, err := session.ReadInfo(rf)
	return &worker{o: o, w: w, deps: deps, plan: plan, sp: sp, rf: rf, info: info}, err
}

// launchArgs are the caller's interactive flags, then the known session id
// (unless the caller gave one) and the --settings file with the status
// line command (the caller's own settings merged in).
func launchArgs(rf *runstore.RunFolder, o Options, plan claudePlan, sid string) ([]string, error) {
	cmd := ""
	if o.Worker.StatusLine != nil {
		cmd = o.Worker.StatusLine(rf.ID())
	}
	settings, err := writeWorkerSettings(rf, plan.Settings, o.Cwd, cmd)
	if err != nil {
		return nil, err
	}
	args := absPaths(withoutFlag(plan.Args, "--settings"), o.Cwd)
	if plan.SessionID == "" {
		args = append(args, "--session-id", sid)
	}
	return append(args, "--settings", settings), nil
}

// withoutFlag drops name and its value (both spellings) from args.
func withoutFlag(args []string, name string) []string {
	var out []string
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == name:
			i++
		case strings.HasPrefix(args[i], name+"="):
		default:
			out = append(out, args[i])
		}
	}
	return out
}

func newSessionID(w *WorkerOptions) string {
	if w.NewSessionID != nil {
		return w.NewSessionID()
	}
	return uuid.NewString()
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
