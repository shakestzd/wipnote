package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/models"
)

// insertParallelSession inserts a session for parallel-session grouping tests.
func insertParallelSession(t *testing.T, database *sql.DB, sid, agent, projectDir, familyID, status, model string) {
	t.Helper()
	now := time.Now().UTC()
	sess := &models.Session{
		SessionID:       sid,
		AgentAssigned:   agent,
		CreatedAt:       now,
		Status:          status,
		SessionFamilyID: familyID,
		Model:           model,
		ProjectDir:      projectDir,
	}
	if err := dbpkg.InsertSession(database, sess); err != nil {
		t.Fatalf("InsertSession %s: %v", sid, err)
	}
	if familyID != "" {
		if err := dbpkg.SetSessionFamilyID(database, sid, familyID); err != nil {
			t.Fatalf("SetSessionFamilyID %s: %v", sid, err)
		}
	}
}

// TestSessionsAPI_IncludesExecutionRootAndFamily asserts that /api/sessions
// response includes exec_root and session_family_id fields for each session.
func TestSessionsAPI_IncludesExecutionRootAndFamily(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	projectDir := "/test/project"
	insertParallelSession(t, database, "sess-a1", "claude-code", projectDir, "fam-001", "active", "claude-sonnet-4-5")

	for i := 0; i < 5; i++ {
		_, _ = database.Exec(
			`INSERT INTO messages (session_id, role, content, ordinal) VALUES (?, ?, ?, ?)`,
			"sess-a1", "user", "hello", i+1,
		)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()
	sessionsHandler(database, projectDir, "/test/project/.wipnote")(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}

	var sessions []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected at least one session in response")
	}

	s := sessions[0]

	famID, hasFam := s["session_family_id"]
	if !hasFam {
		t.Error("session_family_id field missing from sessions API response")
	}
	if famID != "fam-001" {
		t.Errorf("session_family_id = %v, want fam-001", famID)
	}

	if _, ok := s["exec_root"]; !ok {
		t.Error("exec_root field missing from sessions API response")
	}
	if _, ok := s["canonical_project"]; !ok {
		t.Error("canonical_project field missing from sessions API response")
	}
	if _, ok := s["harness"]; !ok {
		t.Error("harness field missing from sessions API response")
	}
}

// TestDashboardParallelSessionsAPI asserts that /api/sessions/parallel returns
// active sessions grouped canonical project -> session family -> session with
// identity fields for plan-c3bbb1ed consumption.
//
// Documented default grouping:
//
//	Level 1: canonical_project (sessions.project_dir)
//	Level 2: session_family_id
//	Level 3: individual session with full identity fields
func TestDashboardParallelSessionsAPI(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	projectDir := "/repo/myproject"
	insertParallelSession(t, database, "s-claude", "claude-code", projectDir, "fam-x", "active", "claude-sonnet")
	insertParallelSession(t, database, "s-codex", "codex", projectDir, "fam-x", "active", "gpt-4o")
	insertParallelSession(t, database, "s-solo", "gemini", projectDir, "fam-y", "active", "gemini-2")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions/parallel", nil)
	w := httptest.NewRecorder()
	parallelSessionsHandler(database)(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d, want 200", w.Code)
	}

	var resp map[string]any
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}

	if _, ok := resp["groups"]; !ok {
		t.Error("response missing 'groups' field")
	}
	if _, ok := resp["active_count"]; !ok {
		t.Error("response missing 'active_count' field")
	}

	groups, ok := resp["groups"].([]any)
	if !ok || len(groups) == 0 {
		t.Fatal("groups is not a non-empty array")
	}

	g := groups[0].(map[string]any)
	if _, ok := g["canonical_project"]; !ok {
		t.Error("group missing canonical_project")
	}
	if _, ok := g["families"]; !ok {
		t.Error("group missing families")
	}

	families := g["families"].([]any)
	for _, fAny := range families {
		f := fAny.(map[string]any)
		if _, ok := f["session_family_id"]; !ok {
			t.Error("family missing session_family_id")
		}
		sessionsAny, ok := f["sessions"].([]any)
		if !ok {
			t.Error("family missing sessions array")
			continue
		}
		for _, sAny := range sessionsAny {
			sess := sAny.(map[string]any)
			for _, field := range []string{
				"session_id", "harness", "model", "exec_root", "claim_collision",
			} {
				if _, ok := sess[field]; !ok {
					t.Errorf("session in parallel API missing field %q", field)
				}
			}
		}
	}
}

// TestSessionsAPI_CollisionFieldPresent verifies claim_collision and
// claim_status are present in sessions list (stable contract for plan-c3bbb1ed).
func TestSessionsAPI_CollisionFieldPresent(t *testing.T) {
	database, err := dbpkg.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	defer database.Close()

	projectDir := "/proj/alpha"
	insertParallelSession(t, database, "sess-col1", "claude-code", projectDir, "fam-col", "active", "claude-haiku")
	for i := 0; i < 5; i++ {
		_, _ = database.Exec(
			`INSERT INTO messages (session_id, role, content, ordinal) VALUES (?, ?, ?, ?)`,
			"sess-col1", "user", "msg", i+1,
		)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	w := httptest.NewRecorder()
	sessionsHandler(database, projectDir, "/proj/alpha/.wipnote")(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status %d", w.Code)
	}

	var sessions []map[string]any
	if err := json.NewDecoder(w.Body).Decode(&sessions); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatal("expected sessions")
	}

	s := sessions[0]
	if _, ok := s["claim_collision"]; !ok {
		t.Error("claim_collision missing from sessions API")
	}
	if _, ok := s["claim_status"]; !ok {
		t.Error("claim_status missing from sessions API")
	}
}
