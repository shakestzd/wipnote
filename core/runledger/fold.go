package runledger

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// Record is one run, folded from its events.
type Record struct {
	ID          string
	TaskID      string
	ToolUseID   string
	Kind        string
	Description string
	Command     string
	Folder      string
	StartedAt   time.Time // zero when only the end was seen
	EndedAt     time.Time // zero while running
	Status      string    // running | completed | failed | killed | unknown
	Exit        *int
	LogPath     string
	LastLine    string
	FirstError  string
	Commit      string
	Session     string
	WorkItem    string
	Harness     string
}

// Fold joins events into runs. Events of one run share a run id; an end that
// was written under a different key (a notification with no tool-use id) is
// joined to its start by tool-use id or task id. The latest end wins, since a
// subagent may notify more than once. A run with no end is running, or unknown
// once sessionEnded reports its session over.
func Fold(events []Event, sessionEnded func(string) bool) []Record {
	sorted := append([]Event(nil), events...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].At.Before(sorted[j].At) })

	var recs []*Record
	for _, e := range sorted {
		r := findRecord(recs, e)
		if r == nil {
			r = &Record{ID: e.RunID}
			recs = append(recs, r)
		}
		applyEvent(r, e)
	}
	out := make([]Record, 0, len(recs))
	for _, r := range recs {
		if r.Status == "" {
			r.Status = StatusRunning
			if sessionEnded != nil && sessionEnded(r.Session) {
				r.Status = StatusUnknown
			}
		}
		out = append(out, *r)
	}
	sort.SliceStable(out, func(i, j int) bool { return sortTime(out[i]).Before(sortTime(out[j])) })
	return out
}

func findRecord(recs []*Record, e Event) *Record {
	for _, r := range recs {
		if r.ID == e.RunID {
			return r
		}
	}
	for _, r := range recs {
		sameSession := r.Session == "" || e.Session == "" || r.Session == e.Session
		if !sameSession {
			continue
		}
		if (e.ToolUseID != "" && r.ToolUseID == e.ToolUseID) || (e.TaskID != "" && r.TaskID == e.TaskID) {
			return r
		}
	}
	return nil
}

// applyEvent merges e into r. Starts fill what is missing; ends replace the
// end fields, so the newest end is the one kept.
func applyEvent(r *Record, e Event) {
	fill := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	for _, f := range []struct {
		dst *string
		v   string
	}{
		{&r.TaskID, e.TaskID}, {&r.ToolUseID, e.ToolUseID}, {&r.Kind, e.Kind},
		{&r.Description, e.Description}, {&r.Command, e.Command}, {&r.Folder, e.Folder},
		{&r.LogPath, e.LogPath}, {&r.Commit, e.Commit}, {&r.Session, e.Session},
		{&r.WorkItem, e.WorkItem}, {&r.Harness, e.Harness},
	} {
		fill(f.dst, f.v)
	}
	if e.Phase == PhaseStart {
		if r.StartedAt.IsZero() || e.At.Before(r.StartedAt) {
			r.StartedAt = e.At
		}
		return
	}
	if r.EndedAt.IsZero() || !e.At.Before(r.EndedAt) {
		r.EndedAt, r.Status, r.Exit = e.At, e.Status, e.Exit
		r.LastLine, r.FirstError = e.LastLine, e.FirstError
	}
}

func sortTime(r Record) time.Time {
	if !r.StartedAt.IsZero() {
		return r.StartedAt
	}
	return r.EndedAt
}

// ForWorkItem returns the runs linked to one work item, in order.
func ForWorkItem(recs []Record, workItem string) []Record {
	var out []Record
	for _, r := range recs {
		if workItem != "" && r.WorkItem == workItem {
			out = append(out, r)
		}
	}
	return out
}

// Label is the run's name: its description, else its command, else its kind.
func (r Record) Label() string {
	for _, v := range []string{r.Description, r.Command, r.Kind} {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return r.ID
}

// Describe renders one run as a line, e.g. "gate exit 0 at 16:31 on commit
// abc1234". Times are shown in loc.
func Describe(r Record, loc *time.Location) string {
	var b strings.Builder
	b.WriteString(r.Label())
	switch r.Status {
	case StatusRunning:
		fmt.Fprintf(&b, " running since %s", clock(r.StartedAt, loc))
	case StatusUnknown:
		fmt.Fprintf(&b, " unknown (no end recorded; started %s)", clock(r.StartedAt, loc))
	default:
		if r.Exit != nil {
			fmt.Fprintf(&b, " exit %d", *r.Exit)
		} else {
			b.WriteString(" " + r.Status)
		}
		if r.Exit != nil && r.Status != StatusCompleted && r.Status != StatusFailed {
			b.WriteString(" " + r.Status)
		}
		fmt.Fprintf(&b, " at %s", clock(r.EndedAt, loc))
	}
	if r.Commit != "" {
		fmt.Fprintf(&b, " on commit %s", shortSHA(r.Commit))
	}
	return b.String()
}

func clock(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return "?"
	}
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("15:04")
}

func shortSHA(sha string) string {
	if len(sha) > 7 {
		return sha[:7]
	}
	return sha
}
