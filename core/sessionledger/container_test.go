package sessionledger

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// TestOpen_ContainerWorkerWritesOwnFile: inside a container worker a start
// and an end go to the worker's own session file, never the shared ledger,
// and a repair is refused.
func TestOpen_ContainerWorkerWritesOwnFile(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	runDir := filepath.Join(wipnoteDir, "delegate", "runs", "dlg-0123456789ab")
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	t.Setenv(containerwrite.RunEnv, "dlg-0123456789ab")
	s := NewStore(wipnoteDir)
	sid := "cccccccc-1111-2222-3333-444444444444"
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	if wrote, err := s.Open(Record{SessionID: sid, Harness: "claude", StartedAt: t0}); err != nil || !wrote {
		t.Fatalf("Open = %v, %v; want a new own row", wrote, err)
	}
	if wrote, _ := s.Open(Record{SessionID: sid, Harness: "claude", StartedAt: t0}); wrote {
		t.Error("a replayed start reported a new row")
	}
	if err := s.Close(sid, t0.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Errorf("container worker wrote the shared sessions ledger (err %v)", err)
	}
	own, _ := containerwrite.ReadSessions(runDir)
	if len(own) != 1 || own[0].Harness != "claude" || !own[0].EndedAt.Equal(t0.Add(time.Hour)) {
		t.Errorf("own rows %+v", own)
	}
	if _, err := s.Correct(sid, t0, EndSourceLiveClose, false); !errors.Is(err, ErrContainerWorker) {
		t.Errorf("Correct err = %v, want ErrContainerWorker", err)
	}
}

// TestHost_RoundTrips: a folded row keeps host=container; a host row has no
// data-host attribute, so existing rows' bytes do not change.
func TestHost_RoundTrips(t *testing.T) {
	t.Setenv(containerwrite.PlaceEnv, "")
	s := NewStore(filepath.Join(t.TempDir(), ".wipnote"))
	t0 := time.Date(2026, 10, 27, 9, 0, 0, 0, time.UTC)
	c := "dddddddd-1111-2222-3333-444444444444"
	h := "eeeeeeee-1111-2222-3333-444444444444"
	if _, err := s.Open(Record{SessionID: c, Harness: "codex", StartedAt: t0, Host: HostContainer}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Open(Record{SessionID: h, Harness: "claude", StartedAt: t0}); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(c, t0.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, _, _ := s.Get(c)
	if got.Host != HostContainer || got.Harness != "codex" || got.IsOpen() {
		t.Errorf("container row %+v: want host=container, codex, closed", got)
	}
	data, _ := os.ReadFile(s.Path())
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, h) && strings.Contains(line, "data-host") {
			t.Errorf("host row carries data-host: %s", line)
		}
	}
}
