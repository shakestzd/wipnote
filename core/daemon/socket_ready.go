package daemon

// Readiness gate (bug-06d2c91d).
//
// The writer daemon binds its socket BEFORE its startup projection is built,
// so a launcher or hook never waits for that build. Its owner submits the build
// as the FIRST op on the single-writer queue, so FIFO order alone guarantees
// that every op enqueued after it applies against the built projection.
//
// While the owner reports not ready:
//
//   - Async (enqueue-only) writes are accepted. They are acked on enqueue and
//     apply after the build, in order.
//   - Applied-ack writes are rejected AT ONCE with an error ack. The caller
//     would otherwise wait for the build inside its own deadline. An error ack
//     sends it to its direct-write fallback in microseconds, which is exactly
//     what it did before, when no socket existed yet.
//   - Reads are unaffected: the Reader works from canonical files, not from
//     the projection.
//   - The daemon is never idle, so it cannot exit before the build ends.

// NotReadyError is the error text of the ack an applied-ack write gets while
// the daemon's startup projection is still being built.
const NotReadyError = "writer not ready: startup projection still building"

// isReady reports whether the owner's state is loaded. A nil gate is ready.
func (l *Listener) isReady() bool {
	return l.ready == nil || l.ready()
}

// notReadyAck returns the immediate rejection for an applied-ack write that
// arrives before the daemon is ready. rejected is false for async writes and
// whenever the daemon is ready.
func (l *Listener) notReadyAck(env Envelope) (ack Ack, rejected bool) {
	if env.Async || l.isReady() {
		return Ack{}, false
	}
	return Ack{Status: AckError, Seq: l.seq.Add(1), Error: NotReadyError}, true
}
