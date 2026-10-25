package hooks

// Test-only resets and readers for process-level hook state.

// ResetDaemonGuaranteeBreach clears the daemon guarantee latch. A hook
// process is short-lived and never needs to clear it.
func ResetDaemonGuaranteeBreach() {
	breachMu.Lock()
	defer breachMu.Unlock()
	breachReason = ""
}

// FallbackCounts returns the fallback counters (writer_unavailable,
// queue_full, timeout).
func FallbackCounts() (writerUnavailable, queueFull, timeout int64) {
	return fallbackWriterUnavailable.Load(), fallbackQueueFull.Load(), fallbackTimeout.Load()
}

// ResetFallbackCounts zeroes the fallback counters.
func ResetFallbackCounts() {
	fallbackWriterUnavailable.Store(0)
	fallbackQueueFull.Store(0)
	fallbackTimeout.Store(0)
}
