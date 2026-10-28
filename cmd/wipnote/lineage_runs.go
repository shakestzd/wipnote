package main

import (
	"fmt"
	"io"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

// lineageRunsLoc is the zone run times are shown in; nil means local time.
// Tests pin it.
var lineageRunsLoc *time.Location

// lineageRun is one run in `wipnote lineage --json`.
type lineageRun struct {
	ID          string `json:"id"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
	Command     string `json:"command,omitempty"`
	Folder      string `json:"folder,omitempty"`
	Status      string `json:"status"`
	Exit        *int   `json:"exit_code,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	EndedAt     string `json:"ended_at,omitempty"`
	Commit      string `json:"commit,omitempty"`
	LogPath     string `json:"log_path,omitempty"`
	LastLine    string `json:"last_line,omitempty"`
	FirstError  string `json:"first_error,omitempty"`
	Session     string `json:"session,omitempty"`
	TaskID      string `json:"task_id,omitempty"`
	Line        string `json:"line"`
}

// loadLineageRuns returns the background runs recorded for a work item
// (feat-36658c22). A run with no end is "unknown" once its session has ended
// in the sessions ledger, else "running". Read errors yield no runs: the
// section is advisory and must never fail lineage.
func loadLineageRuns(wipnoteDir, root string, kind lineageKind) []runledger.Record {
	switch kind {
	case kindFeature, kindBug, kindSpike:
	default:
		return nil
	}
	recs, err := runledger.NewStore(wipnoteDir).Records(endedSessions(wipnoteDir))
	if err != nil {
		return nil
	}
	return runledger.ForWorkItem(recs, root)
}

// endedSessions reads the sessions ledger once and reports which sessions
// have an end.
func endedSessions(wipnoteDir string) func(string) bool {
	ended := map[string]bool{}
	if rows, err := sessionledger.NewStore(wipnoteDir).ReadAll(); err == nil {
		for _, r := range rows {
			if !r.EndedAt.IsZero() {
				ended[r.SessionID] = true
			}
		}
	}
	return func(sid string) bool { return ended[sid] }
}

// printLineageRuns renders the runs section below the tree, e.g.
// "gate exit 0 at 16:31 on commit abc1234 [shell]".
func printLineageRuns(w io.Writer, runs []runledger.Record) {
	if len(runs) == 0 {
		return
	}
	fmt.Fprintf(w, "\n  runs (%d):\n", len(runs))
	for _, r := range runs {
		fmt.Fprintf(w, "    %s  [%s]\n", runledger.Describe(r, lineageRunsLoc), r.Kind)
		if r.FirstError != "" {
			fmt.Fprintf(w, "      first error: %s\n", r.FirstError)
		}
	}
}

func lineageRunsJSON(runs []runledger.Record) []lineageRun {
	out := make([]lineageRun, 0, len(runs))
	for _, r := range runs {
		out = append(out, lineageRun{
			ID: r.ID, Kind: r.Kind, Description: r.Label(), Command: r.Command, Folder: r.Folder,
			Status: r.Status, Exit: r.Exit, StartedAt: jsonTime(r.StartedAt), EndedAt: jsonTime(r.EndedAt),
			Commit: r.Commit, LogPath: r.LogPath, LastLine: r.LastLine, FirstError: r.FirstError,
			Session: r.Session, TaskID: r.TaskID, Line: runledger.Describe(r, lineageRunsLoc),
		})
	}
	return out
}

func jsonTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}
