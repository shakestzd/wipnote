package statusall

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/internal/procsweep"
	"github.com/shakestzd/wipnote/internal/registry"
)

// procsState is the run's one process sweep, its orphans keyed by registry id.
type procsState struct {
	byID  map[string][]procsweep.Proc
	state string // Sources["procs"]
}

// sweepGrace is how long past its own bound the sweep may run before the
// report stops waiting for it.
const sweepGrace = time.Second

// sweepProcs runs procsweep once for every readable registered project. The
// sweep is keyed by registry id, not name, so two projects with one name are
// never mixed up. It is report-only: procsweep never signals a process.
//
// The project checks and the ledger reads inside the sweep do not observe ctx,
// so a stalled project disk could hang them; the whole sweep is waited on only
// up to its bound, and the report goes on without it.
func sweepProcs(ctx context.Context, o Options) procsState {
	if o.Procs == nil {
		return procsState{state: o.ProcsReason}
	}
	ch := make(chan procsState, 1)
	go func() { ch <- sweepAll(ctx, o) }()
	t := time.NewTimer(o.ProcsTimeout + sweepGrace)
	defer t.Stop()
	select {
	case ps := <-ch:
		return ps
	case <-t.C:
		return procsState{state: SourceNotAvailable + ": process sweep timed out"}
	case <-ctx.Done():
		return procsState{state: SourceNotAvailable + ": " + ctx.Err().Error()}
	}
}

// sweepAll is the sweep itself, unbounded.
func sweepAll(ctx context.Context, o Options) procsState {
	var projects []procsweep.Project
	for _, e := range o.Entries {
		if readable(e.ProjectDir) == nil {
			projects = append(projects, procsweep.Project{Name: e.ID, WipnoteDir: filepath.Join(e.ProjectDir, ".wipnote")})
		}
	}
	res, err := o.sweep(ctx, projects, procsweep.Options{Lister: o.Procs, Timeout: o.ProcsTimeout})
	if err != nil {
		return procsState{state: SourceNotAvailable + ": " + err.Error()}
	}
	by := map[string][]procsweep.Proc{}
	for _, p := range res.Orphans() {
		by[p.Project] = append(by[p.Project], p)
	}
	return procsState{byID: by, state: SourceOK}
}

// projectOrphans is e's orphans, named by the registry name, with host paths
// in the command made relative or redacted.
func projectOrphans(ps procsState, e registry.Entry) []procsweep.Proc {
	out := []procsweep.Proc{}
	for _, p := range ps.byID[e.ID] {
		p.Project = e.Name
		p.Command = runledger.Clean(p.Command, e.ProjectDir)
		out = append(out, p)
	}
	return out
}

// orphanNeeds is one needs-you entry per orphaned process.
func orphanNeeds(orphans []procsweep.Proc) []Need {
	var out []Need
	for _, p := range orphans {
		out = append(out, Need{Kind: NeedOrphanProcess, Since: stamp(p.StartedAt),
			What: fmt.Sprintf("orphaned process %d — %s; stop with: %s", p.PID, p.Command, p.StopHint)})
	}
	return out
}
