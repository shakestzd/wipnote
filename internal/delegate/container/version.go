package container

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/version"
)

// MinWorkerVersion is the first wipnote that writes a container worker's
// hook events as plain files in the run folder (bug-6715affb). An older
// wipnote inside the container loses them across the workspace mount.
const MinWorkerVersion = "0.85.0"

// insideVersion pulls the version out of `wipnote version` output
// ("wipnote 0.71.0 (go)"); "" when it is not a release version.
func insideVersion(out string) string {
	f := strings.Fields(firstLine(out))
	if len(f) < 2 || !version.Parseable(f[1]) {
		return ""
	}
	return strings.TrimPrefix(f[1], "v")
}

// versionTooOld names why the container's wipnote cannot serve a worker, or
// "" when it can. An unreadable version (a dev build) is not judged.
func versionTooOld(root, inside, host string) string {
	if inside == "" {
		return ""
	}
	fix := ": run " + CLI + " exec --workspace-folder " + root + " wipnote upgrade"
	switch {
	case version.Parseable(host) && version.IsNewer(host, inside):
		return fmt.Sprintf("wipnote %s inside, %s on the host%s", inside, host, fix)
	case version.IsNewer(MinWorkerVersion, inside):
		return fmt.Sprintf("wipnote %s inside, older than %s, the first to write worker events as plain files (host: %s)%s", inside, MinWorkerVersion, hostLabel(host), fix)
	}
	return ""
}

func hostLabel(host string) string {
	if host == "" {
		return "unknown"
	}
	return host
}

// versionCheck is the "wipnote inside" result for `wipnote version` output.
func versionCheck(root, out, host string) codex.Check {
	c := codex.Check{Name: "wipnote inside", Status: codex.CheckPass}
	c.Detail = firstLine(out) + fmt.Sprintf(" (host %s)", hostLabel(host))
	if msg := versionTooOld(root, insideVersion(out), host); msg != "" {
		c.Status, c.Detail = codex.CheckFail, msg
	}
	return c
}
