package hooks

import (
	"database/sql"
	"html"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"
)

// openWipnoteTestDB opens a per-test SQLite DB and pins WIPNOTE_DB_PATH to
// it. Hooks no longer read or write any database (feat-174dc958); tests that
// still pass this handle prove the hook ignores it.
func openWipnoteTestDB(t *testing.T, projectDir string) (*sql.DB, error) {
	t.Helper()
	dbPath := filepath.Join(projectDir, ".wipnote", "wipnote.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)
	return dbtest.Open(dbPath)
}

// clearNestedSessionEnv unsets the env vars that, when leaked from a nested
// Claude Code session, would change the resolved session ID or mark the hook as
// a subagent — making the session-start writes land under the wrong ID and the
// read-backs return no rows (see MEMORY: nested-session-hooks-test).
func clearNestedSessionEnv(t *testing.T) {
	t.Helper()
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("CLAUDE_CODE_SESSION_ID", "")
	t.Setenv("WIPNOTE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Setenv("WIPNOTE_NESTING_DEPTH", "")
	t.Setenv("CLAUDE_ENV_FILE", "")
	t.Setenv("WIPNOTE_SESSION_FAMILY_ID", "")
}

// htmlSessionRecord is what SessionStart records about a session in its
// canonical session HTML.
type htmlSessionRecord struct {
	ProjectDir string
	Agent      string
}

// sessionHTMLRecord reads the session HTML SessionStart wrote under
// projectDir. Hooks keep no session row anywhere else (feat-174dc958).
func sessionHTMLRecord(t *testing.T, projectDir, sessionID string) htmlSessionRecord {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatalf("read session html: %v", err)
	}
	attr := func(name string) string {
		m := regexp.MustCompile(name + `="([^"]*)"`).FindSubmatch(raw)
		if m == nil {
			return ""
		}
		return html.UnescapeString(string(m[1]))
	}
	return htmlSessionRecord{ProjectDir: attr("data-project-dir"), Agent: attr("data-agent")}
}
