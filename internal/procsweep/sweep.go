package procsweep

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// DefaultTimeout bounds one sweep when Options.Timeout is zero.
const DefaultTimeout = 5 * time.Second

// endClockTolerance absorbs skew between the ledger's recorded end and a
// process start time: a process started up to this long after the end is
// still treated as a leftover of the run that ended.
const endClockTolerance = 2 * time.Second

// Project is one project whose sessions ledger attributes processes.
type Project struct {
	// Name is the display name (a registry name or the folder's base name),
	// never a host path.
	Name string
	// WipnoteDir is the project's .wipnote directory, read-only here.
	WipnoteDir string
}

// Options configures Sweep. The zero value is the production configuration.
type Options struct {
	// Lister lists processes; nil means DefaultLister().
	Lister Lister
	// Timeout bounds the listing; zero means DefaultTimeout.
	Timeout time.Duration
	// Self is the pid whose ancestry is left out (the shell running the sweep
	// is a session process but not one worth reporting). Zero means
	// os.Getpid(); a negative value excludes nothing.
	Self int
}

// Result is what one sweep found.
type Result struct {
	// Procs are the live processes attributed to a known session, by pid.
	Procs []Proc `json:"procs"`
	// Unattributed counts processes carrying a session id that no given
	// project's ledger knows (another project, or a session never recorded).
	Unattributed int `json:"unattributed"`
	// ProjectErrors maps a project name to why its sessions ledger could not
	// be read. That project's sessions are skipped; the others still sweep.
	ProjectErrors map[string]string `json:"project_errors,omitempty"`
}

// Orphans returns the processes whose session has ended.
func (r Result) Orphans() []Proc {
	var out []Proc
	for _, p := range r.Procs {
		if p.Orphan {
			out = append(out, p)
		}
	}
	return out
}

// Sweep lists the current user's live processes, matches each to a session
// in the given projects' ledgers by the session id in its environment, and
// marks a process an orphan when that session has ended. It never signals
// any process. This is the entry point `wipnote reconcile` and
// `status --all` share.
func Sweep(ctx context.Context, projects []Project, opts Options) (Result, error) {
	opts = opts.withDefaults()
	sessions, projectErrs := loadSessions(projects)
	raws, err := listBounded(ctx, opts.Lister, opts.Timeout)
	if err != nil {
		return Result{}, err
	}
	skip := ancestry(raws, opts.Self)
	res := Result{ProjectErrors: projectErrs}
	for _, rp := range raws {
		if skip[rp.PID] {
			continue
		}
		id := SessionIDFromEnv(rp.Env)
		if id == "" {
			continue
		}
		s, ok := sessions[id]
		if !ok {
			res.Unattributed++
			continue
		}
		res.Procs = append(res.Procs, Proc{
			PID: rp.PID, Command: trimCommand(rp.Command), SessionID: id,
			Project: s.project, StartedAt: rp.StartedAt, Orphan: s.orphans(rp.StartedAt),
			StopHint: stopHint(rp.PID),
		})
	}
	sort.Slice(res.Procs, func(i, j int) bool { return res.Procs[i].PID < res.Procs[j].PID })
	return res, nil
}

func (o Options) withDefaults() Options {
	if o.Lister == nil {
		o.Lister = DefaultLister()
	}
	if o.Timeout <= 0 {
		o.Timeout = DefaultTimeout
	}
	if o.Self == 0 {
		o.Self = os.Getpid()
	}
	return o
}

type sessionState struct {
	project string
	latest  sessionledger.Run // the session's latest run
}

// orphans reports whether a process started at start is a leftover of this
// session. It decides from the LATEST run, which the ledger records for every
// resume (bug-2a96e9cb):
//
//   - latest run open: live, whenever the process started — a process left
//     over from an earlier run shares the id of a running session;
//   - latest run ended, start unknown (zero): it cannot be placed relative to
//     the end, so the plain rule holds — orphan;
//   - latest run ended, started at or before its end + endClockTolerance: a
//     leftover of that run or an earlier one — orphan;
//   - latest run ended, started after that: no recorded run covers it (a
//     resume whose SessionStart has not been written yet), so it is not called
//     an orphan.
func (s sessionState) orphans(start time.Time) bool {
	if s.latest.IsOpen() {
		return false
	}
	if start.IsZero() {
		return true
	}
	return !start.After(s.latest.EndedAt.Add(endClockTolerance))
}

// loadSessions indexes every readable project's ledger by session id. The
// first project to know a session claims it. A ledger that cannot be read
// skips only its own project: the reason is returned by project name (nil
// when every ledger read cleanly), so one bad ledger never hides the rest.
func loadSessions(projects []Project) (map[string]sessionState, map[string]string) {
	out := map[string]sessionState{}
	var errs map[string]string
	for _, p := range projects {
		recs, err := sessionledger.NewStore(p.WipnoteDir).ReadAll()
		if err != nil {
			if errs == nil {
				errs = map[string]string{}
			}
			errs[p.Name] = ledgerError(p, err)
			continue
		}
		for _, r := range recs {
			if _, seen := out[r.SessionID]; !seen {
				out[r.SessionID] = sessionState{project: p.Name, latest: r.LatestRun()}
			}
		}
	}
	return out, errs
}

// ledgerError describes a ledger read failure without the host path: the
// project root is stripped so the ledger shows as .wipnote/<file>.
func ledgerError(p Project, err error) string {
	root := filepath.Dir(p.WipnoteDir) + string(filepath.Separator)
	return "read sessions ledger: " + strings.ReplaceAll(err.Error(), root, "")
}

// listBounded runs the lister under a deadline. A lister that ignores its
// context (a /proc read stuck on a wedged process) is abandoned, not waited on.
func listBounded(ctx context.Context, l Lister, d time.Duration) ([]RawProc, error) {
	ctx, cancel := context.WithTimeout(ctx, d)
	defer cancel()
	type out struct {
		procs []RawProc
		err   error
	}
	ch := make(chan out, 1)
	go func() {
		p, err := l.List(ctx)
		ch <- out{p, err}
	}()
	select {
	case o := <-ch:
		return o.procs, o.err
	case <-ctx.Done():
		return nil, fmt.Errorf("%w after %s", ErrTimeout, d)
	}
}

// ancestry returns self and every ancestor of self found in the table.
func ancestry(raws []RawProc, self int) map[int]bool {
	skip := map[int]bool{}
	if self < 0 {
		return skip
	}
	parent := make(map[int]int, len(raws))
	for _, rp := range raws {
		parent[rp.PID] = rp.PPID
	}
	for pid := self; pid > 1 && !skip[pid]; pid = parent[pid] {
		skip[pid] = true
	}
	return skip
}
