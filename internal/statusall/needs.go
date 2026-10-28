package statusall

import (
	"fmt"
	"sort"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

// needs lists what waits on the owner: blocked herdr panes, failed runs not
// since re-run green, and runs with no end whose session has ended. Only runs
// inside window count, so an old failure does not stay forever.
func needs(agents []Agent, runs []runledger.Record, now time.Time, window time.Duration, loc *time.Location) []Need {
	out := []Need{}
	for _, a := range agents {
		if a.State == string(herdr.StateBlocked) {
			out = append(out, Need{Kind: NeedBlockedAgent, What: blockedWhat(a), WorkItem: a.WorkItem})
		}
	}
	cutoff := now.Add(-window)
	for i, r := range runs {
		switch {
		case r.Status == runledger.StatusFailed && r.EndedAt.After(cutoff) && !rerunGreen(runs[i+1:], r):
			what := runledger.Describe(r, loc)
			if r.FirstError != "" {
				what += ": " + r.FirstError
			}
			out = append(out, Need{Kind: NeedFailedRun, What: what, Since: stamp(r.EndedAt), WorkItem: r.WorkItem})
		case r.Status == runledger.StatusUnknown && r.StartedAt.After(cutoff):
			what := r.Label() + " has no recorded end and its session has ended; it may still be running"
			out = append(out, Need{Kind: NeedLostRun, What: what, Since: stamp(r.StartedAt), WorkItem: r.WorkItem})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return needRank(out[i]) < needRank(out[j]) })
	return out
}

func blockedWhat(a Agent) string {
	what := fmt.Sprintf("%s agent in pane %s is blocked", a.Harness, a.Pane)
	if a.WorkItem != "" {
		what += " on " + a.WorkItem
		if a.Title != "" {
			what += " " + a.Title
		}
	}
	return what
}

// rerunGreen reports whether a later run with the same label completed.
// later is in start order, after r.
func rerunGreen(later []runledger.Record, r runledger.Record) bool {
	for _, l := range later {
		if l.Label() == r.Label() && l.Status == runledger.StatusCompleted {
			return true
		}
	}
	return false
}

// needRank puts blocked panes first: they hold an agent up right now.
func needRank(n Need) int {
	if n.Kind == NeedBlockedAgent {
		return 0
	}
	return 1
}
