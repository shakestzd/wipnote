package scratch

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// Done-when 13: no scratchpad_dir, a missing folder, an empty folder and a
// missing store are no-ops that leave nothing behind.
func TestCapture_NothingToCaptureIsANoOp(t *testing.T) {
	e := newEnv(t)
	cases := map[string]Request{
		"no scratchpad_dir": {StoreDir: e.store, SessionID: testSession},
		"missing folder":    {StoreDir: e.store, SessionID: testSession, ScratchpadDir: filepath.Join(e.pad, "nope")},
		"relative folder":   {StoreDir: e.store, SessionID: testSession, ScratchpadDir: "scratchpad"},
		"empty folder":      {StoreDir: e.store, SessionID: testSession, ScratchpadDir: e.pad},
		"missing store":     {StoreDir: filepath.Join(e.store, "nope"), SessionID: testSession, ScratchpadDir: e.pad},
	}
	for name, req := range cases {
		res, err := Capture(req)
		if err != nil || res.Wrote || res.Saved != 0 {
			t.Errorf("%s: result = %+v, %v, want a no-op", name, res, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(e.store, "scratch")); err == nil {
		t.Error("a no-op pass left a scratch folder in the store")
	}
	if _, err := os.Lstat(filepath.Join(e.store, "nope")); err == nil {
		t.Error("Capture created a missing store")
	}
}

// Done-when 13: an unreadable file does not fail the pass, and the readable
// draft beside it is still saved.
func TestCapture_UnreadableFileDoesNotFailThePass(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every file")
	}
	e := newEnv(t)
	locked := e.write("locked.md", "cannot read\n")
	e.write("locked-dir/inner.md", "inner\n")
	e.write("open.md", "can read\n")
	lockedDir := filepath.Join(e.pad, "locked-dir")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(lockedDir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(lockedDir, 0o755) })

	m, _ := e.capture("")

	if !row(t, m, "open.md").Saved {
		t.Error("the readable draft was not saved")
	}
	if hasRow(m, "locked.md") || e.hasCopy("locked.md") {
		t.Errorf("rows = %+v, want no row and no copy for the unreadable file", m.Files)
	}
}

// A folder that becomes unreadable does not mark its saved drafts gone.
func TestCapture_UnreadableFolderKeepsRowsPresent(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	e := newEnv(t)
	e.write("sub/draft.md", "x\n")
	e.capture("")
	sub := filepath.Join(e.pad, "sub")
	if err := os.Chmod(sub, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(sub, 0o755) })

	m, _ := e.capture("")

	if r := row(t, m, "sub/draft.md"); r.State != StatePresent {
		t.Errorf("row = %+v, want it still present", r)
	}
}

// A session id that is not a safe folder name stops the pass.
func TestCapture_UnsafeSessionIDStopsThePass(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "x\n")
	for _, id := range []string{"", ".", "..", "../up", "a/b", `a\b`, ".hidden", "a b", "x\x00y", string(make([]byte, 200))} {
		_, err := Capture(Request{StoreDir: e.store, SessionID: id, ScratchpadDir: e.pad})
		if !errors.Is(err, ErrUnsafeSessionID) {
			t.Errorf("session id %q: err = %v, want ErrUnsafeSessionID", id, err)
		}
	}
	if _, err := os.Lstat(filepath.Join(e.store, "scratch")); err == nil {
		t.Error("an unsafe session id left a scratch folder in the store")
	}
	for _, id := range []string{"sess-0001", "019ee378-abcd-7000-8000-0000000000bb", "test_sess.1"} {
		if !SafeSessionID(id) {
			t.Errorf("SafeSessionID(%q) = false, want true", id)
		}
	}
}

// Done-when 14: the pass stops at the file count, records that, marks no
// unvisited row gone, and a later full pass finishes the work.
func TestCapture_StopsAtFileCount(t *testing.T) {
	e := newEnv(t)
	e.write("a.md", "a\n")
	e.write("b.md", "b\n")
	e.capture("")
	e.write("c.md", "c\n")
	e.write("d.md", "d\n")
	e.lim.maxFiles = 2

	m, res := e.capture("")

	if res.StoppedEarly != StopFileCount || m.StoppedEarly != StopFileCount {
		t.Errorf("stopped_early = %q / %q, want %q", res.StoppedEarly, m.StoppedEarly, StopFileCount)
	}
	e.wantSkipped(m, "c.md", KindDocument, SkipBudget)
	if hasRow(m, "d.md") {
		t.Error("the pass listed a file past the one it stopped at")
	}
	for _, p := range []string{"a.md", "b.md"} {
		if r := row(t, m, p); r.State != StatePresent || !r.Saved {
			t.Errorf("row %s = %+v, want it untouched", p, r)
		}
	}

	e.lim.maxFiles = MaxWalkFiles
	m, _ = e.capture("")
	if m.StoppedEarly != "" || !row(t, m, "c.md").Saved || !row(t, m, "d.md").Saved {
		t.Errorf("manifest = %+v, want a full pass that saves c.md and d.md", m)
	}
}

// Done-when 14: the pass stops when the time budget runs out and records it.
func TestCapture_StopsAtTimeBudget(t *testing.T) {
	e := newEnv(t)
	for i := 0; i < 5; i++ {
		e.write(fmt.Sprintf("f%d.md", i), "x\n")
	}
	clock := time.Unix(1_700_000_000, 0)
	e.lim.now = func() time.Time { // each look at the clock costs one second
		clock = clock.Add(time.Second)
		return clock
	}

	m, res := e.capture("")

	if res.StoppedEarly != StopTimeBudget || m.StoppedEarly != StopTimeBudget {
		t.Fatalf("stopped_early = %q / %q, want %q", res.StoppedEarly, m.StoppedEarly, StopTimeBudget)
	}
	if res.Saved == 0 || res.Saved >= 5 {
		t.Errorf("saved %d of 5, want some and not all", res.Saved)
	}
}

// The fixed limits are the owner's decisions.
func TestLimits_AreTheOwnerDecisions(t *testing.T) {
	lim := defaultLimits()
	if lim.maxFile != 256*1024 || lim.maxSession != 5*1024*1024 || lim.maxFiles != 2000 || lim.budget != 2*time.Second {
		t.Errorf("limits = %+v", lim)
	}
}

// Done-when 18: capture and a concurrent manifest writer never drop each
// other's rows.
func TestCapture_ConcurrentWriterKeepsRows(t *testing.T) {
	e := newEnv(t)
	const n = 25
	var wg sync.WaitGroup
	errs := make(chan error, 2*n)
	for i := 0; i < n; i++ {
		e.write(fmt.Sprintf("draft-%02d.md", i), fmt.Sprintf("draft %d\n", i))
		wg.Add(2)
		go func() {
			defer wg.Done()
			_, err := Capture(Request{StoreDir: e.store, SessionID: testSession, ScratchpadDir: e.pad})
			errs <- err
		}()
		go func(i int) {
			defer wg.Done()
			errs <- Update(e.store, testSession, func(m *Manifest) error {
				m.Files = append(m.Files, Row{Path: fmt.Sprintf("kept-%02d.md", i), Kind: KindDocument, State: StateGone, Pinned: true})
				return nil
			})
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}

	m, _ := e.capture("")
	for i := 0; i < n; i++ {
		if r := row(t, m, fmt.Sprintf("kept-%02d.md", i)); !r.Pinned || r.State != StateGone {
			t.Errorf("writer row %d = %+v, want it kept as written", i, r)
		}
		if r := row(t, m, fmt.Sprintf("draft-%02d.md", i)); !r.Saved {
			t.Errorf("capture row %d = %+v, want it saved", i, r)
		}
	}
}

// A manifest that does not parse stops the pass and is left as it was.
func TestCapture_CorruptManifestIsNotOverwritten(t *testing.T) {
	e := newEnv(t)
	e.write("draft.md", "x\n")
	if err := os.MkdirAll(e.sessionDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ManifestPath(e.sessionDir()), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Capture(Request{StoreDir: e.store, SessionID: testSession, ScratchpadDir: e.pad}); err == nil {
		t.Error("want an error for a corrupt manifest")
	}
	if got, _ := os.ReadFile(ManifestPath(e.sessionDir())); string(got) != "{not json" {
		t.Errorf("corrupt manifest was rewritten: %q", got)
	}
}
