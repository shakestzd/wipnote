package claimledger

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A writer cancelled between CreateTemp and Rename leaves a ".claims-*.tmp"
// behind. The next write removes it once it is old enough to be certainly
// dead, and leaves a fresh one alone because it may belong to a live writer.
func TestWrite_RemovesStaleTempKeepsFreshOne(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	s := NewStore(wipnoteDir)
	if _, _, err := s.Open("root-1", Episode{WorkItemID: "feat-1", SessionID: "root-1", AgentID: "a", StartedAt: time.Now().Add(-time.Hour)}); err != nil {
		t.Fatal(err)
	}
	stale := filepath.Join(s.Dir(), ".claims-111.tmp")
	fresh := filepath.Join(s.Dir(), ".claims-222.tmp")
	other := filepath.Join(s.Dir(), "notes.tmp")
	for _, p := range []string{stale, fresh, other} {
		if err := os.WriteFile(p, []byte("partial"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	old := time.Now().Add(-10 * time.Minute)
	for _, p := range []string{stale, other} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}

	// Closing rewrites the shard through writeAllLocked.
	if n, err := s.CloseAllForSession("root-1", "root-1", OutcomeAbandoned, time.Now()); err != nil || n != 1 {
		t.Fatalf("close: n=%d err=%v", n, err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale .claims-*.tmp survived the next write")
	}
	if _, err := os.Stat(fresh); err != nil {
		t.Error("a fresh temp file (maybe a live writer's) was removed")
	}
	if _, err := os.Stat(other); err != nil {
		t.Error("a file that is not a claims temp was removed")
	}
	if eps, err := s.ReadShard("root-1"); err != nil || len(eps) != 1 || eps[0].IsOpen() {
		t.Fatalf("shard after close: %+v err=%v", eps, err)
	}
}
