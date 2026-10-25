// Package daemon implements the per-project write-owner transport
// (plan-bb91616a Phase A). It graduates the in-process serve_child
// writequeue into a process-spanning single-writer service reachable over
// a per-project Unix domain socket.
//
//   - lease*.go   — the heartbeat single-owner lease (writer.lease.pid)
//   - socket*.go  — the Unix-socket listener that applies versioned write-op
//     envelopes through the existing writequeue
//   - client.go   — WriterClient.Submit (dial, send envelope, await ack)
//   - ensure.go   — the launcher's EnsureDaemon, the only code that starts a
//     writer (bug-5ca53f70)
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"
)

// LeasePath returns the path of the per-project writer lease file. The
// lease lives under .wipnote/ alongside the socket so a future global
// coordinator (trk-cb80b7da) can discover it by project root. Its name ends
// in .pid so the .wipnote/.gitignore every project already has ignores it.
func LeasePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".wipnote", leaseFileName)
}

// LegacyLeasePath is the bare-pid lease that writers before bug-5ca53f70
// held. New writers never write it; they only refuse to fight a live owner
// that still holds it.
func LegacyLeasePath(projectRoot string) string {
	return filepath.Join(projectRoot, ".wipnote", legacyLeaseName)
}

// SocketPath returns the path of the per-project writer Unix domain
// socket. Co-located with the lease so both are registry-discoverable
// from the project root with no protocol change (slice-1 coordinator-ready
// constraint).
func SocketPath(projectRoot string) string {
	return filepath.Join(projectRoot, ".wipnote", "writer.sock")
}

// ErrLeaseHeld is returned by AcquireLease when another owner holds the
// lease, or is taking it right now. The caller must not serve.
var ErrLeaseHeld = fmt.Errorf("daemon: writer lease already held by a live owner")

// Lease is an acquired single-owner write lease. The holder is the sole
// authority that may bind the socket and run the writequeue for its project.
type Lease struct {
	dir    string
	path   string
	mu     sync.Mutex
	record LeaseRecord
	lost   atomic.Bool
	stop   context.CancelFunc
}

// AcquireLease attempts to become the single write-owner for projectRoot.
//
// A lease is free when the file is absent, when its heartbeat is older than
// the stale window, or when it does not parse and is older than the window.
// A live legacy writer.pid owner also counts as held. The read-only check
// runs first so the losers of a race never touch the lock; the winner
// re-checks and writes under the mutation lock (lease_lock.go), so exactly
// one racer can turn a free lease into its own.
func AcquireLease(projectRoot string) (*Lease, error) {
	dir := filepath.Join(projectRoot, ".wipnote")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("ensure .wipnote dir: %w", err)
	}
	if inspectLeaseDir(dir, time.Now()).Held() {
		return nil, ErrLeaseHeld
	}
	unlock, err := lockLease(dir, lockWaitBudget)
	if err != nil {
		return nil, err
	}
	defer unlock()
	if inspectLeaseDir(dir, time.Now()).Held() {
		return nil, ErrLeaseHeld
	}
	l := &Lease{dir: dir, path: filepath.Join(dir, leaseFileName), record: newLeaseRecord()}
	if err := l.writeRecord(); err != nil {
		return nil, fmt.Errorf("write lease: %w", err)
	}
	return l, nil
}

// writeRecord replaces the lease file with the current record through a
// temp file and a rename, so no reader ever sees a partial record. The
// caller holds the mutation lock.
func (l *Lease) writeRecord() error {
	l.mu.Lock()
	data, err := json.Marshal(l.record)
	l.mu.Unlock()
	if err != nil {
		return err
	}
	tmp := filepath.Join(l.dir, ".writer.lease."+newToken()+".pid")
	if err := leaseFS.CreateExcl(tmp, data); err != nil {
		return err
	}
	if err := leaseFS.Rename(tmp, l.path); err != nil {
		_ = leaseFS.Remove(tmp)
		return err
	}
	return nil
}

// Verify reports whether the lease file still names this owner. A lease
// replaced by a reclaimer, or removed, is no longer held. A read error that
// is not "missing" leaves the last known answer in place.
func (l *Lease) Verify() bool {
	if l == nil || l.lost.Load() {
		return false
	}
	data, err := leaseFS.ReadFile(l.path)
	if errors.Is(err, fs.ErrNotExist) {
		return false
	}
	if err != nil {
		return true
	}
	rec, ok := parseLeaseRecord(data)
	return ok && rec.Token == l.Token()
}

// Held reports whether the heartbeat has not yet found the lease lost.
func (l *Lease) Held() bool { return l != nil && !l.lost.Load() }

// Token returns this acquisition's owner token.
func (l *Lease) Token() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.record.Token
}

// Release removes the lease file, but only while it still names this owner:
// a reclaimer's lease is never removed. It stops the heartbeat. Safe to call
// more than once. When the lock cannot be had in time the file is left to go
// stale, which is always safe.
func (l *Lease) Release() error {
	if l == nil {
		return nil
	}
	l.stopKeeping()
	unlock, err := lockLease(l.dir, lockWaitBudget)
	if err != nil {
		return nil
	}
	defer unlock()
	if !l.Verify() {
		return nil
	}
	l.lost.Store(true)
	if err := leaseFS.Remove(l.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// Path returns the lease file path (for diagnostics/tests).
func (l *Lease) Path() string {
	if l == nil {
		return ""
	}
	return l.path
}
