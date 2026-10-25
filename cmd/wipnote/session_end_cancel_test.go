package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/fsprobe"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/core/sessionledger"
)

const cancelSession = "aaaabbbb-cccc-dddd-eeee-ffff00004444"

// TestHelperSessionEndHook is not a test: it is the child process the cancel
// test kills. It runs the real `wipnote hook session-end` command on a slow
// filesystem (HELPER_FS_DELAY_MS added to every probed operation).
func TestHelperSessionEndHook(t *testing.T) {
	if os.Getenv("HELPER_SESSION_END") != "1" {
		t.Skip("helper process only")
	}
	if ms := os.Getenv("HELPER_FS_DELAY_MS"); ms != "" {
		var d int
		_, _ = fmt.Sscanf(ms, "%d", &d)
		fsprobe.Set(func(fsprobe.Op, string) { time.Sleep(time.Duration(d) * time.Millisecond) })
	}
	// TestMain points WIPNOTE_PROJECT_DIR at a sandbox; the hook must resolve the
	// project from the payload's cwd, as Claude Code's hook does.
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR", "WIPNOTE_DB_PATH"} {
		os.Unsetenv(k)
	}
	root := buildRoot()
	root.SetArgs([]string{"hook", "session-end"})
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}

type cancelProject struct {
	dir, wipnoteDir string
}

func newCancelProject(t *testing.T) *cancelProject {
	t.Helper()
	p := &cancelProject{dir: t.TempDir()}
	p.wipnoteDir = filepath.Join(p.dir, ".wipnote")
	if err := os.MkdirAll(p.wipnoteDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := sessionledger.NewStore(p.wipnoteDir).Open(sessionledger.Record{
		SessionID: cancelSession, Harness: "claude_code", ProjectDir: ".",
		StartedAt: time.Now().UTC().Add(-time.Hour),
	}); err != nil {
		t.Fatal(err)
	}
	store := claimledger.NewStore(p.wipnoteDir)
	for i, root := range []string{cancelSession, "bbbb0000-cccc-dddd-eeee-ffff00005555", "bbbb0001-cccc-dddd-eeee-ffff00005555"} {
		for j := 0; j < 2; j++ {
			if _, _, err := store.Open(root, claimledger.Episode{
				WorkItemID: fmt.Sprintf("feat-%04d%04d", i, j), SessionID: root, RootSessionID: root,
				AgentID: "claude-code", StartedAt: time.Now().UTC().Add(-time.Hour),
			}); err != nil {
				t.Fatal(err)
			}
		}
	}
	return p
}

// runKilled starts the hook in a child, sends sig after offset, and waits.
func (p *cancelProject) runKilled(t *testing.T, sig syscall.Signal, offset time.Duration) {
	t.Helper()
	home := t.TempDir()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSessionEndHook$")
	cmd.Env = []string{
		"HELPER_SESSION_END=1", "HELPER_FS_DELAY_MS=60",
		"HOME=" + home, "XDG_CACHE_HOME=" + filepath.Join(home, ".cache"),
		"XDG_DATA_HOME=" + filepath.Join(home, ".local", "share"),
		"PATH=" + os.Getenv("PATH"), "TMPDIR=" + os.TempDir(),
	}
	payload, _ := json.Marshal(map[string]string{
		"session_id": cancelSession, "cwd": p.dir, "hook_event_name": "SessionEnd", "reason": "prompt_input_exit",
	})
	cmd.Dir = p.dir
	cmd.Stdin = bytes.NewReader(payload)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	defer func() { t.Logf("child output: %.400s", out.String()) }()
	select {
	case <-done:
	case <-time.After(offset):
		_ = cmd.Process.Signal(sig)
		<-done
	}
}

// readNoteLines returns the parsed note lines; it fails on any line that is not
// a complete, valid note (the file is absent or fully valid, never partial).
func (p *cancelProject) readNoteLines(t *testing.T) (notes []hooks.SessionEndNote) {
	t.Helper()
	f, err := os.Open(filepath.Join(p.wipnoteDir, "sessions", cancelSession, hooks.SessionEndNoteFile))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var n hooks.SessionEndNote
		if strings.TrimSpace(sc.Text()) == "" {
			continue
		}
		if json.Unmarshal(sc.Bytes(), &n) != nil || n.SessionID != cancelSession || n.EndTime().IsZero() {
			t.Fatalf("partial or invalid end note line: %q", sc.Text())
		}
		notes = append(notes, n)
	}
	return notes
}

// assertCanonicalFilesParse checks every claim shard and the ledger parse
// cleanly, and that the session's own episodes are all-or-nothing.
func (p *cancelProject) assertCanonicalFilesParse(t *testing.T) (open int) {
	t.Helper()
	store := claimledger.NewStore(p.wipnoteDir)
	shards, _ := store.Shards()
	for _, sh := range shards {
		if _, err := claimledger.ReadFile(sh); err != nil {
			t.Fatalf("claim shard %s does not parse: %v", filepath.Base(sh), err)
		}
	}
	if _, err := sessionledger.ReadFile(sessionledger.NewStore(p.wipnoteDir).Path()); err != nil {
		t.Fatalf("sessions ledger does not parse: %v", err)
	}
	eps, err := store.ReadShard(cancelSession)
	if err != nil || len(eps) != 2 {
		t.Fatalf("own shard: %d episodes err=%v, want 2", len(eps), err)
	}
	for _, e := range eps {
		if e.IsOpen() {
			open++
		}
	}
	if open == 1 {
		t.Fatal("own shard is half closed: the close must be one atomic rewrite")
	}
	return open
}

func TestSessionEndHook_CancelAtAnyPointLeavesRecoverableState(t *testing.T) {
	if testing.Short() {
		t.Skip("spawns 22 child processes")
	}
	offsets := []time.Duration{0, 10, 30, 60, 120, 250, 350, 500, 700, 900, 1400}
	for _, sig := range []syscall.Signal{syscall.SIGTERM, syscall.SIGKILL} {
		for _, off := range offsets {
			off := off * time.Millisecond
			t.Run(fmt.Sprintf("%v_at_%dms", sig, off.Milliseconds()), func(t *testing.T) {
				p := newCancelProject(t)
				p.runKilled(t, sig, off)
				p.checkAfterCancel(t)
			})
		}
	}
}

func (p *cancelProject) checkAfterCancel(t *testing.T) {
	t.Helper()
	notes := p.readNoteLines(t)
	open := p.assertCanonicalFilesParse(t)
	ledgerOpen := mustLedgerRow(t, p.wipnoteDir).IsOpen()
	t.Logf("state at cancel: note=%v own episodes open=%d ledger open=%v", len(notes) > 0, open, ledgerOpen)
	if len(notes) == 0 && (open != 2 || !ledgerOpen) {
		// No note: the hook was cut off before it wrote anything, so nothing
		// may have been applied either.
		t.Fatalf("no note but state changed: open episodes=%d ledger open=%v", open, ledgerOpen)
	}

	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "p.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	hooks.CatchUpEndedSessions(p.dir, "", false)

	if len(notes) > 0 {
		end := notes[len(notes)-1].EndTime()
		row := mustLedgerRow(t, p.wipnoteDir)
		if row.IsOpen() || row.EndedAt.Sub(end).Abs() > time.Second {
			t.Errorf("catch-up left ledger open=%v ended=%v, want closed at the note's %v", row.IsOpen(), row.EndedAt, end)
		}
		eps, _ := claimledger.NewStore(p.wipnoteDir).ReadShard(cancelSession)
		for _, e := range eps {
			if e.IsOpen() || e.Outcome != claimledger.OutcomeAbandoned || e.EndedAt.Sub(end).Abs() > time.Second {
				t.Errorf("episode %s open=%v outcome=%q ended=%v; want abandoned at %v", e.WorkItemID, e.IsOpen(), e.Outcome, e.EndedAt, end)
			}
		}
	}
	p.assertStrayTempsGoOnNextWrite(t)
}

// A killed rewrite can leave a ".claims-*.tmp". Once it is old enough, the next
// write of any shard removes it.
func (p *cancelProject) assertStrayTempsGoOnNextWrite(t *testing.T) {
	t.Helper()
	store := claimledger.NewStore(p.wipnoteDir)
	strays, _ := filepath.Glob(filepath.Join(store.Dir(), ".claims-*.tmp"))
	old := time.Now().Add(-10 * time.Minute)
	for _, s := range strays {
		_ = os.Chtimes(s, old, old)
	}
	other := "bbbb0000-cccc-dddd-eeee-ffff00005555"
	if _, err := store.CloseAllForSession(other, other, claimledger.OutcomeReleased, time.Now()); err != nil {
		t.Fatal(err)
	}
	if left, _ := filepath.Glob(filepath.Join(store.Dir(), ".claims-*.tmp")); len(left) != 0 {
		t.Errorf("stray temp files survived the next write: %v", left)
	}
}

func mustLedgerRow(t *testing.T, wipnoteDir string) sessionledger.Record {
	t.Helper()
	rec, found, err := sessionledger.NewStore(wipnoteDir).Get(cancelSession)
	if err != nil || !found {
		t.Fatalf("ledger row found=%v err=%v", found, err)
	}
	return rec
}

// With no cancellation the child finishes inside the window and everything is
// closed without a catch-up pass.
func TestSessionEndHook_UncancelledRunClosesInWindow(t *testing.T) {
	p := newCancelProject(t)
	start := time.Now()
	p.runKilled(t, syscall.SIGKILL, 10*time.Second)
	elapsed := time.Since(start)
	t.Logf("hook process ran %s", elapsed)
	if len(p.readNoteLines(t)) != 1 {
		t.Fatal("expected exactly one end note")
	}
	if p.assertCanonicalFilesParse(t) != 0 {
		t.Error("episodes still open after an uncancelled hook")
	}
	if mustLedgerRow(t, p.wipnoteDir).IsOpen() {
		t.Error("ledger still open after an uncancelled hook")
	}
}
