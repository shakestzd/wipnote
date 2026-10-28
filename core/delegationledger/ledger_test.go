package delegationledger

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

func testStore(t *testing.T) *Store {
	t.Helper()
	return NewStore(filepath.Join(t.TempDir(), ".wipnote"))
}

func intp(v int) *int { return &v }

var t0 = time.Date(2026, 10, 25, 9, 0, 0, 0, time.UTC)

func interactiveRow(id, item string, started time.Time) Record {
	return Record{
		ID: id, Mode: ModeInteractive, Harness: "codex", Name: "w-" + id[4:8],
		WorkItem: item, Link: "sid", CallerSession: "caller-1", Host: "host",
		Started: started,
	}
}

func mustOpen(t *testing.T, s *Store, r Record) Record {
	t.Helper()
	got, err := s.Open(r)
	if err != nil {
		t.Fatalf("Open(%s): %v", r.ID, err)
	}
	return got
}

// TestLedger_OpenCloseEnrichRoundTrip pins that every column survives the HTML
// for both an open and a closed row, including the distinction between exit 0
// and no exit, which a lossy codec would collapse.
func TestLedger_OpenCloseEnrichRoundTrip(t *testing.T) {
	s := testStore(t)
	full := Record{
		ID: "dlg-0123456789ab", Mode: ModeInteractive, Harness: "codex", Name: "parser",
		Label: "review <the> \"parser\" & tests", WorkItem: "feat-dd8b0cde", Link: "tag",
		CallerSession: "caller-1", WorkerSessions: []string{"sess-b"}, Host: "container",
		Started: t0, ResumedFrom: "dlg-ba9876543210",
	}
	oneshot := Record{
		ID: "dlg-aaaaaaaaaaaa", Mode: ModeOneshot, Harness: "claude-print",
		Link: "sid", Host: "host", Started: t0.Add(time.Minute),
	}
	mustOpen(t, s, full)
	mustOpen(t, s, oneshot)

	end := t0.Add(time.Hour)
	if err := s.Close(full.ID, CloseInfo{Ended: end, Exit: intp(0), Reason: EndStop}); err != nil {
		t.Fatalf("Close: %v", err)
	}
	enr := Enrichment{WorkerSessions: []string{"sess-a"}, UsageInput: intp(1200), UsageCached: intp(300), UsageOutput: intp(45)}
	if _, err := s.Enrich(full.ID, enr); err != nil {
		t.Fatalf("Enrich: %v", err)
	}

	want := full
	want.Ended, want.Exit, want.EndReason = end, intp(0), EndStop
	want.WorkerSessions = []string{"sess-b", "sess-a"}
	want.UsageInput, want.UsageCached, want.UsageOutput = 1200, 300, 45

	got, err := NewStore(filepath.Dir(s.Path())).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d rows, want 2: %+v", len(got), got)
	}
	if !reflect.DeepEqual(got[0], want) {
		t.Errorf("closed row round trip:\n got %+v\nwant %+v", got[0], want)
	}
	if got[1].Exit != nil || !got[1].Ended.IsZero() || got[1].EndReason != "" {
		t.Errorf("open row read back closed: exit=%v ended=%v reason=%q", got[1].Exit, got[1].Ended, got[1].EndReason)
	}
	if !reflect.DeepEqual(got[1], oneshot) {
		t.Errorf("open row round trip:\n got %+v\nwant %+v", got[1], oneshot)
	}
}

// TestLedger_CloseKeepsFirstEnd pins that a second Close (a late reaper) never
// moves a recorded end, and that closing an unknown run reports ErrNoRow.
func TestLedger_CloseKeepsFirstEnd(t *testing.T) {
	s := testStore(t)
	r := mustOpen(t, s, interactiveRow("dlg-111111111111", "feat-a", t0))
	first := CloseInfo{Ended: t0.Add(time.Minute), Exit: intp(3), Reason: EndExit}
	if err := s.Close(r.ID, first); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if err := s.Close(r.ID, CloseInfo{Ended: t0.Add(time.Hour), Reason: EndGone}); err != nil {
		t.Fatalf("second Close: %v", err)
	}
	got, _, _ := s.Get(r.ID)
	if !got.Ended.Equal(first.Ended) || got.EndReason != EndExit || got.Exit == nil || *got.Exit != 3 {
		t.Errorf("second Close moved the end: %+v", got)
	}
	if err := s.Close("dlg-999999999999", first); err != ErrNoRow {
		t.Errorf("Close unknown = %v, want ErrNoRow", err)
	}
	if err := s.Close(r.ID, CloseInfo{Ended: t0}); err == nil {
		t.Errorf("Close without a reason was accepted")
	}
}

// CloseIfOpen reports whether this call closed the row: true for the first
// close of an open row, false (and no change) for a closed one.
func TestLedger_CloseIfOpenReports(t *testing.T) {
	s := testStore(t)
	r := mustOpen(t, s, interactiveRow("dlg-222222222222", "feat-a", t0))
	closed, err := s.CloseIfOpen(r.ID, CloseInfo{Ended: t0.Add(time.Minute), Reason: EndStop})
	if err != nil || !closed {
		t.Fatalf("first CloseIfOpen = %v, %v; want true", closed, err)
	}
	closed, err = s.CloseIfOpen(r.ID, CloseInfo{Ended: t0.Add(time.Hour), Exit: intp(0), Reason: EndExit})
	if err != nil || closed {
		t.Fatalf("second CloseIfOpen = %v, %v; want false", closed, err)
	}
	if got, _, _ := s.Get(r.ID); got.EndReason != EndStop || got.Exit != nil {
		t.Errorf("second CloseIfOpen changed the row: %+v", got)
	}
	if _, err := s.CloseIfOpen("dlg-999999999999", CloseInfo{Reason: EndExit}); err != ErrNoRow {
		t.Errorf("CloseIfOpen unknown = %v, want ErrNoRow", err)
	}
}

// TestLedger_ConcurrentOpen: 8 goroutines in each of 2 processes append at
// once; every row must survive the shared filelock.Guard.
func TestLedger_ConcurrentOpen(t *testing.T) {
	const goroutines, perG = 8, 5
	if dir := os.Getenv("WIPNOTE_TEST_DLG_DIR"); dir != "" {
		if err := openMany(NewStore(dir), os.Getenv("WIPNOTE_TEST_DLG_PROC"), goroutines, perG); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		os.Exit(0)
	}
	if !filelock.CrossProcess {
		t.Skip("no cross-process lock on this platform (bug-68f3593b)")
	}
	dir := filepath.Join(t.TempDir(), ".wipnote")
	runWorkerProcesses(t, dir, 2)
	recs, err := NewStore(dir).ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if want := 2 * goroutines * perG; len(recs) != want {
		t.Fatalf("got %d rows after concurrent Open, want %d", len(recs), want)
	}
	seen := map[string]bool{}
	for _, r := range recs {
		seen[r.Label] = true
	}
	if len(seen) != 2*goroutines*perG {
		t.Errorf("distinct labels = %d, want %d", len(seen), 2*goroutines*perG)
	}
}

// runWorkerProcesses re-execs this test binary n times at once, each in
// worker mode against dir.
func runWorkerProcesses(t *testing.T, dir string, n int) {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable: %v", err)
	}
	var wg sync.WaitGroup
	errs := make([]error, n)
	for p := 0; p < n; p++ {
		wg.Add(1)
		go func(p int) {
			defer wg.Done()
			cmd := exec.Command(exe, "-test.run", "^TestLedger_ConcurrentOpen$")
			cmd.Env = append(os.Environ(), "WIPNOTE_TEST_DLG_DIR="+dir, "WIPNOTE_TEST_DLG_PROC="+strconv.Itoa(p))
			if out, err := cmd.CombinedOutput(); err != nil {
				errs[p] = fmt.Errorf("process %d: %v\n%s", p, err, out)
			}
		}(p)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
}

func openMany(s *Store, proc string, goroutines, perG int) error {
	var wg sync.WaitGroup
	errs := make(chan error, goroutines*perG)
	for g := 0; g < goroutines; g++ {
		wg.Add(1)
		go func(g int) {
			defer wg.Done()
			for i := 0; i < perG; i++ {
				r := Record{Mode: ModeOneshot, Harness: "codex", Host: "host",
					Label: fmt.Sprintf("p%s-g%d-i%d", proc, g, i)}
				if _, err := s.Open(r); err != nil {
					errs <- err
				}
			}
		}(g)
	}
	wg.Wait()
	close(errs)
	return <-errs
}

// TestLedger_RefusesHostPath: every string column is checked at write time; a
// refused write leaves no ledger behind, and a portable value is accepted.
func TestLedger_RefusesHostPath(t *testing.T) {
	t.Setenv("HOME", "/opt/fakehome")
	s := testStore(t)
	cases := map[string]func(*Record){
		"label":           func(r *Record) { r.Label = "fix /home/alice/proj/x.go" },
		"name":            func(r *Record) { r.Name = "/Users/bob" },
		"caller_session":  func(r *Record) { r.CallerSession = "/opt/fakehome" },
		"worker_sessions": func(r *Record) { r.WorkerSessions = []string{"ok", "/workspaces/c/x"} },
	}
	for field, mutate := range cases {
		r := interactiveRow("dlg-222222222222", "feat-a", t0)
		mutate(&r)
		if _, err := s.Open(r); err == nil {
			t.Errorf("Open with a host path in %s was accepted", field)
		}
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Fatalf("a refused Open created the ledger: %v", err)
	}
	r := interactiveRow("dlg-222222222222", "feat-a", t0)
	r.Label = "fix docs/design/orch.md"
	mustOpen(t, s, r)
	if _, err := s.Enrich(r.ID, Enrichment{WorkerSessions: []string{"/home/alice/s"}}); err == nil {
		t.Errorf("Enrich with a host-path session id was accepted")
	}
	got, _, _ := s.Get(r.ID)
	if len(got.WorkerSessions) != 0 {
		t.Errorf("refused Enrich still wrote: %+v", got.WorkerSessions)
	}
}

// TestLedger_TornTailRepair: a crash mid-append leaves a fragment; the next
// Open drops only the fragment and keeps every complete row.
func TestLedger_TornTailRepair(t *testing.T) {
	s := testStore(t)
	mustOpen(t, s, interactiveRow("dlg-333333333333", "feat-a", t0))
	data, err := os.ReadFile(s.Path())
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	torn := strings.TrimSuffix(string(data), ledgerFooter) + `<tr id="dlg-44444444` // no newline, no footer
	if err := os.WriteFile(s.Path(), []byte(torn), 0o644); err != nil {
		t.Fatalf("write torn: %v", err)
	}
	mustOpen(t, s, interactiveRow("dlg-555555555555", "feat-a", t0.Add(time.Minute)))
	recs, err := s.ReadAll()
	if err != nil {
		t.Fatalf("ReadAll: %v", err)
	}
	if len(recs) != 2 || recs[0].ID != "dlg-333333333333" || recs[1].ID != "dlg-555555555555" {
		t.Fatalf("rows after repair = %+v, want the survivor and the new row", recs)
	}
}

// TestLedger_OpenInteractiveRowsForItem: only OPEN INTERACTIVE rows for the
// item count; an unreadable ledger reports ok=false so the claim gate fails
// open, while a missing ledger is readable and empty.
func TestLedger_OpenInteractiveRowsForItem(t *testing.T) {
	s := testStore(t)
	mustOpen(t, s, interactiveRow("dlg-a00000000001", "feat-x", t0))
	mustOpen(t, s, interactiveRow("dlg-a00000000002", "feat-x", t0.Add(time.Second)))
	closed := mustOpen(t, s, interactiveRow("dlg-a00000000003", "feat-x", t0))
	if err := s.Close(closed.ID, CloseInfo{Ended: t0.Add(time.Hour), Reason: EndStop}); err != nil {
		t.Fatal(err)
	}
	mustOpen(t, s, Record{ID: "dlg-a00000000004", Mode: ModeOneshot, Harness: "codex", WorkItem: "feat-x", Started: t0})
	mustOpen(t, s, interactiveRow("dlg-a00000000005", "feat-y", t0))

	runs, ok := s.ReadOpen()
	if !ok {
		t.Fatalf("ReadOpen ok=false on a readable ledger")
	}
	var ids []string
	for _, r := range runs.InteractiveForItem("feat-x") {
		ids = append(ids, r.ID)
	}
	if want := []string{"dlg-a00000000001", "dlg-a00000000002"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("InteractiveForItem(feat-x) = %v, want %v", ids, want)
	}
	if got := runs.InteractiveForItem("feat-none"); len(got) != 0 {
		t.Errorf("InteractiveForItem(feat-none) = %v, want none", got)
	}
	ids = nil
	for _, r := range runs.Interactive() { // every item; never one-shot or closed rows
		ids = append(ids, r.ID)
	}
	if want := []string{"dlg-a00000000001", "dlg-a00000000005", "dlg-a00000000002"}; !reflect.DeepEqual(ids, want) {
		t.Errorf("Interactive() = %v, want %v", ids, want)
	}

	if _, ok := testStore(t).ReadOpen(); !ok {
		t.Errorf("missing ledger reported unreadable")
	}
	bad := testStore(t)
	if err := os.MkdirAll(bad.Path(), 0o755); err != nil { // a directory cannot be parsed
		t.Fatal(err)
	}
	if _, ok := bad.ReadOpen(); ok {
		t.Errorf("unreadable ledger reported ok=true; the claim gate would not fail open")
	}
}

// TestLedger_OpenRowByRunID: the open row for a run id is found; a closed or
// unknown run is not.
func TestLedger_OpenRowByRunID(t *testing.T) {
	s := testStore(t)
	live := mustOpen(t, s, interactiveRow("dlg-b00000000001", "feat-x", t0))
	done := mustOpen(t, s, interactiveRow("dlg-b00000000002", "feat-x", t0))
	if err := s.Close(done.ID, CloseInfo{Ended: t0.Add(time.Hour), Exit: intp(0), Reason: EndExit}); err != nil {
		t.Fatal(err)
	}
	runs, ok := s.ReadOpen()
	if !ok {
		t.Fatal("ReadOpen ok=false")
	}
	if got, found := runs.ByRunID(live.ID); !found || got.ID != live.ID || got.CallerSession != "caller-1" {
		t.Errorf("ByRunID(open) = %+v, %v", got, found)
	}
	if _, found := runs.ByRunID(done.ID); found {
		t.Errorf("ByRunID returned a closed row")
	}
	if _, found := runs.ByRunID("dlg-ffffffffffff"); found {
		t.Errorf("ByRunID returned a row for an unknown run")
	}
}

// TestLedger_IDHas12Hex: minted ids carry 12 hex (48 bits; orch's 8 collided,
// P-6), and an id with fewer than 12 hex is refused.
func TestLedger_IDHas12Hex(t *testing.T) {
	shape := regexp.MustCompile(`^dlg-[0-9a-f]{12}$`)
	seen := map[string]bool{}
	for i := 0; i < 1000; i++ {
		id := NewRunID()
		if !shape.MatchString(id) {
			t.Fatalf("NewRunID = %q, want dlg-<12 hex>", id)
		}
		if seen[id] {
			t.Fatalf("NewRunID repeated %q within 1000 draws", id)
		}
		seen[id] = true
	}
	s := testStore(t)
	got := mustOpen(t, s, Record{Mode: ModeOneshot, Harness: "codex", Started: t0})
	if !shape.MatchString(got.ID) {
		t.Errorf("Open minted %q", got.ID)
	}
	short := interactiveRow("dlg-abcd1234", "feat-a", t0)
	if _, err := s.Open(short); err == nil {
		t.Errorf("Open accepted an 8-hex id")
	}
	long := interactiveRow("dlg-0123456789abcdef", "feat-a", t0)
	if _, err := s.Open(long); err != nil {
		t.Errorf("Open refused a 16-hex id: %v", err)
	}
	if _, err := s.Open(long); err == nil {
		t.Errorf("Open accepted a duplicate id")
	}
}

// TestLedger_WorkerSessionsKeepOrder: worker sessions stay in the order they
// were used (oldest first, slice 20 resumes the last), never sorted, with a
// repeat ignored.
func TestLedger_WorkerSessionsKeepOrder(t *testing.T) {
	s := testStore(t)
	r := interactiveRow("dlg-c00000000001", "feat-a", t0)
	r.WorkerSessions = []string{"zzz-first"}
	mustOpen(t, s, r)
	for _, add := range [][]string{{"mmm-second"}, {"aaa-third", "zzz-first"}} {
		if _, err := s.Enrich(r.ID, Enrichment{WorkerSessions: add}); err != nil {
			t.Fatalf("Enrich: %v", err)
		}
	}
	got, found, err := s.Get(r.ID)
	if err != nil || !found {
		t.Fatalf("Get: %v %v", found, err)
	}
	want := []string{"zzz-first", "mmm-second", "aaa-third"}
	if !reflect.DeepEqual(got.WorkerSessions, want) {
		t.Errorf("WorkerSessions = %v, want %v", got.WorkerSessions, want)
	}
	if got.LastWorkerSession() != "aaa-third" {
		t.Errorf("LastWorkerSession = %q", got.LastWorkerSession())
	}
}

// TestLedger_RefusesBadEnums: values outside the data-model enums are refused,
// and end_reason=imported is a valid close.
func TestLedger_RefusesBadEnums(t *testing.T) {
	s := testStore(t)
	for name, mutate := range map[string]func(*Record){
		"mode":       func(r *Record) { r.Mode = "batch" },
		"harness":    func(r *Record) { r.Harness = "gemini" },
		"link":       func(r *Record) { r.Link = "branch" },
		"host":       func(r *Record) { r.Host = "remote" },
		"work_item":  func(r *Record) { r.WorkItem = "trk-1234" },
		"end_reason": func(r *Record) { r.Ended, r.EndReason = t0.Add(time.Hour), "crashed" },
		"oneshot name": func(r *Record) {
			r.Mode = ModeOneshot
		},
	} {
		r := interactiveRow("dlg-d00000000001", "feat-a", t0)
		mutate(&r)
		if _, err := s.Open(r); err == nil {
			t.Errorf("Open accepted a bad %s", name)
		}
	}
	imported := interactiveRow("dlg-d00000000002", "bug-1234abcd", t0)
	imported.Ended, imported.EndReason = t0.Add(time.Hour), EndImported
	if _, err := s.Open(imported); err != nil {
		t.Errorf("Open refused an imported row: %v", err)
	}
}

// TestLedger_OnCommitGetsRepoRelativePath: writes go through the artifact
// commit path with a repo-relative path, never an absolute one.
func TestLedger_OnCommitGetsRepoRelativePath(t *testing.T) {
	var got []string
	OnCommit = func(_, rel, action string) { got = append(got, rel+"|"+action) }
	defer func() { OnCommit = nil }()
	s := testStore(t)
	r := mustOpen(t, s, interactiveRow("dlg-e00000000001", "feat-a", t0))
	if err := s.Close(r.ID, CloseInfo{Ended: t0.Add(time.Hour), Reason: EndPaneClosed}); err != nil {
		t.Fatal(err)
	}
	want := []string{".wipnote/delegations-ledger.html|delegation open", ".wipnote/delegations-ledger.html|delegation close"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("OnCommit calls = %v, want %v", got, want)
	}
}
