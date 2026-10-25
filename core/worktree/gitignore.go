package worktree

import (
	"os"
	"path/filepath"
	"strings"
)

// wipnoteGitignoreContent is the managed policy block written to
// .wipnote/.gitignore. Patterns are relative to .wipnote/ (nested gitignore).
//
// Design: a missing file is written fresh. An existing file (the user may have
// edited it) is never rewritten: its lines stay as they are, and only managed
// patterns it lacks are appended under one marker comment (upgradeGitignore).
// That lets a pattern added in a later wipnote release, such as delegate/
// (plan-28b4a7f6 slice 5), reach projects whose file an older release wrote.
// A pattern already present anywhere in the file is not added again, so a
// second call changes nothing.
//
// TRACKED canonical content is NOT ignored here: features/, bugs/, tracks/,
// plans/, *.html work items, and project config/metadata must remain visible
// to git. sessions/ being ignored already covers session HTML, events.ndjson,
// state.json, .index-offset, .collector-pid under sessions/<id>/ — the **/
// patterns are belt-and-suspenders for any such markers outside sessions/.
//
// .index-offset carries a trailing * because the marker is written
// temp-then-rename, and a crash between the two leaves a .index-offset-tmp-*
// file behind (observe/otel/indexer/progress_marker.go). The exact-name
// pattern matched the marker but not its debris, so leftovers surfaced as
// untracked noise in a repo that already fights that.
const wipnoteGitignoreContent = `# Managed by wipnote — do not edit this block manually.
# Patterns are relative to .wipnote/ (this is a nested .gitignore).
# See: https://git-scm.com/docs/gitignore

# Runtime/session directories
sessions/
events/
logs/

# Live delegation runs, events and mailboxes (only ledger rows are committed)
delegate/

# Database and index artifacts
*.db
*.db-*
*.sqlite*
*.bloom
archive-index/

# Obsolete analytics artifacts
cigs/

# Runtime telemetry files
**/*.jsonl
**/*.log
**/*.lock
.active-session
.launch-mode
.otel-notice-shown
.session-warning-state.json
.session-families.lock
session-families.json
parent-activity.json
.error-spikes.json
active-auto-spikes.json
drift-queue.json
.otlp-port
.serve.lock

# Migration markers
migrations/*.done

# Process/collector pid and offset markers
**/*.pid
**/*.sock
**/.index-offset*
**/.collector-pid
**/state.json
`

// upgradeMarker heads the lines the upgrader appends to an existing file.
const upgradeMarker = "# Added by wipnote: managed patterns missing from this file."

// EnsureWipnoteGitignore ensures that .wipnote/.gitignore holds the wipnote
// runtime-artifact policy. A missing file is written whole; an existing one
// gains only the managed patterns it lacks (see upgradeGitignore). It is
// idempotent. If writing fails, the error is silently swallowed — a missing
// .gitignore is undesirable but must never block a session launch.
//
// Wire this at any .wipnote bootstrap seam: session-start hooks and the
// family-lock path both call it so all harnesses (Claude, Codex, Gemini)
// receive the file on their first session after upgrading.
func EnsureWipnoteGitignore(projectDir string) {
	if projectDir == "" {
		return
	}
	dir := filepath.Join(projectDir, ".wipnote")
	// Ensure the directory exists (best-effort; callers may have already done this).
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	target := filepath.Join(dir, ".gitignore")
	// O_CREATE|O_EXCL fails if the file exists: then upgrade it in place.
	f, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		if os.IsExist(err) {
			upgradeGitignore(target)
		}
		return
	}
	defer f.Close()
	_, _ = f.WriteString(wipnoteGitignoreContent)
}

// upgradeGitignore appends to an existing file every managed pattern line it
// does not already hold, under upgradeMarker. Comments and blank lines of the
// managed block are not copied. Existing lines are never changed or removed,
// and a symlinked file is left alone.
func upgradeGitignore(target string) {
	if fi, err := os.Lstat(target); err != nil || !fi.Mode().IsRegular() {
		return
	}
	data, err := os.ReadFile(target)
	if err != nil {
		return
	}
	missing := missingPatterns(string(data))
	if len(missing) == 0 {
		return
	}
	var b strings.Builder
	if len(data) > 0 && !strings.HasSuffix(string(data), "\n") {
		b.WriteString("\n")
	}
	b.WriteString("\n" + upgradeMarker + "\n")
	for _, p := range missing {
		b.WriteString(p + "\n")
	}
	f, err := os.OpenFile(target, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(b.String())
}

// missingPatterns returns the managed pattern lines absent from existing, in
// the managed block's order.
func missingPatterns(existing string) []string {
	have := map[string]bool{}
	for _, l := range strings.Split(existing, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var out []string
	for _, l := range strings.Split(wipnoteGitignoreContent, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "#") || have[l] {
			continue
		}
		have[l] = true
		out = append(out, l)
	}
	return out
}
