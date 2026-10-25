package daemon

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
