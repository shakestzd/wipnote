package gateledger_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/containerwrite"
	"github.com/shakestzd/wipnote/core/gateledger"
)

const gateRun = "dlg-0123456789ab"

// TestAppend_ContainerWorkerWritesOwnFile: inside a container worker a gate
// run goes to the worker's own gate-<id>.json, never the shared ledger, and
// the worker's own reads still find it (a completion gate in the container).
func TestAppend_ContainerWorkerWritesOwnFile(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	runDir := filepath.Join(wipnoteDir, "delegate", "runs", gateRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, containerwrite.PlaceContainer)
	t.Setenv(containerwrite.RunEnv, gateRun)
	store := gateledger.NewStore(wipnoteDir)
	rec, err := store.Append(passing("sess-c", "feat-c", time.Now().UTC()))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path()); !os.IsNotExist(err) {
		t.Errorf("container worker wrote the shared gate ledger (err %v)", err)
	}
	if _, err := os.Stat(filepath.Join(runDir, "gate-"+rec.ID+".json")); err != nil {
		t.Errorf("no own gate file: %v", err)
	}
	got, err := store.LatestForSession("sess-c")
	if err != nil || got == nil || got.ID != rec.ID {
		t.Errorf("LatestForSession = %+v, %v; want the own record", got, err)
	}
	own, err := gateledger.ReadOwn(runDir)
	if err != nil || len(own) != 1 || own[0].Signature != rec.Signature {
		t.Errorf("ReadOwn = %+v, %v; want the record with its signature", own, err)
	}
}

// TestAppend_HostWritesSharedLedger is the other side: on the host the run
// is appended to the shared ledger and no own file is written.
func TestAppend_HostWritesSharedLedger(t *testing.T) {
	wipnoteDir := filepath.Join(t.TempDir(), ".wipnote")
	runDir := filepath.Join(wipnoteDir, "delegate", "runs", gateRun)
	if err := os.MkdirAll(runDir, 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv(containerwrite.PlaceEnv, "")
	t.Setenv(containerwrite.RunEnv, gateRun)
	store := gateledger.NewStore(wipnoteDir)
	if _, err := store.Append(passing("sess-h", "feat-h", time.Now().UTC())); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(store.Path()); err != nil {
		t.Errorf("host did not write the shared ledger: %v", err)
	}
	if own, _ := gateledger.ReadOwn(runDir); len(own) != 0 {
		t.Errorf("host wrote own gate files: %+v", own)
	}
}
