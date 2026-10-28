package containerwrite

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

const testRun = "dlg-0123456789ab"

func worker(t *testing.T) (wipnoteDir, runDir string) {
	t.Helper()
	wipnoteDir = filepath.Join(t.TempDir(), ".wipnote")
	runDir = filepath.Join(wipnoteDir, "delegate", "runs", testRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(PlaceEnv, PlaceContainer)
	t.Setenv(RunEnv, testRun)
	return wipnoteDir, runDir
}

// TestActive_OnlyAContainerWorker: the place env and a valid run id make a
// container worker; either alone, or a bad run id, does not.
func TestActive_OnlyAContainerWorker(t *testing.T) {
	cases := []struct {
		place, run string
		want       bool
	}{
		{PlaceContainer, testRun, true},
		{"", testRun, false},
		{"host", testRun, false},
		{PlaceContainer, "", false},
		{PlaceContainer, "not-a-run", false},
	}
	for _, c := range cases {
		t.Setenv(PlaceEnv, c.place)
		t.Setenv(RunEnv, c.run)
		if got := Active(); got != c.want {
			t.Errorf("place %q run %q: Active %v, want %v", c.place, c.run, got, c.want)
		}
	}
}

// TestRunDir_RefusesWhatIsNotTheWorkersOwnFolder: a missing folder or a
// symlinked one is not the worker's.
func TestRunDir_RefusesWhatIsNotTheWorkersOwnFolder(t *testing.T) {
	wipnoteDir, runDir := worker(t)
	if got, err := RunDir(wipnoteDir); err != nil || got != runDir {
		t.Fatalf("RunDir = %q, %v; want %q", got, err, runDir)
	}
	t.Setenv(RunEnv, "dlg-bbbbbbbbbbbb")
	if _, err := RunDir(wipnoteDir); err == nil {
		t.Error("a missing run folder was accepted")
	}
	link := filepath.Join(wipnoteDir, "delegate", "runs", "dlg-aaaaaaaaaaaa")
	if err := os.Symlink(t.TempDir(), link); err != nil {
		t.Fatal(err)
	}
	t.Setenv(RunEnv, "dlg-aaaaaaaaaaaa")
	if _, err := RunDir(wipnoteDir); err == nil {
		t.Error("a symlinked run folder was accepted")
	}
}

// TestSession_OwnFileByRename: the worker's session row is its own file,
// written by rename; a second open keeps the first start, a close stamps
// the end once; and the other side, nothing else lands in the folder.
func TestSession_OwnFileByRename(t *testing.T) {
	wipnoteDir, runDir := worker(t)
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if err := OpenSession(wipnoteDir, Session{SessionID: "sid-1", Harness: "claude", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := OpenSession(wipnoteDir, Session{SessionID: "sid-1", Harness: "claude", StartedAt: t0.Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	if err := CloseSession(wipnoteDir, "sid-1", t0.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := CloseSession(wipnoteDir, "sid-1", t0.Add(3*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, err := ReadSessions(runDir)
	if err != nil || len(got) != 1 {
		t.Fatalf("ReadSessions = %+v, %v", got, err)
	}
	s := got[0]
	if !s.StartedAt.Equal(t0) || !s.EndedAt.Equal(t0.Add(2*time.Hour)) || s.Harness != "claude" {
		t.Errorf("session %+v: want the first start and the first end", s)
	}
	entries, _ := os.ReadDir(runDir)
	if len(entries) != 1 || entries[0].Name() != "session-sid-1.json" {
		t.Errorf("run folder holds %v, want only session-sid-1.json (no temp file left)", names(entries))
	}
}

// TestSession_RefusesOutsideAContainerWorker: on the host the own-file
// writers refuse, so a host session can never be routed away from the
// shared ledger by mistake.
func TestSession_RefusesOutsideAContainerWorker(t *testing.T) {
	wipnoteDir, _ := worker(t)
	t.Setenv(PlaceEnv, "")
	if err := OpenSession(wipnoteDir, Session{SessionID: "sid-1", StartedAt: time.Now()}); err == nil {
		t.Error("OpenSession wrote outside a container worker")
	}
	if err := WriteJSON(wipnoteDir, "gate-x.json", map[string]string{}); err == nil {
		t.Error("WriteJSON wrote outside a container worker")
	}
}

// TestAppendLine_ByRenameKeepsEveryLine: concurrent appends from one side
// (the container) keep every line, and no temp file is left.
func TestAppendLine_ByRenameKeepsEveryLine(t *testing.T) {
	_, runDir := worker(t)
	path := filepath.Join(runDir, "events-sid-1.ndjson")
	var wg sync.WaitGroup
	for i := 0; i < 40; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := AppendLine(path, []byte("{\"x\":1}\n")); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	data, _ := os.ReadFile(path)
	if n := strings.Count(string(data), "\n"); n != 40 {
		t.Errorf("%d lines, want 40", n)
	}
	entries, _ := os.ReadDir(runDir)
	if len(entries) != 1 {
		t.Errorf("run folder holds %v, want only the events file", names(entries))
	}
}

// TestFamily_RecordedOnTheOwnFile: the family index entry travels on the
// session's own file.
func TestFamily_RecordedOnTheOwnFile(t *testing.T) {
	wipnoteDir, runDir := worker(t)
	if err := SetFamily(wipnoteDir, "sid-1", "fam-1"); err != nil {
		t.Fatal(err)
	}
	if err := OpenSession(wipnoteDir, Session{SessionID: "sid-1", Harness: "codex", StartedAt: time.Now()}); err != nil {
		t.Fatal(err)
	}
	got, _ := ReadSessions(runDir)
	if len(got) != 1 || got[0].Family != "fam-1" || got[0].Harness != "codex" {
		t.Errorf("sessions %+v, want family fam-1 kept across the open", got)
	}
}

func names(entries []os.DirEntry) []string {
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

// TestSession_ResumeAddsRun: a start after the session ended is a resume
// (a second run with its own end); a start while a run is open is a replay
// and adds nothing.
func TestSession_ResumeAddsRun(t *testing.T) {
	wipnoteDir, runDir := worker(t)
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(OpenSession(wipnoteDir, Session{SessionID: "sid-1", Harness: "claude", StartedAt: t0}))
	must(OpenSession(wipnoteDir, Session{SessionID: "sid-1", StartedAt: t0.Add(time.Minute)}))
	must(CloseSession(wipnoteDir, "sid-1", t0.Add(time.Hour)))
	must(OpenSession(wipnoteDir, Session{SessionID: "sid-1", StartedAt: t0.Add(2 * time.Hour)}))
	must(CloseSession(wipnoteDir, "sid-1", t0.Add(3*time.Hour)))
	must(CloseSession(wipnoteDir, "sid-1", t0.Add(4*time.Hour)))
	got, _ := ReadSessions(runDir)
	if len(got) != 1 {
		t.Fatalf("sessions %+v", got)
	}
	runs := got[0].RunList()
	want := []Run{{StartedAt: t0, EndedAt: t0.Add(time.Hour)}, {StartedAt: t0.Add(2 * time.Hour), EndedAt: t0.Add(3 * time.Hour)}}
	if !slices.Equal(runs, want) || !got[0].StartedAt.Equal(t0) {
		t.Errorf("runs %+v\nwant %+v (first start kept)", runs, want)
	}
}
