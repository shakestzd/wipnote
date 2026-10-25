package hooks

import (
	"encoding/json"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/ingest"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/provenance"
)

// SessionEvent holds the data needed to write a <li> element to a session's
// HTML activity log. Kept minimal — only fields that appear in the HTML output.
// Status and Reason are optional and emit data-status/data-reason attributes
// when set; they are used by the orphan sweep to write synthetic aborted
// entries while leaving the live PostToolUse append path unchanged.
type SessionEvent struct {
	Timestamp time.Time
	ToolName  string
	Success   bool
	EventID   string
	FeatureID string
	Summary   string
	Status    string
	Reason    string
}

// CreateSessionHTML writes the initial session HTML file to
// .wipnote/sessions/{session-id}.html. It creates the sessions directory if
// needed. Errors are silently logged via debugLog — HTML is non-critical.
func CreateSessionHTML(projectDir string, s *models.Session) {
	sessDir := filepath.Join(projectDir, ".wipnote", "sessions")
	if err := os.MkdirAll(sessDir, 0o755); err != nil {
		debugLog(projectDir, "[session-html] mkdir sessions: %v", err)
		return
	}

	content := renderInitialSessionHTML(projectDir, s)
	htmlPath := filepath.Join(sessDir, s.SessionID+".html")
	writeInitialSessionHTML(projectDir, htmlPath, s.SessionID, content)
}

// renderInitialSessionHTML renders the empty, active session HTML shell.
func renderInitialSessionHTML(projectDir string, s *models.Session) string {
	// data-project-dir is the canonical attribution for the session and
	// must reflect the TRANSCRIPT'S source project, not the destination
	// repo that the HTML is being written into. RenderIngestedSessionHTML
	// sets s.ProjectDir to the decoded source path before calling here,
	// so prefer that over the function argument when set. The function
	// argument is only used as a fallback for the live hook path where
	// projectDir IS the source project. (bug found in roborev review of
	// d2731bec — without this guard, ingested sessions would canonicalize
	// the wrong project_dir and reindex would propagate it forward.)
	//
	// Normalize to canonical root for local sessions (e.g. "."); mark with
	// "unresolved:" prefix for sessions ingested from foreign machines where
	// the canonical root differs from the local repo, preserving the foreign
	// origin so it remains queryable without silently mangling the path.
	raw := s.ProjectDir
	if raw == "" {
		raw = projectDir
	}
	canonicalProjectDir := paths.NormalizeProjectDir(raw)

	isSubagent := "false"
	if s.IsSubagent {
		isSubagent = "true"
	}

	startedAt := s.CreatedAt.UTC().Format(time.RFC3339)

	var b strings.Builder
	writeSessionArticleOpen(&b, s, canonicalProjectDir, isSubagent, startedAt)
	writeSessionArticleBody(&b, s, startedAt)
	return b.String()
}

// writeSessionArticleOpen writes the document head and the session
// <article> opening tag with its data attributes.
func writeSessionArticleOpen(b *strings.Builder, s *models.Session, canonicalProjectDir, isSubagent, startedAt string) {
	b.WriteString(`<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <meta name="wipnote-version" content="1.0">
    <title>Session `)
	b.WriteString(html.EscapeString(startedAt))
	b.WriteString(`</title>
    <link rel="stylesheet" href="../styles.css">
</head>
<body>
    <article id="`)
	b.WriteString(html.EscapeString(s.SessionID))
	b.WriteString(`"
             data-type="session"
             data-status="active"
             data-agent="`)
	b.WriteString(html.EscapeString(s.AgentAssigned))
	b.WriteString(`"
             data-project-dir="`)
	b.WriteString(html.EscapeString(canonicalProjectDir))
	b.WriteString(`"
             data-started-at="`)
	b.WriteString(html.EscapeString(startedAt))
	b.WriteString(`"
             data-event-count="0"
             data-is-subagent="`)
	b.WriteString(isSubagent)
	b.WriteString(`"
             data-start-commit="`)
	b.WriteString(html.EscapeString(s.StartCommit))
	b.WriteString(`"`)
	if s.ActiveFeatureID != "" {
		b.WriteString(`
             data-active-feature-id="`)
		b.WriteString(html.EscapeString(s.ActiveFeatureID))
		b.WriteString(`"`)
	}
	// Provenance attributes — emitted only when populated so older sessions
	// continue to round-trip with the same byte shape.
	prov := provenance.Provenance{
		Agent:      s.CreatedByAgent,
		Model:      s.CreatedByModel,
		Role:       s.CreatedByRole,
		CLIVersion: s.CreatedByCLIVersion,
	}
	b.WriteString(prov.HTMLAttrs())
}

// writeSessionArticleBody writes the session header, empty edge nav and
// activity log, and closes the document.
func writeSessionArticleBody(b *strings.Builder, s *models.Session, startedAt string) {
	b.WriteString(`>

        <header>
            <h1>Session `)
	b.WriteString(html.EscapeString(startedAt))
	b.WriteString(`</h1>
            <div class="metadata">
                <span class="badge status-active">Active</span>
                <span class="badge">`)
	b.WriteString(html.EscapeString(s.AgentAssigned))
	b.WriteString(`</span>
                <span class="badge">0 events</span>
            </div>
        </header>

        <nav data-graph-edges>
        </nav>

        <section data-activity-log>
            <ol reversed>
            </ol>
        </section>
    </article>
</body>
</html>
`)
}

// writeInitialSessionHTML writes content to htmlPath under an exclusive lock.
func writeInitialSessionHTML(projectDir, htmlPath, sessionID, content string) {
	// Guard: do not clobber an existing session HTML that is already
	// populated or belongs to a different session. Only write when:
	//   (a) the file does not yet exist, or
	//   (b) the file exists, encodes the SAME session ID, AND is an empty
	//       shell (data-event-count=0 and not completed/aborted) — i.e. a
	//       safe same-session re-initialisation before any events have been
	//       recorded.
	// Cases that MUST NOT overwrite:
	//   • different session ID on disk (UUID collision guard — unchanged)
	//   • same session ID but already has recorded events (data-event-count>0)
	//   • same session ID but already finalized (status != "active")
	// This fixes the SessionStart REPLAY / resume / re-init data-loss bug
	// (roborev job 272, commit 2d54416ed): the previous guard only checked
	// for a different id, so a same-session replay could reset a 732-event
	// session back to an empty shell.
	f, err := os.OpenFile(htmlPath, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		debugLog(projectDir, "[session-html] open %s: %v", htmlPath, err)
		return
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		debugLog(projectDir, "[session-html] flock %s: %v", htmlPath, err)
		return
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN) //nolint:errcheck

	// Read under the lock so the guard and the write see the same file.
	if existing, readErr := io.ReadAll(f); readErr == nil && len(existing) > 0 {
		if !sessionHTMLMayOverwrite(projectDir, htmlPath, sessionID, string(existing)) {
			return
		}
	}
	if err := rewriteLocked(f, content); err != nil {
		debugLog(projectDir, "[session-html] write %s: %v", htmlPath, err)
	}
}

// rewriteLocked replaces the contents of f, which the caller has flocked.
func rewriteLocked(f *os.File, content string) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.Seek(0, 0); err != nil {
		return err
	}
	_, err := f.WriteString(content)
	return err
}

// sessionHTMLMayOverwrite applies the CreateSessionHTML clobber guard to the
// existing file content. Only a same-session, empty, still-active shell may be
// re-initialised: a different session id (UUID collision), recorded events, or
// a finalized status all keep the file as is.
//
// data-event-count is only updated by FinalizeSessionHTML, so population is
// detected by counting <li> entries too (roborev job 272, commit 2d54416ed).
func sessionHTMLMayOverwrite(projectDir, htmlPath, sessionID, existing string) bool {
	if id := extractArticleID(existing); id != "" && id != sessionID {
		debugLog(projectDir, "[session-html] skipping write: %s already belongs to session %q (new=%q)", htmlPath, id, sessionID)
		return false
	}
	if extractEventCount(existing) > 0 || countActivityEntries(existing) > 0 {
		debugLog(projectDir, "[session-html] skipping write: %s has recorded events (same session %q)", htmlPath, sessionID)
		return false
	}
	if st := extractArticleStatus(existing); st != "" && st != "active" {
		debugLog(projectDir, "[session-html] skipping write: %s is already %q (same session %q)", htmlPath, st, sessionID)
		return false
	}
	return true
}

// extractArticleID extracts the id="..." value from the first <article id="...">
// element in an HTML string. Returns "" when not found. Used by CreateSessionHTML
// to detect UUID collisions before overwriting an existing session file.
func extractArticleID(content string) string {
	const needle = `<article id="`
	idx := strings.Index(content, needle)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// extractEventCount returns the integer value of data-event-count="..." from
// the session HTML content. Returns 0 when the attribute is absent or cannot
// be parsed — which is the safe default (treat as empty shell).
func extractEventCount(content string) int {
	const needle = `data-event-count="`
	idx := strings.Index(content, needle)
	if idx < 0 {
		return 0
	}
	rest := content[idx+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return 0
	}
	n := 0
	for _, ch := range rest[:end] {
		if ch < '0' || ch > '9' {
			return 0
		}
		n = n*10 + int(ch-'0')
	}
	return n
}

// extractArticleStatus returns the value of data-status="..." from the session
// HTML content. Returns "" when the attribute is absent.
func extractArticleStatus(content string) string {
	const needle = `data-status="`
	idx := strings.Index(content, needle)
	if idx < 0 {
		return ""
	}
	rest := content[idx+len(needle):]
	end := strings.IndexByte(rest, '"')
	if end < 0 {
		return ""
	}
	return rest[:end]
}

// RenderIngestedSessionHTML writes a complete session HTML file for an
// ingested Claude Code transcript. It reuses CreateSessionHTML to scaffold
// the file and AppendEventToSessionHTML (via the flock path) for each
// tool call, so the resulting file is byte-for-byte compatible with a
// live-hook-written session and round-trips through reindexSessions.
//
// wipnoteDir is the .wipnote directory of the project receiving the
// ingest. sessionSourceDir is the decoded Claude-projects project directory
// the session originated from and is recorded on the session model for
// display scoping. The render is a no-op when the target HTML file already
// exists unless force is true — live hook writes are authoritative and
// ingest must not clobber them.
func RenderIngestedSessionHTML(wipnoteDir, sessionID, sessionSourceDir string, result *ingest.ParseResult, force bool) error {
	parentDir := filepath.Dir(wipnoteDir)
	htmlPath := filepath.Join(wipnoteDir, "sessions", sessionID+".html")
	if !force {
		if _, err := os.Stat(htmlPath); err == nil {
			return nil
		}
	} else if _, err := os.Stat(htmlPath); err == nil {
		if rmErr := os.Remove(htmlPath); rmErr != nil {
			return fmt.Errorf("remove existing %s: %w", htmlPath, rmErr)
		}
	}

	createdAt := time.Now().UTC()
	if len(result.Messages) > 0 {
		createdAt = result.Messages[0].Timestamp.UTC()
	}

	s := &models.Session{
		SessionID:     sessionID,
		AgentAssigned: "claude-code",
		CreatedAt:     createdAt,
		Status:        "active",
		Model:         result.Model,
		ProjectDir:    sessionSourceDir,
	}
	CreateSessionHTML(parentDir, s)

	if _, err := os.Stat(htmlPath); err != nil {
		return fmt.Errorf("CreateSessionHTML did not produce %s: %w", htmlPath, err)
	}

	msgTimestamps := make(map[int]time.Time, len(result.Messages))
	for _, m := range result.Messages {
		msgTimestamps[m.Ordinal] = m.Timestamp
	}

	endedAt := createdAt
	for i, tc := range result.ToolCalls {
		ts := createdAt
		if t, ok := msgTimestamps[tc.MessageOrdinal]; ok {
			ts = t.UTC()
		}
		if ts.After(endedAt) {
			endedAt = ts
		}

		var inputMap map[string]any
		if tc.InputJSON != "" {
			_ = json.Unmarshal([]byte(tc.InputJSON), &inputMap)
		}

		AppendEventToSessionHTML(parentDir, sessionID, SessionEvent{
			Timestamp: ts,
			ToolName:  tc.ToolName,
			Success:   true,
			EventID:   ingest.EventID(sessionID, tc.ToolUseID, tc.ToolName, i),
			FeatureID: tc.FeatureID,
			Summary:   SummariseInput(tc.ToolName, inputMap),
		})
	}

	FinalizeSessionHTML(parentDir, sessionID,
		endedAt.Format(time.RFC3339), "completed", len(result.ToolCalls))

	return nil
}
