package container

import (
	"errors"
	"os"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/gateledger"
)

// gateCount is how many times gate is in the host's gate ledger.
func gateCount(t *testing.T, wipnoteDir string, gate gateledger.Record) int {
	t.Helper()
	gates, err := gateledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, g := range gates {
		if g.ID == gate.ID {
			n++
		}
	}
	return n
}

// TestFold_GateFailureLeavesFolderUnfolded: a gate append that fails (a
// transient lock or IO error) leaves the folder unfolded, so FoldPending
// retries it and the gate run reaches the host ledger exactly once. The
// other side: once folded, a further FoldPending folds nothing.
func TestFold_GateFailureLeavesFolderUnfolded(t *testing.T) {
	wipnoteDir, runDir := foldProject(t)
	gate := workerWrites(t, wipnoteDir, "abababab-1111-2222-3333-444444444444", time.Now().UTC())
	old := time.Now().Add(-time.Hour) // the worker wrote well before this fold
	for _, path := range ownFiles(runDir) {
		_ = os.Chtimes(path, old, old)
	}
	foldGateErr = func() error { return errors.New("gate ledger locked") }
	t.Cleanup(func() { foldGateErr = nil })
	if _, err := Fold(wipnoteDir, runDir); err == nil {
		t.Fatal("a failed gate append was not reported")
	}
	if !Unfolded(runDir) {
		t.Fatal("a fold whose gate append failed marked the folder folded")
	}
	foldGateErr = nil
	later := time.Now().Add(time.Hour)
	foldNow = func() time.Time { return later }
	t.Cleanup(func() { foldNow = time.Now })
	if n, err := FoldPending(wipnoteDir); err != nil || n != 1 {
		t.Fatalf("FoldPending = %d, %v; want the folder retried", n, err)
	}
	if c := gateCount(t, wipnoteDir, gate); c != 1 {
		t.Fatalf("gate recorded %d times, want once", c)
	}
	if Unfolded(runDir) {
		t.Error("still unfolded after a fold that succeeded")
	}
	if n, _ := FoldPending(wipnoteDir); n != 0 || gateCount(t, wipnoteDir, gate) != 1 {
		t.Errorf("a further FoldPending folded %d folders", n)
	}
}
