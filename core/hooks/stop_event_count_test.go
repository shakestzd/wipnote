package hooks

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// TestStop_EventCountFromCanonicalToolLog pins where Stop's session event-count
// badge comes from: the number of events in the session's canonical
// tool-events.ndjson log, not agent_events rows in the hook's database handle
// (feat-af8d1689, owner decision 2026-10-25). The handle holds 5 rows and the
// log holds 3 events; the badge must say 3.
func TestStop_EventCountFromCanonicalToolLog(t *testing.T) {
	const sessionID = "019ee380-abcd-7000-8000-00000000c0de"
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote", "sessions"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv("WIPNOTE_SESSION_ID", sessionID)
	t.Setenv("CLAUDE_SESSION_ID", "")
	t.Setenv("WIPNOTE_PARENT_SESSION", "")
	t.Chdir(projectDir)

	database, err := db.OpenEphemeralProjection()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	sess := &models.Session{SessionID: sessionID, AgentAssigned: "claude-code", Status: "active", CreatedAt: time.Now().UTC()}
	if err := db.UpsertSession(database, sess); err != nil {
		t.Fatal(err)
	}
	for i := range 5 {
		now := time.Now().UTC()
		ev := &models.AgentEvent{EventID: "evt-db-" + string(rune('a'+i)), AgentID: "claude-code",
			EventType: models.EventToolCall, Timestamp: now, ToolName: "Bash", SessionID: sessionID,
			Status: "completed", Source: "hook", CreatedAt: now, UpdatedAt: now}
		if err := db.InsertEvent(database, ev); err != nil {
			t.Fatal(err)
		}
	}
	CreateSessionHTML(projectDir, sess)
	for _, tool := range []string{"Read", "Bash", "Edit"} {
		appendCanonicalToolEvent(projectDir, sessionID, canonicalToolEvent{Tool: tool, Summary: "x"})
	}

	if _, err := Stop(&CloudEvent{SessionID: sessionID, CWD: projectDir}, database); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	raw, err := os.ReadFile(filepath.Join(projectDir, ".wipnote", "sessions", sessionID+".html"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), `data-event-count="3"`) {
		t.Fatalf("want data-event-count=\"3\" (canonical log), got:\n%s", eventCountAttr(string(raw)))
	}
}

func eventCountAttr(html string) string {
	i := strings.Index(html, "data-event-count=")
	if i < 0 {
		return "(no data-event-count attribute)"
	}
	end := min(i+30, len(html))
	return html[i:end]
}
