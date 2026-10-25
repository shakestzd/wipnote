package daemon

import (
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"time"
)

// Tuning a test child reads from its environment. Each value is a Go
// duration; an unset value keeps the production default.
const (
	childTickEnv    = "WIPNOTE_DAEMON_TEST_TICK"    // idleCheckInterval
	childBeatEnv    = "WIPNOTE_DAEMON_TEST_BEAT"    // leaseHeartbeat
	childLatencyEnv = "WIPNOTE_DAEMON_TEST_LATENCY" // added to every lease file op
)

func applyChildTuning() {
	if d, ok := envDuration(childTickEnv); ok {
		idleCheckInterval = d
	}
	if d, ok := envDuration(childBeatEnv); ok {
		leaseHeartbeat = d
	}
	if d, ok := envDuration(childLatencyEnv); ok {
		leaseFS = slowLeaseFS{inner: osLeaseFS{}, delay: d}
	}
}

func envDuration(name string) (time.Duration, bool) {
	d, err := time.ParseDuration(os.Getenv(name))
	return d, err == nil && d > 0
}

func keepChildLease(ctx context.Context, stop context.CancelFunc, lease *Lease) {
	lease.Keep(ctx, stop)
}

func childListenerConfig(root string, lease *Lease) ListenerConfig {
	return ListenerConfig{SocketPath: SocketPath(root), Lease: lease}
}

// leaseOwnerPIDForTest returns the pid recorded in the lease, or 0.
func leaseOwnerPIDForTest(root string) int {
	data, err := os.ReadFile(LeasePath(root))
	if err != nil {
		return 0
	}
	var rec LeaseRecord
	if json.Unmarshal(data, &rec) != nil {
		return 0
	}
	return rec.PID
}

// writeStaleLeaseForTest leaves a lease whose heartbeat stopped an hour ago.
func writeStaleLeaseForTest(root string) error {
	rec := newLeaseRecord()
	rec.PID = 999999
	rec.HeartbeatAt = time.Now().Add(-time.Hour)
	data, _ := json.Marshal(rec)
	return os.WriteFile(LeasePath(root), data, 0o644)
}

// slowLeaseFS adds delay before every lease and socket-dir file operation,
// standing in for a slow bind mount.
type slowLeaseFS struct {
	inner leaseFileOps
	delay time.Duration
}

func (s slowLeaseFS) wait() { time.Sleep(s.delay) }

func (s slowLeaseFS) ReadFile(n string) ([]byte, error)   { s.wait(); return s.inner.ReadFile(n) }
func (s slowLeaseFS) Stat(n string) (fs.FileInfo, error)  { s.wait(); return s.inner.Stat(n) }
func (s slowLeaseFS) Lstat(n string) (fs.FileInfo, error) { s.wait(); return s.inner.Lstat(n) }
func (s slowLeaseFS) CreateExcl(n string, d []byte) error { s.wait(); return s.inner.CreateExcl(n, d) }
func (s slowLeaseFS) Rename(a, b string) error            { s.wait(); return s.inner.Rename(a, b) }
func (s slowLeaseFS) Link(a, b string) error              { s.wait(); return s.inner.Link(a, b) }
func (s slowLeaseFS) Remove(n string) error               { s.wait(); return s.inner.Remove(n) }
