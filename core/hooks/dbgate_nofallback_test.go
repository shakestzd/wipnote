package hooks

import (
	"database/sql"
	"strings"
	"sync"
	"testing"
)

// daemonCapture stubs both daemon seams as a daemon MISS (every route returns
// false) and records each statement offered to the daemon.
type daemonCapture struct {
	mu    sync.Mutex
	stmts []string
}

func stubDaemonMiss(t *testing.T) *daemonCapture {
	t.Helper()
	c := &daemonCapture{}
	record := func(_ string, sqlStmt string, _ ...any) bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		c.stmts = append(c.stmts, sqlStmt)
		return false
	}
	prevAsync, prevApplied := routeSQLAsync, routeSQLApplied
	t.Cleanup(func() { routeSQLAsync, routeSQLApplied = prevAsync, prevApplied })
	routeSQLAsync, routeSQLApplied = record, record
	return c
}

func (c *daemonCapture) offered(fragment string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, s := range c.stmts {
		if strings.Contains(s, fragment) {
			return true
		}
	}
	return false
}

func countRows(t *testing.T, database *sql.DB, query string, args ...any) int {
	t.Helper()
	var n int
	if err := database.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("%s: %v", query, err)
	}
	return n
}

// TestDaemonMiss_HookWritesNeverReachTheFileDB pins the no-fallback contract
// (feat-174dc958): a hook write is offered to the writer daemon, and when the
// daemon is not there the write is dropped. Nothing is written to the
// per-project file database in its place.
func TestDaemonMiss_HookWritesNeverReachTheFileDB(t *testing.T) {
	clearNestedSessionEnv(t)
	unsetenv(t, "WIPNOTE_PROJECT_DIR")
	unsetenv(t, "CLAUDE_PROJECT_DIR")
	projectDir := t.TempDir()
	database, err := openWipnoteTestDB(t, projectDir)
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()
	capture := stubDaemonMiss(t)

	const sid = "sess-nofallback-0001"
	if _, err := SessionStart(&CloudEvent{SessionID: sid, CWD: projectDir}, database, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	sub := &CloudEvent{SessionID: sid, CWD: projectDir, AgentID: "agent-nofallback-01", AgentType: "patch-coder"}
	if _, err := SubagentStart(sub, database); err != nil {
		t.Fatalf("SubagentStart: %v", err)
	}

	for _, frag := range []string{"INSERT OR IGNORE INTO sessions", "INSERT INTO agent_events",
		"session_family_id", "pending_subagent_starts"} {
		if !capture.offered(frag) {
			t.Errorf("no statement containing %q was offered to the daemon", frag)
		}
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM sessions`); n != 0 {
		t.Errorf("file db holds %d sessions rows after a daemon miss, want 0", n)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM agent_events`); n != 0 {
		t.Errorf("file db holds %d agent_events rows after a daemon miss, want 0", n)
	}
	if n := countRows(t, database, `SELECT COUNT(*) FROM pending_subagent_starts`); n != 0 {
		t.Errorf("file db holds %d pending_subagent_starts rows after a daemon miss, want 0", n)
	}
}
