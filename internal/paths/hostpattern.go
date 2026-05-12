package paths

import "regexp"

// HostPathPattern matches absolute paths that are specific to a developer's
// machine and therefore must not appear in committed work-item artifacts or
// in stored CloudEvent payloads:
//
//   - /Users/<name>/        — macOS home directories
//   - /home/<name>/         — Linux home directories
//   - /workspaces/<name>/   — GitHub Codespaces per-user workspace paths
//   - /private/var/folders/ — macOS temp directory (always machine-specific)
//
// /home/runner/ (GitHub Actions CI) is allowed via a separate filter in the
// precommit gate (see cmd/wipnote/check_host_paths.go: ciAllowPattern).
//
// This pattern is the single source of truth shared by:
//   - the precommit gate (cmd/wipnote/check_host_paths.go), and
//   - the runtime normalizer (NormalizeToRepoRelative) which marks
//     outside-repo absolute paths as "unresolved:" so the downstream
//     migration rewriter can repair them later.
//
// Any future expansion (e.g. /Volumes/, C:\Users\) must be reflected in BOTH
// hostpattern_test.go and the precommit-gate tests.
var HostPathPattern = regexp.MustCompile(
	`/Users/[^/\s]+/` +
		`|/home/[^/\s]+/` +
		`|/workspaces/[^/\s]+/` +
		`|/private/var/folders/`,
)
