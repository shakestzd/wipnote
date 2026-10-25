package graph_test

import (
	"database/sql"
	"testing"

	"github.com/shakestzd/wipnote/core/db/dbtest"
	"github.com/shakestzd/wipnote/core/graph"
)

// openTestDB creates an in-memory SQLite database with the full schema.
func openTestDB(t *testing.T) *sql.DB {
	t.Helper()
	database, err := dbtest.Open(":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func mustExec(t *testing.T, database *sql.DB, query string, args ...any) {
	t.Helper()
	if _, err := database.Exec(query, args...); err != nil {
		t.Fatalf("seed: %v", err)
	}
}

func resolveOne(t *testing.T, database *sql.DB, id string) graph.NodeResult {
	t.Helper()
	m := graph.ResolveToMap(database, nil, []string{id})
	r, ok := m[id]
	if !ok {
		t.Fatalf("ResolveToMap(%q): not resolved, got %v", id, m)
	}
	return r
}

func TestResolveToMap_CommitMetadata(t *testing.T) {
	database := openTestDB(t)
	mustExec(t, database,
		`INSERT INTO git_commits (commit_hash, session_id, message) VALUES (?, ?, ?)`,
		"abc123", "sess-1", "fix: resolve the issue with long message padding")

	r := resolveOne(t, database, "abc123")
	if r.Type != "commit" || r.Title == "" || r.Status != "done" {
		t.Errorf("commit metadata = %+v, want type commit, a title, status done", r)
	}
}

func TestResolveToMap_FileMetadata(t *testing.T) {
	database := openTestDB(t)
	mustExec(t, database,
		`INSERT INTO feature_files (id, feature_id, file_path, operation) VALUES (?, ?, ?, ?)`,
		"ff-1", "feat-a", "internal/graph/dsl.go", "modified")

	r := resolveOne(t, database, "internal/graph/dsl.go")
	if r.Type != "file" || r.Title != "internal/graph/dsl.go" {
		t.Errorf("file metadata = %+v, want type file titled by its path", r)
	}
}

func TestResolveToMap_SessionMetadata(t *testing.T) {
	database := openTestDB(t)
	mustExec(t, database,
		`INSERT INTO sessions (session_id, agent_assigned, status) VALUES (?, ?, ?)`,
		"sess-1", "claude", "active")

	r := resolveOne(t, database, "sess-1")
	if r.Type != "session" || r.Status != "active" {
		t.Errorf("session metadata = %+v, want type session, status active", r)
	}
}

func TestResolveToMap_UnresolvedIDKeepsEmptyMetadata(t *testing.T) {
	database := openTestDB(t)
	r := resolveOne(t, database, "sess-orphan")
	if r.Title != "" {
		t.Errorf("unresolved node title = %q, want empty", r.Title)
	}
}

func TestFormatNodeLabel(t *testing.T) {
	results := map[string]graph.NodeResult{"feat-a": {ID: "feat-a", Title: "Alpha"}}
	if got := graph.FormatNodeLabel("feat-a", results); got != "feat-a (Alpha)" {
		t.Errorf("FormatNodeLabel = %q", got)
	}
	if got := graph.FormatNodeLabel("feat-b", results); got != "feat-b" {
		t.Errorf("FormatNodeLabel without title = %q", got)
	}
}
