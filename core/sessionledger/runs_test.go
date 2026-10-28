package sessionledger

import (
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	runT0 = time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC) // first start
	runT1 = runT0.Add(time.Hour)                          // first end
	runT2 = runT1.Add(2 * time.Hour)                      // resume
	runT3 = runT2.Add(30 * time.Minute)                   // second end
)

// resumedOnce opens, closes and resumes sidA, leaving the second run open.
func resumedOnce(t *testing.T, s *Store) {
	t.Helper()
	mustOpen(t, s, sidA, runT0)
	if err := s.Close(sidA, runT1); err != nil {
		t.Fatalf("close run 1: %v", err)
	}
	written, err := s.Open(Record{SessionID: sidA, Harness: "claude-code", StartedAt: runT2})
	if err != nil || !written {
		t.Fatalf("resume Open = written %v err %v, want a new run", written, err)
	}
}

func mustGet(t *testing.T, s *Store, id string) Record {
	t.Helper()
	rec, ok, err := s.Get(id)
	if err != nil || !ok {
		t.Fatalf("get %s: ok %v err %v", id, ok, err)
	}
	return rec
}

func wantRuns(t *testing.T, rec Record, want ...Run) {
	t.Helper()
	got := rec.RunList()
	if len(got) != len(want) {
		t.Fatalf("runs = %+v, want %+v", got, want)
	}
	for i := range want {
		if !got[i].StartedAt.Equal(want[i].StartedAt) || !got[i].EndedAt.Equal(want[i].EndedAt) {
			t.Errorf("run %d = %+v, want %+v", i+1, got[i], want[i])
		}
	}
}

// TestResumeRecordsASecondRun is bug-2a96e9cb: open, close, resume, close must
// leave one row with two runs, the first start and the second end.
func TestResumeRecordsASecondRun(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	rec := mustGet(t, s, sidA)
	if !rec.IsOpen() || !rec.StartedAt.Equal(runT0) {
		t.Fatalf("resumed row: open %v start %v, want open with the first start", rec.IsOpen(), rec.StartedAt)
	}
	wantRuns(t, rec, Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2})

	if err := s.Close(sidA, runT3); err != nil {
		t.Fatalf("close run 2: %v", err)
	}
	recs, _ := s.ReadAll()
	if len(recs) != 1 {
		t.Fatalf("got %d rows, want one row per session", len(recs))
	}
	rec = recs[0]
	if !rec.EndedAt.Equal(runT3) || !rec.StartedAt.Equal(runT0) || rec.EndSource != EndSourceLiveClose {
		t.Fatalf("row = start %v end %v src %q, want %v..%v live-close", rec.StartedAt, rec.EndedAt, rec.EndSource, runT0, runT3)
	}
	wantRuns(t, rec, Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2, EndedAt: runT3})
}

// TestReplayedOpenAddsNoRun: an Open on an OPEN row is a replayed hook, on a
// fresh row and on a resumed one alike.
func TestReplayedOpenAddsNoRun(t *testing.T) {
	s := testStore(t)
	mustOpen(t, s, sidB, runT0)
	if written, _ := s.Open(Record{SessionID: sidB, Harness: "claude-code", StartedAt: runT1}); written {
		t.Error("a replayed Open on an open row wrote something")
	}
	wantRuns(t, mustGet(t, s, sidB), Run{StartedAt: runT0})

	resumedOnce(t, s)
	if written, _ := s.Open(Record{SessionID: sidA, Harness: "claude-code", StartedAt: runT3}); written {
		t.Error("a replayed Open on a resumed, open row added a run")
	}
	wantRuns(t, mustGet(t, s, sidA), Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2})
}

// TestSecondCloseOfAResumedRunKeepsTheFirstEnd: the first recorded end of a
// run is the true one, as for run 1.
func TestSecondCloseOfAResumedRunKeepsTheFirstEnd(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	for _, end := range []time.Time{runT3, runT3.Add(time.Hour)} {
		if err := s.Close(sidA, end); err != nil {
			t.Fatalf("close: %v", err)
		}
	}
	if rec := mustGet(t, s, sidA); !rec.EndedAt.Equal(runT3) {
		t.Errorf("a second close moved the end to %v", rec.EndedAt)
	}
}

// TestLedgerWithoutRunEventsReadsAsOneRun: a ledger written before runs
// existed reads exactly as before, as one implicit run.
func TestLedgerWithoutRunEventsReadsAsOneRun(t *testing.T) {
	legacy := `<!DOCTYPE html><html><body><table data-session-ledger="true"><tbody>` +
		`<tr id="` + sidA + `" data-session-id="` + sidA + `" data-start="` + FormatTime(runT0) +
		`" data-end="` + FormatTime(runT1) + `" data-end-source="live-close"></tr>` +
		`</tbody></table></body></html>`
	recs, err := parseLedgerString(legacy)
	if err != nil || len(recs) != 1 {
		t.Fatalf("legacy parse = %+v, %v", recs, err)
	}
	if recs[0].Runs != nil {
		t.Errorf("a legacy row grew explicit runs: %+v", recs[0].Runs)
	}
	if !recs[0].StartedAt.Equal(runT0) || !recs[0].EndedAt.Equal(runT1) {
		t.Errorf("legacy row = %v..%v, want %v..%v", recs[0].StartedAt, recs[0].EndedAt, runT0, runT1)
	}
	wantRuns(t, recs[0], Run{StartedAt: runT0, EndedAt: runT1})
}

// TestOlderReadersSeeTheFirstRun: the session row itself still carries run 1
// exactly as before, so a binary that predates runs reads it unchanged.
func TestOlderReadersSeeTheFirstRun(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	if err := s.Close(sidA, runT3); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatal(err)
	}
	old, err := parseBaseRowsOnly(string(raw))
	if err != nil || len(old) != 1 {
		t.Fatalf("base-row parse = %+v, %v", old, err)
	}
	if !old[0].StartedAt.Equal(runT0) || !old[0].EndedAt.Equal(runT1) {
		t.Errorf("session row = %v..%v, want run 1 %v..%v", old[0].StartedAt, old[0].EndedAt, runT0, runT1)
	}
}

// TestRewritesKeepRuns: the read-modify-write paths (Enrich, RetagHarness)
// must carry the runs through a full rewrite.
func TestRewritesKeepRuns(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	if err := s.Close(sidA, runT3); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Enrich(sidA, Enrichment{ArchivePath: "archive/a.tar.gz"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RetagHarness(sidA, "", false); err != nil {
		t.Fatal(err)
	}
	rec := mustGet(t, s, sidA)
	wantRuns(t, rec, Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2, EndedAt: runT3})
	if rec.ArchivePath != "archive/a.tar.gz" || rec.Harness != "claude" {
		t.Errorf("rewrite lost fields: %+v", rec)
	}
}

// TestEnrichAndCorrectTargetTheLatestRun: an end from a reconcile pass that
// predates the resume describes an earlier run and must not close the live
// one; a later end closes the latest run.
func TestEnrichAndCorrectTargetTheLatestRun(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	if changed, _ := s.Enrich(sidA, Enrichment{EndedAt: runT1, EndSource: EndSourceLastActivity}); changed {
		t.Error("an end from before the resume closed the resumed run")
	}
	if c, _ := s.Correct(sidA, runT1.Add(time.Minute), EndSourceSessionRecord, false); c.Applied {
		t.Errorf("Correct applied an end before the latest run's start: %+v", c)
	}
	if !mustGet(t, s, sidA).IsOpen() {
		t.Fatal("the resumed run was closed by an earlier run's end")
	}
	if _, err := s.Enrich(sidA, Enrichment{EndedAt: runT3, EndSource: EndSourceLastActivity}); err != nil {
		t.Fatal(err)
	}
	if c, _ := s.Correct(sidA, runT3.Add(time.Minute), EndSourceSessionRecord, false); !c.Applied {
		t.Errorf("Correct did not repair the latest run: %+v", c)
	}
	rec := mustGet(t, s, sidA)
	wantRuns(t, rec, Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2, EndedAt: runT3.Add(time.Minute)})
	if rec.EndSource != EndSourceSessionRecord {
		t.Errorf("end source = %q, want session-record", rec.EndSource)
	}
}

// TestEnsureNeverResumes: a late writer (a CLI command run by a leftover
// process) must not reopen a closed session.
func TestEnsureNeverResumes(t *testing.T) {
	s := testStore(t)
	mustOpen(t, s, sidA, runT0)
	if err := s.Close(sidA, runT1); err != nil {
		t.Fatal(err)
	}
	written, err := s.Ensure(Record{SessionID: sidA, Harness: "claude", StartedAt: runT2})
	if err != nil || written {
		t.Fatalf("Ensure on a closed row = written %v err %v, want no change", written, err)
	}
	if rec := mustGet(t, s, sidA); rec.IsOpen() || rec.Runs != nil {
		t.Errorf("Ensure reopened the session: %+v", rec)
	}
	if written, _ := s.Ensure(Record{SessionID: sidB, Harness: "claude", StartedAt: runT2}); !written {
		t.Error("Ensure did not record an unrecorded session")
	}
}

// TestConcurrentResumesAppendOneRun: many SessionStart hooks racing on a
// closed row append exactly one run; the rest see it open and replay.
func TestConcurrentResumesAppendOneRun(t *testing.T) {
	s := testStore(t)
	mustOpen(t, s, sidA, runT0)
	if err := s.Close(sidA, runT1); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	var mu sync.Mutex
	writes := 0
	for i := 0; i < 16; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			w, err := s.Open(Record{SessionID: sidA, Harness: "claude-code", StartedAt: runT2.Add(time.Duration(i) * time.Second)})
			if err != nil {
				t.Errorf("open: %v", err)
			}
			if w {
				mu.Lock()
				writes++
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()
	if writes != 1 {
		t.Errorf("%d concurrent resumes wrote, want exactly 1", writes)
	}
	raw, _ := os.ReadFile(s.Path())
	if n := strings.Count(string(raw), `data-run-event="start"`); n != 1 {
		t.Errorf("ledger holds %d run-start events, want 1", n)
	}
	if rec := mustGet(t, s, sidA); len(rec.RunList()) != 2 || !rec.IsOpen() {
		t.Errorf("after concurrent resumes: %+v", rec)
	}
}

// parseBaseRowsOnly reads markup the way a binary that predates runs does:
// session rows only, run events invisible.
func parseBaseRowsOnly(markup string) ([]Record, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(markup))
	if err != nil {
		return nil, err
	}
	var records []Record
	doc.Find(rowSelector).Each(func(_ int, row *goquery.Selection) {
		if r, ok := parseRow(row); ok {
			records = append(records, r)
		}
	})
	return records, nil
}

// TestStaleEndDoesNotCloseAResumedRun: an end note from run 1 that is caught
// up only after the resume (SessionEnd catch-up) predates the live run's start.
// It describes the earlier run, so Close must leave the resumed run open
// instead of clamping it to the start and closing it.
func TestStaleEndDoesNotCloseAResumedRun(t *testing.T) {
	s := testStore(t)
	resumedOnce(t, s)
	if err := s.Close(sidA, runT1); err != nil {
		t.Fatalf("stale close: %v", err)
	}
	rec := mustGet(t, s, sidA)
	if !rec.IsOpen() {
		t.Fatalf("stale end closed the resumed run: %+v", rec.RunList())
	}
	wantRuns(t, rec, Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2})

	// The resumed run's own end still closes it.
	if err := s.Close(sidA, runT3); err != nil {
		t.Fatalf("close run 2: %v", err)
	}
	wantRuns(t, mustGet(t, s, sidA), Run{StartedAt: runT0, EndedAt: runT1}, Run{StartedAt: runT2, EndedAt: runT3})
}
