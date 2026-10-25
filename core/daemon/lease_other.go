//go:build !linux

package daemon

// localNamespace is unknown off Linux, so no lease record is ever judged to
// share this process's namespace: staleness is the heartbeat alone.
func localNamespace() (bootID, pidNS string) { return "", "" }

// legacyWriterAlive trusts the kill(pid, 0) check that ran before it. There
// is no /proc to confirm the process is a writer.
func legacyWriterAlive(pid int) bool { return true }
