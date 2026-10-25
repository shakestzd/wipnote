package oneshot

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Options are one `wipnote delegate run`.
type Options struct {
	Argv          []string // the tool argv after --
	Label         string   // explicit --label: the only label the ledger keeps
	WorkItem      string   // the row's work item (the caller resolves the default)
	CallerSession string
	NoPane        bool // force headless inside herdr
	Cwd           string
	Settings      Settings
	Store         *runstore.Store
	Ledger        *delegationledger.Store
	Herdr         herdr.Client // nil outside herdr
	HerdrEnv      herdr.Env
	// PaneLine is the shell line that runs the pane worker for a run id.
	PaneLine  func(runID string) string
	CodexHome string // where codex profiles live; CODEX_HOME or ~/.codex when empty
	Stdout    io.Writer
	Stderr    io.Writer
	Poll      time.Duration // pane wait; 1 s (the mod's) when zero
	CcxWait   time.Duration // DefaultCcxWait when zero
	KeepAge   time.Duration // sweep age; runstore.DefaultSweepAge when zero
}

func (o Options) poll() time.Duration {
	if o.Poll > 0 {
		return o.Poll
	}
	return time.Second
}

// Result is how the run ended.
type Result struct {
	RunID     string
	Exit      int
	EndReason string
}

// Run classifies the argv, opens the ledger row and the run folder, runs the
// tool in a herdr pane (inside herdr) or headless, prints the report, closes
// and enriches the row and sweeps old runs. A refused argv returns an error
// before anything is written. Exit is the tool's; 130 when its pane closed.
func Run(ctx context.Context, o Options) (Result, error) {
	r, err := Classify(o.Argv)
	if err != nil {
		return Result{}, err
	}
	rec, err := o.Ledger.Open(delegationledger.Record{
		Mode: delegationledger.ModeOneshot, Harness: string(r.Kind), Label: strings.TrimSpace(o.Label),
		WorkItem: o.WorkItem, CallerSession: o.CallerSession, Host: ledgerHost(r),
	})
	if err != nil {
		return Result{}, err
	}
	rf, err := o.Store.CreateRun(rec.ID)
	if err != nil {
		_ = o.Ledger.Close(rec.ID, delegationledger.CloseInfo{Reason: delegationledger.EndGone})
		return Result{}, err
	}
	res, err := runFolder(ctx, o, r, rf)
	if err != nil {
		return res, err
	}
	closeRow(o.Ledger, rf, res)
	sweep(o)
	return res, nil
}

// ledgerHost: ccx means container Claude (decision C).
func ledgerHost(r Recipe) string {
	if r.Kind == KindCcx {
		return "container"
	}
	return "host"
}

func runFolder(ctx context.Context, o Options, r Recipe, rf *runstore.RunFolder) (Result, error) {
	label := strings.TrimSpace(o.Label)
	if label == "" {
		label = r.DerivedLabel()
	}
	_ = rf.WriteLabel(label)
	job := buildJob(o, r, rf)
	res := Result{RunID: rf.ID(), EndReason: delegationledger.EndExit}
	exit, closed, err := dispatch(ctx, o, r, rf, job)
	if err != nil {
		return res, err
	}
	res.Exit = exit
	if ctx.Err() != nil && !closed {
		res.EndReason = delegationledger.EndStop // interrupted: the tool was stopped
	}
	page, _ := render.WritePage(rf)
	if closed {
		res.EndReason = delegationledger.EndPaneClosed
		return res, nil
	}
	printReport(o, r, rf, page)
	return res, nil
}

func buildJob(o Options, r Recipe, rf *runstore.RunFolder) Job {
	home := o.CodexHome
	if home == "" {
		home = codexHome()
	}
	argv := r.Command(CommandOptions{LastFile: filepath.Join(rf.Dir(), runstore.FileLastMD),
		LeanProfile: o.Settings.leanProfile(), CodexHome: home})
	own, _ := r.OwnLastFile()
	wait := o.CcxWait
	if wait <= 0 {
		wait = DefaultCcxWait
	}
	return Job{Kind: r.Kind, Argv: argv, Cwd: o.Cwd, OwnLast: own, CcxRuns: ccxRunsDir(o.Settings.CcxRunsDir),
		CcxWaitMS: wait.Milliseconds(), ClosePane: o.Settings.ClosePane, CloseDelay: o.Settings.CloseDelay}
}

func codexHome() string {
	if h := os.Getenv("CODEX_HOME"); h != "" {
		return h
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".codex")
}

// inHerdr: a pane needs HERDR_ENV=1, a client and the herdr binary.
func (o Options) inHerdr() bool {
	if o.NoPane || o.Herdr == nil || !o.HerdrEnv.InHerdr {
		return false
	}
	if _, ok := o.Herdr.(*herdr.CLI); ok {
		if _, err := exec.LookPath("herdr"); err != nil {
			return false
		}
	}
	return true
}

// dispatch runs the job in a pane when it can have one, else headless.
func dispatch(ctx context.Context, o Options, r Recipe, rf *runstore.RunFolder, job Job) (exit int, closed bool, err error) {
	if o.inHerdr() {
		pane, registry, note := placePane(ctx, o)
		if note != "" {
			io.WriteString(o.Stdout, note+"\n")
		}
		if pane != "" {
			job.Pane, job.Registry = string(pane), registry
			if err := writeJob(rf, job); err == nil {
				_ = rf.WriteHost(runstore.HostHerdr)
				exit, closed, ok, err := runInPane(ctx, o, rf, r, pane)
				if ok || err != nil {
					return exit, closed, err
				}
			}
			unregister(registry, pane)
			_ = o.Herdr.Close(ctx, pane) // herdr would not run the line: leave no empty pane
			job.Pane, job.Registry = "", ""
		}
	}
	_ = writeJob(rf, job)
	_ = rf.WriteHost(runstore.HostNone)
	return Execute(ctx, rf, job), false, nil
}

func printReport(o Options, r Recipe, rf *runstore.RunFolder, page string) {
	info, _ := rf.Read()
	read := func(name string) string {
		b, _, _ := rf.ReadFrom(name, 0)
		return string(b)
	}
	if r.Kind == KindCcx {
		out, errOut := CcxReport(CcxReportInput{Dir: rf.Dir(), Out: read(runstore.FileOut), Err: read(runstore.FileRawLog),
			MaxLines: o.Settings.MaxReturnLines, Page: page})
		io.WriteString(o.Stdout, out)
		io.WriteString(o.Stderr, errOut)
		return
	}
	var secs int64
	if !info.Start.IsZero() && !info.End.IsZero() {
		secs = int64(info.End.Sub(info.Start) / time.Second)
	}
	io.WriteString(o.Stdout, Report(ReportInput{Name: r.Name(), Dir: rf.Dir(), Exit: info.Exit, Seconds: secs, Usage: info.Usage,
		Last: read(runstore.FileLastMD), CleanLog: read(runstore.FileCleanLog),
		TailLines: o.Settings.TailLines, MaxLines: o.Settings.MaxReturnLines, Page: page}))
}

// closeRow stamps the end and the usage the run folder recorded.
func closeRow(l *delegationledger.Store, rf *runstore.RunFolder, res Result) {
	exit := res.Exit
	_ = l.Close(res.RunID, delegationledger.CloseInfo{Ended: time.Now().UTC(), Exit: &exit, Reason: res.EndReason})
	info, _ := rf.Read()
	if in, cached, out, ok := UsageCounts(info.Usage); ok {
		_, _ = l.Enrich(res.RunID, delegationledger.Enrichment{UsageInput: &in, UsageCached: &cached, UsageOutput: &out})
	}
}

// sweep removes finished runs and ccx claims older than the keep age (the
// mod's CODEX_HERDR_KEEP_DAYS sweep, 7 days). There are no generated
// scripts to sweep.
func sweep(o Options) {
	keep := o.KeepAge
	if keep <= 0 {
		keep = runstore.DefaultSweepAge
	}
	now := time.Now()
	_, _ = o.Store.Sweep(now, keep)
	sweepClaims(keep, now)
}
