package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// inheritedParentSessionID is a session that started the environment this
// session inherited — the herdr-server case (bug-5333ea49): a pane spawned by a
// long-lived server carries WIPNOTE_PARENT_SESSION from whichever session
// started that server.
const inheritedParentSessionID = "99998888-7777-6666-5555-444433332222"

func readSessionHTML(t *testing.T, projectDir, sessionID string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatalf("read session html: %v", err)
	}
	return string(b)
}

// TestSessionStartInheritedParentAtDepthZeroIsRootWithLineage: a launcher
// marks its session root with WIPNOTE_NESTING_DEPTH=0. An inherited parent is
// then lineage only — the session keeps its own claude ledger row, and the
// parent id is recorded on the session record, not dropped.
func TestSessionStartInheritedParentAtDepthZeroIsRootWithLineage(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("WIPNOTE_PARENT_SESSION", inheritedParentSessionID)
	t.Setenv("WIPNOTE_NESTING_DEPTH", "0")

	event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
	if _, err := SessionStart(event, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}

	recs := ledgerRows(t, projectDir)
	if len(recs) != 1 || recs[0].SessionID != ledgerTestSessionID {
		t.Fatalf("got ledger rows %+v, want exactly one row for %s", recs, ledgerTestSessionID)
	}
	if recs[0].Harness != "claude" {
		t.Errorf("harness = %q, want claude", recs[0].Harness)
	}
	page := readSessionHTML(t, projectDir, ledgerTestSessionID)
	if !strings.Contains(page, `data-is-subagent="false"`) {
		t.Error("a launcher-started session must be recorded as a root session")
	}
	if !strings.Contains(page, `data-parent-session-id="`+inheritedParentSessionID+`"`) {
		t.Errorf("the inherited parent was not recorded as lineage on the session record")
	}
}

// TestSessionStartWithoutParentRecordsNoLineage is the other side: no
// inherited parent, no lineage attribute, and a session whose parent variable
// is its own id (the CLAUDE_ENV_FILE export) is not its own parent.
func TestSessionStartWithoutParentRecordsNoLineage(t *testing.T) {
	for _, parent := range []string{"", ledgerTestSessionID} {
		projectDir := newLedgerHookProject(t)
		t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
		t.Setenv("WIPNOTE_PARENT_SESSION", parent)
		t.Setenv("WIPNOTE_NESTING_DEPTH", "0")

		event := &CloudEvent{SessionID: ledgerTestSessionID, CWD: projectDir}
		if _, err := SessionStart(event, projectDir); err != nil {
			t.Fatalf("SessionStart: %v", err)
		}
		if page := readSessionHTML(t, projectDir, ledgerTestSessionID); strings.Contains(page, "data-parent-session-id") {
			t.Errorf("parent %q: session record carries a parent link it does not have", parent)
		}
	}
}

// TestSessionStartRealSubagentStillGetsNoRootRow: a real subagent is known by
// its own agent_id in the payload, not by the inherited variable. Even under a
// launcher environment that marks depth 0, it must not get a root ledger row.
func TestSessionStartRealSubagentStillGetsNoRootRow(t *testing.T) {
	projectDir := newLedgerHookProject(t)
	t.Setenv("CLAUDE_CODE_ENTRYPOINT", "cli")
	t.Setenv("WIPNOTE_PARENT_SESSION", inheritedParentSessionID)
	t.Setenv("WIPNOTE_NESTING_DEPTH", "0")

	event := &CloudEvent{SessionID: ledgerTestSessionID, AgentID: "a1b2c3d4e5f6", CWD: projectDir}
	if _, err := SessionStart(event, projectDir); err != nil {
		t.Fatalf("SessionStart: %v", err)
	}
	if recs := ledgerRows(t, projectDir); len(recs) != 0 {
		t.Errorf("a real subagent got %d root ledger row(s), want 0", len(recs))
	}
	if page := readSessionHTML(t, projectDir, ledgerTestSessionID); !strings.Contains(page, `data-is-subagent="true"`) {
		t.Error("a real subagent must still be recorded as a subagent")
	}
}
