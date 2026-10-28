package statusall

import (
	"sort"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
)

// recentRuns is the newest limit runs, newest first. recs is in start order.
func recentRuns(recs []runledger.Record, limit int, loc *time.Location) []Run {
	out := []Run{}
	for i := len(recs) - 1; i >= 0 && len(out) < limit; i-- {
		out = append(out, toRun(recs[i], loc))
	}
	return out
}

func toRun(r runledger.Record, loc *time.Location) Run {
	return Run{
		ID: r.ID, Kind: r.Kind, Description: r.Description, Command: r.Command, Folder: r.Folder,
		Status: r.Status, Exit: r.Exit, StartedAt: stamp(r.StartedAt), EndedAt: stamp(r.EndedAt),
		Commit: r.Commit, LogPath: r.LogPath, LastLine: r.LastLine, FirstError: r.FirstError,
		WorkItem: r.WorkItem, Session: r.Session, Harness: r.Harness, Line: runledger.Describe(r, loc),
	}
}

func sortItems(items []Item) {
	sort.Slice(items, func(i, j int) bool { return items[i].ID < items[j].ID })
}
