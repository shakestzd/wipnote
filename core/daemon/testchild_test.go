package daemon

import (
	"context"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/db/writequeue"
)

// Test child: the daemon test binary re-executes itself as a minimal writer
// server so the lease and socket rules can be tested across real processes.
// The child takes the lease, binds the socket and serves, exactly like
// runWriterOnly in cmd/wipnote, but with no projection or maintenance.
const (
	childModeEnv = "WIPNOTE_DAEMON_TEST_CHILD"
	childRootEnv = "WIPNOTE_DAEMON_TEST_ROOT"
	childIdleEnv = "WIPNOTE_DAEMON_TEST_IDLE"
)

// Exit codes a test child uses. A loser of the lease race exits childLost.
const (
	childServed  = 0
	childLost    = 3
	childNoBind  = 4
	childBadArgs = 5
)

// barrierPath is the file the parent creates to release every waiting child
// at once, so the children race on the lease instead of starting in turn.
func barrierPath(root string) string { return filepath.Join(root, "go") }

// runTestChildIfAsked runs the child server and exits when the test binary
// was started as a child. It returns normally in the parent test process.
func runTestChildIfAsked() {
	if os.Getenv(childModeEnv) != "serve" {
		return
	}
	os.Exit(runTestChild())
}

func runTestChild() int {
	root := os.Getenv(childRootEnv)
	idle, err := time.ParseDuration(os.Getenv(childIdleEnv))
	if root == "" || err != nil {
		return childBadArgs
	}
	applyChildTuning()
	waitForBarrier(root)
	lease, err := AcquireLease(root)
	if err != nil {
		return childLost
	}
	defer lease.Release()
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	return serveTestChild(ctx, stop, root, lease, idle)
}

func serveTestChild(ctx context.Context, stop context.CancelFunc, root string, lease *Lease, idle time.Duration) int {
	q := writequeue.New(writequeue.Config{Capacity: 16})
	if err := q.Start(ctx); err != nil {
		return childNoBind
	}
	defer q.Stop(time.Second)
	keepChildLease(ctx, stop, lease)
	ln, err := NewListener(childListenerConfig(root, q, lease))
	if err != nil {
		return childNoBind
	}
	defer ln.Close()
	_ = ln.ServeWithIdleTimeout(ctx, idle)
	return childServed
}

func waitForBarrier(root string) {
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(barrierPath(root)); err == nil {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
}
