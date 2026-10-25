package daemon

import (
	"fmt"
	"strconv"
	"strings"
)

// OlderOwnerNotice returns one line for the launcher to print when the
// project's writer lease is held by an older wipnote, or "" (bug-5ca53f70).
//
// A new binary cannot stop an older writer safely: it may serve another
// session, and older writers do not check whether they still own the lease.
// So the launcher leaves it running and says so. Two cases are detected: a
// live legacy owner of writer.pid (from before the heartbeat lease), and a
// heartbeat owner whose record carries an older version than selfVersion.
func OlderOwnerNotice(projectRoot, selfVersion string) string {
	state := InspectLease(projectRoot)
	switch {
	case state.Status == LeaseLegacyHeld:
		return fmt.Sprintf("wipnote: an older wipnote writer (pid %d) holds this project's legacy lease; "+
			"it is left running. Stop it to let this version start its own.", state.LegacyPID)
	case state.Record != nil && versionOlder(state.Record.Version, selfVersion):
		return fmt.Sprintf("wipnote: this project's writer daemon runs an older wipnote (%s, pid %d); "+
			"it is left running until it idles out.", state.Record.Version, state.Record.PID)
	}
	return ""
}

// versionOlder reports whether release version a is older than b. A version
// that is not dotted numbers ("dev", "") compares as neither older nor newer.
func versionOlder(a, b string) bool {
	va, okA := parseVersion(a)
	vb, okB := parseVersion(b)
	if !okA || !okB {
		return false
	}
	for i := range va {
		if va[i] != vb[i] {
			return va[i] < vb[i]
		}
	}
	return false
}

// parseVersion reads "v1.2.3", "1.2" or "1.2.3-rc1" as three numbers.
func parseVersion(v string) ([3]int, bool) {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+ "); i >= 0 {
		v = v[:i]
	}
	parts := strings.Split(v, ".")
	if v == "" || len(parts) > 3 {
		return out, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil || n < 0 {
			return out, false
		}
		out[i] = n
	}
	return out, true
}
