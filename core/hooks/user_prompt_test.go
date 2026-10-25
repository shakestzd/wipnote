package hooks

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// setupTestDB creates a per-test on-disk SQLite DB with schema and a session.
// Each call gets its own isolated database to prevent UNIQUE constraint
// violations when tests share the same in-memory connection cache.
// Also clears the featureIDCache to prevent cross-test pollution.
func setupTestDB(t *testing.T) *testDB {
	t.Helper()

	// Hooks route derived-index writes to the writer daemon and have no
	// fallback (feat-174dc958). These tests read back what a hook wrote, so a
	// stub daemon applies every routed statement to this database.
	t.Setenv("WIPNOTE_NO_AUTO_WRITER", "1")

	// Reset the global feature ID cache to prevent interference between tests
	// (each test may use the same session ID "test-sess" but with different
	// active features or no active feature).

	dbPath := filepath.Join(t.TempDir(), "wipnote.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)
	database, err := db.Open(dbPath)
	if err != nil {
		t.Fatalf("db.Open: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	now := time.Now().UTC()

	sess := &models.Session{
		SessionID:     "test-sess",
		AgentAssigned: "claude-code",
		CreatedAt:     now,
		Status:        "active",
		Model:         "sonnet-4",
	}
	if err := db.UpsertSession(database, sess); err != nil {
		t.Fatalf("UpsertSession: %v", err)
	}

	return &testDB{DB: database, now: now, t: t}
}

type testDB struct {
	DB  *sql.DB
	now time.Time
	t   *testing.T
}

func (td *testDB) addTrack(id, title string) {
	td.t.Helper()
	now := td.now.Format(time.RFC3339)
	_, err := td.DB.Exec(
		`INSERT INTO tracks (id, title, status, created_at, updated_at) VALUES (?,?,?,?,?)`,
		id, title, "active", now, now,
	)
	if err != nil {
		td.t.Fatalf("insert track: %v", err)
	}
}

func (td *testDB) addFeature(id, ftype, title, status string) {
	td.t.Helper()
	feat := &db.Feature{
		ID:        id,
		Type:      ftype,
		Title:     title,
		Status:    status,
		Priority:  "medium",
		CreatedAt: td.now,
		UpdatedAt: td.now,
	}
	if err := db.UpsertFeature(td.DB, feat); err != nil {
		td.t.Fatalf("UpsertFeature(%s): %v", id, err)
	}
}

// addSession inserts a bare session row, optionally linked to a parent and an
// active feature. Empty parentID / activeFeatureID are stored as NULL.
func (td *testDB) addSession(id, parentID, activeFeatureID string) {
	td.t.Helper()
	var parent, feat interface{}
	if parentID != "" {
		parent = parentID
	}
	if activeFeatureID != "" {
		feat = activeFeatureID
	}
	_, err := td.DB.Exec(
		`INSERT INTO sessions (session_id, agent_assigned, parent_session_id, active_feature_id, created_at, status)
		 VALUES (?,?,?,?,?,?)`,
		id, "claude-code", parent, feat, td.now.Format(time.RFC3339), "active",
	)
	if err != nil {
		td.t.Fatalf("addSession(%s): %v", id, err)
	}
}

func TestUserPrompt_EmptyPrompt(t *testing.T) {
	td := setupTestDB(t)
	defer td.DB.Close()

	os.Setenv("WIPNOTE_SESSION_ID", "test-sess")
	defer os.Unsetenv("WIPNOTE_SESSION_ID")

	event := &CloudEvent{SessionID: "test-sess", Prompt: ""}
	result, err := UserPrompt(event)
	if err != nil {
		t.Fatalf("UserPrompt: %v", err)
	}
	if !result.Continue {
		t.Error("expected Continue=true for empty prompt")
	}
}

func TestUserPrompt_NoActiveNoOneliner(t *testing.T) {
	td := setupTestDB(t)
	defer td.DB.Close()

	os.Setenv("WIPNOTE_SESSION_ID", "test-sess")
	defer os.Unsetenv("WIPNOTE_SESSION_ID")

	td.addFeature("feat-aaa", "feature", "Auth System", "in-progress")
	// Don't set active feature
	event := &CloudEvent{SessionID: "test-sess", Prompt: "can you investigate the performance bottleneck?"}
	result, err := UserPrompt(event)
	if err != nil {
		t.Fatalf("UserPrompt: %v", err)
	}

	// When no active item, the ACTIVE line should not appear in additionalContext
	if result.AdditionalContext != "" && strings.Contains(result.AdditionalContext, "ACTIVE:") {
		t.Errorf("UserPrompt should not have ACTIVE line when no active feature, got: %s", result.AdditionalContext)
	}
	// With no active item there is nothing to add to the turn.
	if result.AdditionalContext != "" || !result.Continue {
		t.Errorf("want empty context and Continue=true, got: %+v", result)
	}
}

func TestBuildActiveItemOneLiner_WithoutTitle(t *testing.T) {
	td := setupTestDB(t)
	defer td.DB.Close()

	td.addFeature("feat-001", "feature", "", "in-progress")
	hint := buildActiveItemOneLiner("", "feat-001")

	expected := "ACTIVE: feat-001"
	if hint != expected {
		t.Errorf("expected %q, got %q", expected, hint)
	}
}

func TestBuildActiveItemOneLiner_NotFound(t *testing.T) {
	td := setupTestDB(t)
	defer td.DB.Close()

	hint := buildActiveItemOneLiner("", "nonexistent-id")
	expected := "ACTIVE: nonexistent-id"
	if hint != expected {
		t.Errorf("expected %q, got %q", expected, hint)
	}
}

func TestBuildActiveItemOneLiner_Empty(t *testing.T) {
	td := setupTestDB(t)
	defer td.DB.Close()

	hint := buildActiveItemOneLiner("", "")
	if hint != "" {
		t.Errorf("expected empty string for empty ID, got %q", hint)
	}
}

// TestUserPrompt_PerTurnContextCases pins the per-turn context
// (feat-174dc958): it is exactly the active-item line, built from the
// session's open claim in the canonical claim ledger and the item's canonical
// title. With no claim, or a prompt that is only a system block, there is no
// context at all.
func TestUserPrompt_PerTurnContextCases(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee00006666"
	for _, tc := range []struct {
		name, prompt string
		claim        bool
		want         string
	}{
		{"open claim", "implement the caching layer and search the codebase", true, "ACTIVE: spk-0a0b0c0d — Research caching"},
		{"no claim", "implement the caching layer", false, ""},
		{"system block only", "<system-reminder>internal</system-reminder>", true, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := taskClaimProject(t, sid, false)
			writeCanonicalItem(t, projectDir, "spikes", "spk-0a0b0c0d", "spike", "in-progress", "Research caching")
			if tc.claim {
				openCanonicalClaim(t, filepath.Join(projectDir, ".wipnote"), sid, sid, "claude-code", "spk-0a0b0c0d")
			}
			res, err := UserPrompt(&CloudEvent{SessionID: sid, CWD: projectDir, Prompt: tc.prompt})
			if err != nil {
				t.Fatalf("UserPrompt: %v", err)
			}
			if res.AdditionalContext != tc.want {
				t.Errorf("context = %q, want %q", res.AdditionalContext, tc.want)
			}
		})
	}
}
