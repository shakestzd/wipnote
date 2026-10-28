package statusall

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/registry"
)

// Defaults for Options.
const (
	DefaultWorkers   = 4
	DefaultDeadline  = 30 * time.Second
	DefaultRunsLimit = 5
	DefaultWindow    = 24 * time.Hour
	// GitTimeout bounds one local git call.
	GitTimeout = 5 * time.Second
	// lateGrace is how long Collect waits past the deadline for a project
	// that is still reading before it reports that project as timed out.
	lateGrace = 2 * time.Second
)

// HerdrSource is the part of the herdr client status --all uses.
// *herdr.CLI satisfies it.
type HerdrSource interface {
	AgentList(ctx context.Context) ([]herdr.Agent, error)
	Read(ctx context.Context, pin herdr.Pin, lines int) (string, error)
}

// Options configures Collect. Zero values take the defaults above.
type Options struct {
	Entries   []registry.Entry
	Now       func() time.Time
	Loc       *time.Location // zone run lines are shown in; nil is local time
	Workers   int            // projects read at once
	Deadline  time.Duration  // the whole run
	RunsLimit int            // recent runs kept per project
	Window    time.Duration  // how far back failed and lost runs need the owner

	// GH returns the gh runner for one project folder; nil skips gh and
	// GHReason is reported instead (e.g. "skipped").
	GH       func(dir string) gitwatch.Runner
	GHReason string
	// Herdr lists agents; nil marks herdr HerdrReason (e.g. not available).
	Herdr       HerdrSource
	HerdrReason string
}

func (o Options) withDefaults() Options {
	if o.Now == nil {
		o.Now = time.Now
	}
	if o.Workers <= 0 {
		o.Workers = DefaultWorkers
	}
	if o.Deadline <= 0 {
		o.Deadline = DefaultDeadline
	}
	if o.RunsLimit <= 0 {
		o.RunsLimit = DefaultRunsLimit
	}
	if o.Window <= 0 {
		o.Window = DefaultWindow
	}
	if o.GHReason == "" {
		o.GHReason = SourceSkipped
	}
	if o.HerdrReason == "" {
		o.HerdrReason = SourceNotAvailable
	}
	return o
}

// Collect reads every registered project, at most o.Workers at once, and
// returns them newest activity first. It returns by o.Deadline plus a short
// grace even when a read hangs; such a project is an error row.
func Collect(ctx context.Context, o Options) Report {
	o = o.withDefaults()
	ctx, cancel := context.WithTimeout(ctx, o.Deadline)
	defer cancel()
	hs := listHerdr(ctx, o)
	placed := placeByCwd(ctx, o.Entries, &hs)
	work := func(ctx context.Context, i int) Project {
		return collectProject(ctx, o, o.Entries[i], hs, placed[i])
	}
	late := func(i int) Project {
		p := newProject(o.Entries[i])
		p.Errors = append(p.Errors, fmt.Sprintf("not read: the %s time limit was reached", o.Deadline))
		p.LastActivity = o.Entries[i].LastSeen
		return p
	}
	rows := runPool(ctx, len(o.Entries), o.Workers, o.Deadline+lateGrace, work, late)
	sortByActivity(rows)
	return Report{Schema: Schema, GeneratedAt: stamp(o.Now()), Projects: rows}
}

type result struct {
	i int
	p Project
}

// runPool runs work for 0..n-1 with at most workers at once. Indexes still
// unfinished at hardLimit, or not started before ctx ends, get late(i).
func runPool(ctx context.Context, n, workers int, hardLimit time.Duration,
	work func(context.Context, int) Project, late func(int) Project) []Project {
	out := make([]Project, n)
	done := make([]bool, n)
	ch := make(chan result, n)
	go dispatch(ctx, n, workers, ch, work, late)
	timer := time.NewTimer(hardLimit)
	defer timer.Stop()
	for got := 0; got < n; {
		select {
		case r := <-ch:
			out[r.i], done[r.i] = r.p, true
			got++
		case <-timer.C:
			for i := range out {
				if !done[i] {
					out[i] = late(i)
				}
			}
			return out
		}
	}
	return out
}

func dispatch(ctx context.Context, n, workers int, ch chan<- result,
	work func(context.Context, int) Project, late func(int) Project) {
	sem := make(chan struct{}, workers)
	for i := 0; i < n; i++ {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
			ch <- result{i, late(i)}
			continue
		}
		go func(i int) {
			defer func() { <-sem }()
			ch <- result{i, safely(ctx, i, work, late)}
		}(i)
	}
}

// safely turns a panic while reading one project into its error row.
func safely(ctx context.Context, i int, work func(context.Context, int) Project, late func(int) Project) (p Project) {
	defer func() {
		if r := recover(); r != nil {
			p = late(i)
			p.Errors = []string{fmt.Sprintf("cannot read project: internal error: %v", r)}
		}
	}()
	return work(ctx, i)
}

// sortByActivity orders rows newest activity first, then by name. Rows that
// could not be read have no activity to compare and go last.
func sortByActivity(rows []Project) {
	sort.SliceStable(rows, func(i, j int) bool {
		if ui, uj := unread(rows[i]), unread(rows[j]); ui != uj {
			return uj
		}
		ti, tj := parseStamp(rows[i].LastActivity), parseStamp(rows[j].LastActivity)
		if !ti.Equal(tj) {
			return ti.After(tj)
		}
		return rows[i].Name < rows[j].Name
	})
}

// unread reports a row that holds only an error: nothing of it was read.
func unread(p Project) bool { return len(p.Sources) == 0 }

// newProject is an empty row for e: every list empty, never nil, so the JSON
// shows [] rather than null.
func newProject(e registry.Entry) Project {
	return Project{
		Name: e.Name, ID: e.ID,
		InProgress: []Item{}, Agents: []Agent{}, Runs: []Run{}, NeedsYou: []Need{},
		Orphans: []any{}, Sources: map[string]string{}, Errors: []string{},
	}
}

// stamp renders t as RFC 3339 UTC, or "" for the zero time.
func stamp(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

func parseStamp(s string) time.Time {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
