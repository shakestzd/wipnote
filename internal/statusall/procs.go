package statusall

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/internal/procsweep"
	"github.com/shakestzd/wipnote/internal/registry"
)

// procsState is the run's one process sweep, its orphans keyed by registry id.
type procsState struct {
	byID  map[string][]procsweep.Proc
	state string // Sources["procs"]
}

// sweepProcs runs procsweep once for every readable registered project. The
// sweep is keyed by registry id, not name, so two projects with one name are
// never mixed up. It is report-only: procsweep never signals a process.
func sweepProcs(ctx context.Context, o Options) procsState {
	if o.Procs == nil {
		return procsState{state: o.ProcsReason}
	}
	var projects []procsweep.Project
	for _, e := range o.Entries {
		if readable(e.ProjectDir) == nil {
			projects = append(projects, procsweep.Project{Name: e.ID, WipnoteDir: filepath.Join(e.ProjectDir, ".wipnote")})
		}
	}
	res, err := procsweep.Sweep(ctx, projects, procsweep.Options{Lister: o.Procs, Timeout: o.ProcsTimeout})
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
