package container

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/gateledger"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const foldRun = "dlg-0123456789ab"

// workerWrites writes, as the container worker would, its own session row
// (started and ended), its family entry and one gate run.
func workerWrites(t *testing.T, wipnoteDir, sid string, t0 time.Time) gateledger.Record {
	t.Helper()
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	t.Setenv(containerwrite.RunEnv, foldRun)
	defer t.Setenv(containerwrite.PlaceEnv, "")
	if _, err := sessionledger.NewStore(wipnoteDir).Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := agent.RegisterSessionFamily(filepath.Dir(wipnoteDir), sid, sid); err != nil {
		t.Fatal(err)
	}
	if err := sessionledger.NewStore(wipnoteDir).Close(sid, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	rec, err := gateledger.NewStore(wipnoteDir).Append(gateledger.Record{SessionID: sid, WorkItemID: "feat-1", Harness: "claude",
		ProjectType: "go", GateCommand: "go test ./...", Status: gateledger.StatusPass, CheckedAt: t0.Add(time.Minute), Source: "check"})
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func foldProject(t *testing.T) (wipnoteDir, runDir string) {
	t.Helper()
	wipnoteDir = filepath.Join(t.TempDir(), ".wipnote")
	runDir = filepath.Join(wipnoteDir, "delegate", "runs", foldRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	return wipnoteDir, runDir
}

// TestFold_SessionRowReachesHostLedger: the host folds a container worker's
// own files into the sessions ledger (host=container, the real harness, its
// end), the family index and the gate ledger; a second fold changes nothing.
func TestFold_SessionRowReachesHostLedger(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	sid := "ffffffff-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	gate := workerWrites(t, wipnoteDir, sid, t0)
	if _, err := os.Stat(sessionledger.NewStore(wipnoteDir).Path()); !os.IsNotExist(err) {
		t.Fatalf("the worker wrote the shared ledger before the fold (err %v)", err)
	}
	got, err := Fold(wipnoteDir, runDir)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sessions != 1 || got.Ended != 1 || got.Gates != 1 || got.Families != 1 {
		t.Errorf("Fold = %+v, want one of each", got)
	}
	row, ok, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if !ok || row.Host != sessionledger.HostContainer || row.Harness != "claude" || !row.EndedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("host row %+v (found %v): want host=container, claude, ended", row, ok)
	}
	if fam := agent.SessionFamilyFor(filepath.Dir(wipnoteDir), sid); fam != sid {
		t.Errorf("family %q, want %q", fam, sid)
	}
	gates, _ := gateledger.NewStore(wipnoteDir).ReadAll()
	if len(gates) != 1 || gates[0].ID != gate.ID || gates[0].Signature != gate.Signature {
		t.Errorf("gate ledger %+v, want the worker's record with its id and signature", gates)
	}
	before, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	again, err := Fold(wipnoteDir, runDir)
	after, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	if err != nil || again != (Folded{}) || string(before) != string(after) {
		t.Errorf("second fold = %+v, %v (ledger changed %v), want nothing", again, err, string(before) != string(after))
	}
	if gates, _ := gateledger.NewStore(wipnoteDir).ReadAll(); len(gates) != 1 {
		t.Errorf("second fold appended the gate again: %d records", len(gates))
	}
}

// TestFold_OpenWorkerStaysOpen is the other side of the end: a worker whose
// own file has no end yet is folded as an open row; its later end closes it.
func TestFold_OpenWorkerStaysOpen(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	sid := "abababab-1111-2222-3333-444444444444"
	t.Setenv(containerwrite.RunEnv, foldRun)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if _, err := sessionledger.NewStore(wipnoteDir).Open(sessionledger.Record{SessionID: sid, Harness: "codex", StartedAt: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	if _, err := Fold(wipnoteDir, runDir); err != nil {
		t.Fatal(err)
	}
	row, _, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if !row.IsOpen() || row.Harness != "codex" {
		t.Fatalf("row %+v: want open, codex", row)
	}
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if err := sessionledger.NewStore(wipnoteDir).Close(sid, time.Now().UTC()); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	if got, _ := Fold(wipnoteDir, runDir); got.Ended != 1 {
		t.Errorf("fold after the end = %+v, want it ended", got)
	}
}

// TestFold_RefusedInsideAContainerWorker: only the host folds; a worker
// inside never writes the shared ledgers, even through Fold.
func TestFold_RefusedInsideAContainerWorker(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	t.Setenv(containerwrite.RunEnv, foldRun)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if _, err := Fold(wipnoteDir, runDir); err == nil {
		t.Error("Fold ran inside a container worker")
	}
}

// TestFold_ResumedRunReachesHostLedger: a container worker that resumes a
// session the host already folded as ended gets its new run folded in (a
// second run, the session ended at the new end); folding the same finished
// file again changes nothing.
func TestFold_ResumedRunReachesHostLedger(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	sid := "cdcdcdcd-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	worker := func(fn func(*sessionledger.Store)) {
		t.Setenv(containerwrite.RunEnv, foldRun)
		t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
		fn(sessionledger.NewStore(wipnoteDir))
		t.Setenv(containerwrite.PlaceEnv, "")
	}
	worker(func(s *sessionledger.Store) {
		_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0})
		_ = s.Close(sid, t0.Add(time.Hour))
	})
	if _, err := Fold(wipnoteDir, runDir); err != nil {
		t.Fatal(err)
	}
	worker(func(s *sessionledger.Store) {
		_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0.Add(2 * time.Hour)})
		_ = s.Close(sid, t0.Add(3*time.Hour))
	})
	got, err := Fold(wipnoteDir, runDir)
	if err != nil || got.Resumed != 1 || got.Ended != 1 {
		t.Errorf("fold after the resume = %+v, %v; want one resumed run, ended", got, err)
	}
	row, _, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	runs := row.RunList()
	if len(runs) != 2 || !runs[1].StartedAt.Equal(t0.Add(2*time.Hour)) || !row.EndedAt.Equal(t0.Add(3*time.Hour)) {
		t.Errorf("host row runs %+v ended %v: want the resumed run", runs, row.EndedAt)
	}
	before, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	again, _ := Fold(wipnoteDir, runDir)
	after, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	if again != (Folded{}) || string(before) != string(after) {
		t.Errorf("refold = %+v (ledger changed %v), want nothing", again, string(before) != string(after))
	}
}

// TestFold_EnsureAfterEndStaysEnded: a late Ensure inside the worker after
// its session ended leaves the host's folded row ended with one run.
func TestFold_EnsureAfterEndStaysEnded(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	sid := "dededede-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	t.Setenv(containerwrite.RunEnv, foldRun)
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	s := sessionledger.NewStore(wipnoteDir)
	_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0})
	_ = s.Close(sid, t0.Add(time.Hour))
	_, _ = s.Ensure(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0.Add(2 * time.Hour)})
	t.Setenv(containerwrite.PlaceEnv, "")
	if _, err := Fold(wipnoteDir, runDir); err != nil {
		t.Fatal(err)
	}
	row, _, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if row.IsOpen() || len(row.RunList()) != 1 {
		t.Errorf("host row %+v: want one ended run", row)
	}
}

// TestFold_ResumedRunInNewFolder: a resumed container run is a new run
// folder whose own file holds only the new run of the same session. Its
// fold adds that run to the host's row (closed by the first run's fold) and
// ends it; folding either folder again writes nothing.
func TestFold_ResumedRunInNewFolder(t *testing.T) {
	wipnoteDir, firstDir := foldProject(t)
	second := "dlg-0123456789cd"
	secondDir := filepath.Join(wipnoteDir, "delegate", "runs", second)
	if err := os.MkdirAll(secondDir, 0o700); err != nil {
		t.Fatal(err)
	}
	sid := "acacacac-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	inRun := func(run string, start, end time.Time) {
		t.Setenv(containerwrite.RunEnv, run)
		t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
		s := sessionledger.NewStore(wipnoteDir)
		_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: start})
		_ = s.Close(sid, end)
		t.Setenv(containerwrite.PlaceEnv, "")
	}
	inRun(foldRun, t0, t0.Add(time.Hour))
	if _, err := Fold(wipnoteDir, firstDir); err != nil {
		t.Fatal(err)
	}
	inRun(second, t0.Add(2*time.Hour), t0.Add(3*time.Hour))
	got, err := Fold(wipnoteDir, secondDir)
	if err != nil || got.Resumed != 1 || got.Ended != 1 {
		t.Errorf("fold of the resumed run's folder = %+v, %v; want one resumed run, ended", got, err)
	}
	row, _, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if runs := row.RunList(); len(runs) != 2 || !row.EndedAt.Equal(t0.Add(3*time.Hour)) {
		t.Errorf("host row runs %+v ended %v: want both runs, ended at the resume's end", runs, row.EndedAt)
	}
	before, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	a, _ := Fold(wipnoteDir, firstDir)
	b, _ := Fold(wipnoteDir, secondDir)
	after, _ := os.ReadFile(sessionledger.NewStore(wipnoteDir).Path())
	if a != (Folded{}) || b != (Folded{}) || string(before) != string(after) {
		t.Errorf("refolds = %+v, %+v (ledger changed %v), want nothing", a, b, string(before) != string(after))
	}
}

// TestFold_ConcurrentFoldsAppendGateOnce: two host folds of one run (the
// pane worker's and the waiting caller's) must append the worker's gate
// record once. The foldGateCheck seam runs a whole second fold at the point
// where the first has decided its record is missing and has not appended
// yet. The other side: one fold alone appends it once.
func TestFold_ConcurrentFoldsAppendGateOnce(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	gate := workerWrites(t, wipnoteDir, "efefefef-0000-2222-3333-444444444444", time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC))
	second := false
	foldGateCheck = func() {
		if !second {
			second = true
			if _, err := Fold(wipnoteDir, runDir); err != nil {
				t.Error(err)
			}
		}
	}
	t.Cleanup(func() { foldGateCheck = nil })
	if _, err := Fold(wipnoteDir, runDir); err != nil {
		t.Fatal(err)
	}
	gates, _ := gateledger.NewStore(wipnoteDir).ReadAll()
	n := 0
	for _, g := range gates {
		if g.ID == gate.ID {
			n++
		}
	}
	if !second || n != 1 {
		t.Errorf("gate %s recorded %d times (second fold ran: %v), want once", gate.ID, n, second)
	}
}

// TestFold_SnapshotAfterFoldLock: a fold that loaded the worker's session
// files, then waited for the fold lock while another fold folded a resumed
// run, must not fold from its stale snapshot. The foldBeforeLock seam runs
// the resume and a whole second fold at that point. The other side: the
// first fold then finds nothing new, and the host row has both runs once.
func TestFold_SnapshotAfterFoldLock(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	sid := "f0f0f0f0-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	inWorker := func(fn func(*sessionledger.Store)) {
		t.Setenv(containerwrite.RunEnv, foldRun)
		t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
		fn(sessionledger.NewStore(wipnoteDir))
		t.Setenv(containerwrite.PlaceEnv, "")
	}
	inWorker(func(s *sessionledger.Store) {
		_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0})
		_ = s.Close(sid, t0.Add(time.Hour))
	})
	raced := false
	foldBeforeLock = func() {
		if raced {
			return
		}
		raced = true
		inWorker(func(s *sessionledger.Store) { // the resume, still running
			_, _ = s.Open(sessionledger.Record{SessionID: sid, Harness: "claude", StartedAt: t0.Add(2 * time.Hour)})
		})
		if _, err := Fold(wipnoteDir, runDir); err != nil {
			t.Error(err)
		}
	}
	t.Cleanup(func() { foldBeforeLock = nil })
	if _, err := Fold(wipnoteDir, runDir); err != nil {
		t.Fatal(err)
	}
	row, _, _ := sessionledger.NewStore(wipnoteDir).Get(sid)
	if runs := row.RunList(); !raced || len(runs) != 2 || !row.IsOpen() {
		t.Errorf("host row runs %+v ended %v: want both runs once, the resume still open", runs, row.EndedAt)
	}
}

// TestFoldSession_ShortSnapshotFoldsNothing: a snapshot with fewer runs
// than folded.json records is nothing new: no panic, no write.
func TestFoldSession_ShortSnapshotFoldsNothing(t *testing.T) {
	wipnoteDir, _ := foldProject(t)
	s := containerwrite.Session{SessionID: "f1f1f1f1-1111-2222-3333-444444444444", StartedAt: time.Now().UTC()}
	fs := foldedSession{Runs: 2}
	var f Folded
	if err := foldSession(wipnoteDir, s, &fs, &f); err != nil || f != (Folded{}) || fs.Runs != 2 {
		t.Errorf("short snapshot: %v, %+v, %+v; want nothing folded", err, f, fs)
	}
	if _, err := os.Stat(sessionledger.NewStore(wipnoteDir).Path()); !os.IsNotExist(err) {
		t.Errorf("a short snapshot wrote the ledger (err %v)", err)
	}
}
