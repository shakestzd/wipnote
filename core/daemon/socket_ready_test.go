package daemon

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db/writequeue"
)

// newGatedListener is newTestListener with a readiness gate.
func newGatedListener(t *testing.T, applier Applier, ready func() bool) (*Listener, string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	sock := SocketPath(dir)
	q := writequeue.New(writequeue.Config{Capacity: 16})
	if err := q.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	ln, err := NewListener(ListenerConfig{SocketPath: sock, Queue: q, Applier: applier, Ready: ready})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	go func() { _ = ln.Serve(ctx) }()
	waitForSocket(t, sock)
	t.Cleanup(func() { cancel(); _ = ln.Close(); q.Stop(time.Second) })
	return ln, sock
}

// An applied-ack write that arrives while the startup projection is building
// gets an error ack at once, so the caller falls back instead of waiting.
func TestNotReady_SyncWriteRejectedAtOnce(t *testing.T) {
	applier, calls := countingApplier()
	_, sock := newGatedListener(t, applier, func() bool { return false })

	start := time.Now()
	ack, err := NewWriterClientForSocket(sock).Submit(context.Background(), Envelope{OpID: "s1", OpType: "test"})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != AckError || ack.Error != NotReadyError {
		t.Fatalf("ack = %+v, want error ack %q", ack, NotReadyError)
	}
	if elapsed > 200*time.Millisecond {
		t.Fatalf("rejection took %v, want an immediate reply", elapsed)
	}
	if calls.Load() != 0 {
		t.Fatal("a rejected op must not be applied")
	}
}

// An async write is accepted while not ready and applies once ready.
func TestNotReady_AsyncWriteEnqueued(t *testing.T) {
	applier, calls := countingApplier()
	_, sock := newGatedListener(t, applier, func() bool { return false })

	ack, err := NewWriterClientForSocket(sock).Submit(context.Background(), Envelope{OpID: "a1", OpType: "test", Async: true})
	if err != nil {
		t.Fatal(err)
	}
	if ack.Status != AckEnqueued {
		t.Fatalf("ack = %q, want %q", ack.Status, AckEnqueued)
	}
	waitForCalls(t, calls, 1)
}

// Readiness does not keep a daemon with no client alive (bug-5ca53f70):
// idleness is the same before and after the projection is built, and
// applied-ack writes go through once it is ready.
func TestNotReady_IdleWithoutClientThenApplies(t *testing.T) {
	var ready atomic.Bool
	applier, calls := countingApplier()
	ln, sock := newGatedListener(t, applier, ready.Load)

	ln.lastActivity.Store(time.Now().Add(-time.Hour).UnixNano())
	if !ln.isIdle(time.Minute) {
		t.Fatal("a building daemon with no client for the window should be idle")
	}
	ready.Store(true)
	if !ln.isIdle(time.Minute) {
		t.Fatal("a ready daemon with no activity should be idle")
	}
	ack, err := NewWriterClientForSocket(sock).Submit(context.Background(), Envelope{OpID: "s2", OpType: "test"})
	if err != nil || ack.Status != AckApplied {
		t.Fatalf("ack = %+v err = %v, want applied", ack, err)
	}
	if calls.Load() != 1 {
		t.Fatalf("applier ran %d times, want 1", calls.Load())
	}
}

// trueBinary is an executable that starts and exits without binding a socket:
// a daemon that is spawned but not up yet.
func trueBinary(t *testing.T) string {
	t.Helper()
	p, err := exec.LookPath("true")
	if err != nil {
		t.Skip("no `true` binary on PATH")
	}
	return p
}

// A daemon that is spawned but not up within the caller's deadline is
// reported as "starting", which the launcher treats as no failure at all.
func TestEnsureDaemon_NotUpYetIsErrDaemonStarting(t *testing.T) {
	dir := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, err := EnsureDaemon(ctx, dir, trueBinary(t), os.Getpid())
	if !errors.Is(err, ErrDaemonStarting) {
		t.Fatalf("err = %v, want ErrDaemonStarting", err)
	}
}
