package procsweep

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// The orphan rule decides from the session's LATEST run (bug-2a96e9cb):
//
//   - latest run open             → live, whenever the process started;
//   - latest run ended, no start  → orphan;
//   - latest run ended, started at or before the latest end + endClockTolerance
//     → orphan (a leftover of run 1 or of a resumed run alike);
//   - latest run ended, started after that → not an orphan: no recorded run
//     covers it (a resume whose start is not written yet).
const (
	resumedEnded = "44444444-4444-4444-8444-444444444444"
	resumedOpen  = "55555555-5555-4555-8555-555555555555"
)

var (
	runOneStart = time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	runOneEnd   = runOneStart.Add(time.Hour)
	runTwoStart = runOneEnd.Add(time.Hour)
	runTwoEnd   = runTwoStart.Add(time.Hour)
)

// resumedProject records two sessions that ended and were resumed through the
// ledger: resumedEnded exited again, resumedOpen is still in its second run.
func resumedProject(t *testing.T) Project {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	dir := filepath.Join(t.TempDir(), ".wipnote")
	store := sessionledger.NewStore(dir)
	for _, id := range []string{resumedEnded, resumedOpen} {
		open := sessionledger.Record{SessionID: id, Harness: "claude", StartedAt: runOneStart}
		if _, err := store.Open(open); err != nil {
			t.Fatalf("open %s: %v", id, err)
		}
		if err := store.Close(id, runOneEnd); err != nil {
			t.Fatalf("close %s: %v", id, err)
		}
		open.StartedAt = runTwoStart
		if _, err := store.Open(open); err != nil {
			t.Fatalf("resume %s: %v", id, err)
		}
	}
	if err := store.Close(resumedEnded, runTwoEnd); err != nil {
		t.Fatalf("close resumed run: %v", err)
	}
	return Project{Name: "demo", WipnoteDir: dir}
}

func resumedOrphan(t *testing.T, session string, start time.Time) bool {
	t.Helper()
	rp := raw(400, "npm run dev", map[string]string{"CLAUDE_CODE_SESSION_ID": session})
	rp.StartedAt = start
	res, err := Sweep(context.Background(), []Project{resumedProject(t)}, Options{Lister: fakeTable(rp), Self: -1})
	if err != nil {
		t.Fatalf("Sweep: %v", err)
	}
	p, ok := byPID(res, 400)
	if !ok {
		t.Fatalf("process of %s not found", session)
	}
	return p.Orphan
}

func TestSweep_ResumedSessionOrphanRule(t *testing.T) {
	cases := []struct {
		name    string
		session string
		start   time.Time
		orphan  bool
	}{
		{"resumed then exited: started during run 2", resumedEnded, runTwoStart.Add(30 * time.Minute), true},
		{"resumed then exited: left over from run 1", resumedEnded, runOneStart.Add(30 * time.Minute), true},
		{"resumed then exited: started between runs", resumedEnded, runOneEnd.Add(30 * time.Minute), true},
		{"resumed then exited: within tolerance of the latest end", resumedEnded, runTwoEnd.Add(endClockTolerance - time.Millisecond), true},
		{"resumed then exited: after the latest end", resumedEnded, runTwoEnd.Add(time.Minute), false},
		{"resumed then exited: unknown start", resumedEnded, time.Time{}, true},
		{"run 2 open: started during run 2", resumedOpen, runTwoStart.Add(30 * time.Minute), false},
		{"run 2 open: left over from run 1", resumedOpen, runOneStart.Add(30 * time.Minute), false},
		{"run 2 open: unknown start", resumedOpen, time.Time{}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := resumedOrphan(t, c.session, c.start); got != c.orphan {
				t.Errorf("orphan = %v, want %v", got, c.orphan)
			}
		})
	}
}
