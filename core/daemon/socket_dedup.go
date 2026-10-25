package daemon

import (
	"fmt"
	"os"
	"path/filepath"
)

// unlinkStaleSocket removes a leftover socket inode so a fresh bind succeeds
// after an unclean exit. It refuses to unlink when a LIVE lease owner exists
// for the same project (lease co-located with the socket under .wipnote/): the
// lease, not the socket, is the ownership authority, so a live owner's socket
// must never be stomped. However, if the lease is held by ownerPID (the current
// lease holder), the socket is reclaimable and will be unlinked — the lease
// holder may clear its own stale socket before binding (feat-075c110d bugfix).
// A missing socket is a no-op.
func unlinkStaleSocket(sockPath string, ownerPID int) error {
	if _, err := os.Lstat(sockPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("stat socket %s: %w", sockPath, err)
	}
	// The lease lives next to the socket: .wipnote/writer.pid alongside
	// .wipnote/writer.sock. If a live owner still holds it, do not unlink —
	// unless the owner is us (ownerPID == our pid), in which case the socket
	// is our stale leftover and we may reclaim it.
	leasePath := filepath.Join(filepath.Dir(sockPath), "writer.pid")
	if leaseOwnerAlive(leasePath) {
		// Check if the live owner is us (the lease holder passed to NewListener).
		if ownerPID != os.Getpid() {
			return fmt.Errorf("daemon: refusing to unlink socket %s: a live writer holds the lease", sockPath)
		}
		// The lease is held by the current process — the socket is our stale
		// leftover, safe to unlink.
	}
	if err := os.Remove(sockPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("clear stale socket: %w", err)
	}
	return nil
}

// recordApplied adds opID to the APPLIED dedup set (no-op for an empty id). Used
// by the sync path AFTER SubmitSync confirms the commit, so a later duplicate of
// a known-committed op durably dedups (AckDuplicate). It also defensively clears
// any stale pending entry for the same id.
func (l *Listener) recordApplied(opID string) {
	if opID == "" {
		return
	}
	l.dedupMu.Lock()
	l.dedupPending.remove(opID)
	l.dedupApplied.add(opID)
	l.dedupMu.Unlock()
}

// recordPending adds opID to the PENDING dedup set (no-op for an empty id). Used
// by the async path on ENQUEUE, before the worker commits. A pending op_id is
// NOT durably deduped — a duplicate that arrives while it is pending is
// re-enqueued (roborev-482 round-5 finding 1).
func (l *Listener) recordPending(opID string) {
	if opID == "" {
		return
	}
	l.dedupMu.Lock()
	l.dedupPending.add(opID)
	l.dedupMu.Unlock()
}

// dropPending removes opID from the PENDING set (no-op for an empty id). The
// single-writer worker calls it from the op wrapper when an async op's apply
// FAILS (so a resubmit re-runs), and submitEnqueueOnly calls it when Submit
// itself fails. Invoked on the worker goroutine, so it takes dedupMu.
func (l *Listener) dropPending(opID string) {
	if opID == "" {
		return
	}
	l.dedupMu.Lock()
	l.dedupPending.remove(opID)
	l.dedupMu.Unlock()
}

// promotePendingToApplied moves opID from the PENDING set to the APPLIED set
// (no-op for an empty id). The single-writer worker calls it from the op wrapper
// when an async op's apply SUCCEEDS, so a post-success duplicate durably dedups
// instead of needlessly re-applying (roborev-482 round-5 finding 1). Invoked on
// the worker goroutine, so it takes dedupMu.
func (l *Listener) promotePendingToApplied(opID string) {
	if opID == "" {
		return
	}
	l.dedupMu.Lock()
	l.dedupPending.remove(opID)
	l.dedupApplied.add(opID)
	l.dedupMu.Unlock()
}

// isApplied reports whether opID is in the APPLIED dedup set (false for an
// empty id).
//
// In-memory dedup (roborev-482 round-5 finding 1): ONLY a CONFIRMED-APPLIED
// op_id returns a durable AckDuplicate without re-running. A still-PENDING
// async op_id (enqueued but not yet confirmed-applied) is deliberately NOT
// short-circuited — if we acked it duplicate the caller would skip its
// fallback, and a subsequent apply FAILURE of the first op would silently lose
// this submission. Instead a pending duplicate falls through and is
// RE-ENQUEUED (every async-routed op is idempotent), giving it its own apply
// attempt. (Durable dedup across restart is slice-3.)
func (l *Listener) isApplied(opID string) bool {
	if opID == "" {
		return false
	}
	l.dedupMu.Lock()
	defer l.dedupMu.Unlock()
	return l.dedupApplied.contains(opID)
}
