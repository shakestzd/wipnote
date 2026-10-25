package daemon

import (
	"context"
	"os"
	"testing"
	"time"
)

// herdSize and herdRounds size the concurrent-starter tests. Every round
// starts herdSize real processes on one project and releases them together.
const (
	herdSize   = 32
	herdRounds = 4
)

// Fast watch and heartbeat for herd children, so a duplicate would show
// itself inside assertOneServer's settle time.
var herdTuning = []string{childTickEnv + "=100ms", childBeatEnv + "=100ms"}

// A stale lease must still admit exactly one server when many starters race
// to reclaim it (bug-5ca53f70). Before the fix, the check-remove-create
// reclaim let two or three racers each believe they had won.
func TestLeaseHerd_StaleLeaseOneServer(t *testing.T) {
	for r := 0; r < herdRounds; r++ {
		root := newHerdRoot(t)
		if err := writeStaleLeaseForTest(root); err != nil {
			t.Fatal(err)
		}
		h := startHerd(t, root, herdSize, time.Minute, herdTuning...)
		assertOneServer(t, h, r)
		h.stop()
	}
}

// With no lease at all, and with a lease that went stale while empty, the
// herd still yields one server, 64 starters at a time.
func TestLeaseHerd_AbsentAndStaleEmptyLease(t *testing.T) {
	for r := 0; r < herdRounds; r++ {
		root := newHerdRoot(t)
		if r%2 == 1 {
			_ = os.WriteFile(LeasePath(root), nil, 0o644)
			old := time.Now().Add(-2 * leaseStaleAfter)
			_ = os.Chtimes(LeasePath(root), old, old)
		}
		h := startHerd(t, root, 2*herdSize, time.Minute, herdTuning...)
		assertOneServer(t, h, r)
		h.stop()
	}
}

// A live server keeps the project: a herd arriving later all lose.
func TestLeaseHerd_HeldLiveKeepsOneServer(t *testing.T) {
	root := newHerdRoot(t)
	h := startHerd(t, root, 1, time.Minute, herdTuning...)
	first := waitSocketOwner(root, 10*time.Second)
	for r := 0; r < herdRounds; r++ {
		h.wave(herdSize, time.Minute, herdTuning)
		assertOneServer(t, h, r)
		if got := socketOwnerPID(root); got != first {
			t.Fatalf("round %d: socket moved from pid %d to %d", r, first, got)
		}
	}
}

// A lease no herd may take: a fresh foreign-namespace owner whose pid is
// dead here, or a half-written lease younger than the stale window. Every
// starter loses and the lease is untouched.
func TestLeaseHerd_ForeignOrHalfWrittenLeaseIsHeld(t *testing.T) {
	for _, kind := range []string{"foreign", "empty", "partial"} {
		root := newHerdRoot(t)
		want := writeHeldLeaseForTest(t, root, kind)
		h := startHerd(t, root, herdSize, time.Minute, herdTuning...)
		if alive := h.waitAlive(0, 10*time.Second); len(alive) != 0 {
			t.Fatalf("%s lease: %d starters still running %v, want none", kind, len(alive), alive)
		}
		if codes := h.exitCodes(); codes[childLost] != herdSize {
			t.Fatalf("%s lease: exit codes %v, want all %d to lose the lease", kind, codes, herdSize)
		}
		assertLeaseBytes(t, root, want)
	}
}

func writeHeldLeaseForTest(t *testing.T, root, kind string) []byte {
	t.Helper()
	switch kind {
	case "foreign":
		return writeRecordForTest(t, root, nil)
	case "empty":
		_ = os.WriteFile(LeasePath(root), nil, 0o644)
		return []byte{}
	}
	data := []byte(`{"format":2,"tok`)
	_ = os.WriteFile(LeasePath(root), data, 0o644)
	return data
}

// On a slow file system (latency on every lease and socket-dir operation)
// the herd still yields one server, and a hook's read never waits for it.
func TestLeaseHerd_SlowFilesystemOneServerHooksDoNotWait(t *testing.T) {
	root := newHerdRoot(t)
	if err := writeStaleLeaseForTest(root); err != nil {
		t.Fatal(err)
	}
	slow := append([]string{childLatencyEnv + "=15ms"}, herdTuning...)
	h := startHerd(t, root, herdSize/2, time.Minute, slow...)
	worst := hookSubmitWorst(root, 2*time.Second)
	if worst > 250*time.Millisecond {
		t.Fatalf("a hook read waited %v while the daemon was starting", worst)
	}
	assertOneServer(t, h, 0)
}

// hookSubmitWorst reads as a hook does for d and returns the slowest call.
func hookSubmitWorst(root string, d time.Duration) time.Duration {
	var worst time.Duration
	for end := time.Now().Add(d); time.Now().Before(end); {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		start := time.Now()
		_, _ = NewReadClient(root).Ping(ctx)
		cancel()
		worst = max(worst, time.Since(start))
		time.Sleep(10 * time.Millisecond)
	}
	return worst
}

// A real server process that loses the lease or its socket exits within the
// watch interval, though a live launcher is attached and idle-exit is far.
func TestServer_DuplicateExitsWithinWatchInterval(t *testing.T) {
	for _, loss := range []string{"lease", "socket"} {
		root := newHerdRoot(t)
		h := startHerd(t, root, 1, time.Hour, herdTuning...)
		pid := waitSocketOwner(root, 10*time.Second)
		attachSelf(t, root)
		if loss == "lease" {
			writeRecordForTest(t, root, nil)
		} else {
			_ = os.Remove(SocketPath(root))
		}
		if !h.waitExit(pid, 2*time.Second) {
			t.Fatalf("server %d kept running after losing its %s", pid, loss)
		}
	}
}

// An orphaned server with no client exits within its idle timeout; one with
// a live attached launcher stays.
func TestServer_OrphanIdlesOutAttachedStays(t *testing.T) {
	orphanRoot := newHerdRoot(t)
	orphans := startHerd(t, orphanRoot, 1, 500*time.Millisecond, herdTuning...)
	orphan := waitSocketOwner(orphanRoot, 10*time.Second)

	keptRoot := newHerdRoot(t)
	kept := startHerd(t, keptRoot, 1, 500*time.Millisecond, herdTuning...)
	keptPID := waitSocketOwner(keptRoot, 10*time.Second)
	attachSelf(t, keptRoot)

	if !orphans.waitExit(orphan, 3*time.Second) {
		t.Fatalf("orphaned server %d did not idle out", orphan)
	}
	if kept.waitExit(keptPID, 2*time.Second) {
		t.Fatalf("server %d with a live attached launcher exited", keptPID)
	}
}

func attachSelf(t *testing.T, root string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := NewReadClient(root).Attach(ctx, os.Getpid()); err != nil {
		t.Fatalf("attach: %v", err)
	}
}
