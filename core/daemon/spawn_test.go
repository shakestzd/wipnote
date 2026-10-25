package daemon

import (
	"context"
	"os"
	"testing"
	"time"
)

// A hook or CLI Submit with no writer up falls back at once and leaves no
// trace of a start: no lease, no lock, no writer log (bug-5ca53f70).
func TestSubmit_NoWriterFallsBackWithoutStarting(t *testing.T) {
	root := t.TempDir()
	_ = os.MkdirAll(root+"/.wipnote", 0o755)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	start := time.Now()
	_, err := NewWriterClient(root).Submit(ctx, Envelope{OpID: "x", OpType: "test"})
	if err != ErrWriterUnavailable {
		t.Fatalf("err = %v, want ErrWriterUnavailable", err)
	}
	if elapsed := time.Since(start); elapsed > 100*time.Millisecond {
		t.Fatalf("Submit waited %v with no writer, want an immediate fallback", elapsed)
	}
	for _, p := range []string{LeasePath(root), root + "/.wipnote/" + leaseLockName, root + "/.wipnote/logs/writer.log"} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("Submit left %s behind (err %v): it must never start a writer", p, err)
		}
	}
}

// Submit against a running writer uses it.
func TestSubmit_FastPath(t *testing.T) {
	applier, calls := countingApplier()
	_, sock, cleanup := newTestListener(t, applier)
	defer cleanup()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	ack, err := NewWriterClientForSocket(sock).Submit(ctx, Envelope{OpID: "fp-1", OpType: "test"})
	if err != nil || ack.Status != AckApplied || calls.Load() != 1 {
		t.Fatalf("ack %+v err %v calls %d, want one applied op", ack, err, calls.Load())
	}
}
