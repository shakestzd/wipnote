package daemon

import (
	"context"
	"os"

	"github.com/shakestzd/wipnote/core/db/writequeue"
)

// The pieces of the test child that depend on the lease API.

func applyChildTuning() {}

func keepChildLease(ctx context.Context, stop context.CancelFunc, lease *Lease) {}

func childListenerConfig(root string, q *writequeue.Queue, lease *Lease) ListenerConfig {
	return ListenerConfig{SocketPath: SocketPath(root), Queue: q, Applier: RejectingApplier, OwnerPID: os.Getpid()}
}

// leaseOwnerPIDForTest returns the pid recorded in the lease, or 0.
func leaseOwnerPIDForTest(root string) int {
	data, err := os.ReadFile(LeasePath(root))
	if err != nil {
		return 0
	}
	n := 0
	for _, c := range string(data) {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	return n
}

// writeStaleLeaseForTest leaves a lease whose owner is gone.
func writeStaleLeaseForTest(root string) error {
	return os.WriteFile(LeasePath(root), []byte("999999\n"), 0o644)
}
