package hooks

// Test-only resets for process-level hook state.

// ResetDaemonGuaranteeBreach clears the daemon guarantee latch. A hook
// process is short-lived and never needs to clear it.
func ResetDaemonGuaranteeBreach() {
	breachMu.Lock()
	defer breachMu.Unlock()
	breachReason = ""
}
