package delegationledger

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/containerwrite"
)

// TestOpen_RefusedInContainerWorker: a container worker never writes the
// shared delegations ledger (a nested delegation from inside the container is
// refused); the host opens rows as before.
func TestOpen_RefusedInContainerWorker(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	run := "dlg-0123456789ab"
	if err := os.MkdirAll(filepath.Join(wipnoteDir, "delegate", "runs", run), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.RunEnv, run)
	s := NewStore(wipnoteDir)
	rec := Record{Mode: ModeOneshot, Harness: "codex", Host: "host"}
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	if _, err := s.Open(rec); !errors.Is(err, ErrContainerWorker) {
		t.Fatalf("container Open err = %v, want ErrContainerWorker", err)
	}
	if _, err := os.Stat(s.Path()); !os.IsNotExist(err) {
		t.Errorf("container worker created the shared ledger (err %v)", err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	if _, err := s.Open(rec); err != nil {
		t.Fatalf("host Open: %v", err)
	}
}
