package main

import (
	"bytes"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/htmlgraph/internal/registry"
)

// makeProjectDBWithSchema creates a tmpdir "project" with a .htmlgraph/
// subdirectory and a SQLite DB that has a `features` table matching the
// real htmlgraph schema (type column — 'feature' | 'bug' | 'spike').
// Populates a few rows so ITEMS counts are non-zero.
func makeProjectDBWithSchema(t *testing.T, numFeatures, numBugs, numSpikes int) string {
	t.Helper()
	tmp := t.TempDir()
	hgDir := filepath.Join(tmp, ".htmlgraph")
	if err := os.MkdirAll(filepath.Join(hgDir, ".db"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Create .git directory so project passes looksLikeRealProject check
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(hgDir, ".db", "htmlgraph.db")
	t.Setenv("HTMLGRAPH_DB_PATH", dbPath)

	// Use modernc.org/sqlite driver registered as "sqlite".
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE features (
		id TEXT PRIMARY KEY,
		type TEXT NOT NULL,
		title TEXT NOT NULL DEFAULT ''
	)`)
	if err != nil {
		t.Fatal(err)
	}
	insert := func(kind string, n int) {
		for i := 0; i < n; i++ {
			_, err := db.Exec("INSERT INTO features (id, type, title) VALUES (?, ?, ?)",
				kind+string(rune('a'+i)), kind, kind+" title")
			if err != nil {
				t.Fatal(err)
			}
		}
	}
	insert("feature", numFeatures)
	insert("bug", numBugs)
	insert("spike", numSpikes)
	db.Close()
	return tmp
}

// withRegistryAtAndStale sets up a registry with entries that were previously registered
// but whose .htmlgraph directories may have been deleted (stale). It writes entries directly
// via registry.WriteEntriesForTest — bypassing Upsert's looksLikeRealProject guard so the
// tests can verify prune behavior. The on-disk format is sourced from the same atomic-write
// path Save uses, so this helper cannot drift if the schema evolves.
func withRegistryAtAndStale(t *testing.T, entries []registry.Entry) string {
	t.Helper()
	tmpHome := t.TempDir()
	regPath := filepath.Join(tmpHome, "projects.json")

	// Manually fill in the metadata Upsert would normally populate.
	for i := range entries {
		if entries[i].LastSeen == "" {
			entries[i].LastSeen = time.Now().UTC().Format(time.RFC3339)
		}
		if entries[i].ID == "" {
			entries[i].ID = registry.ComputeID(entries[i].ProjectDir)
		}
	}

	if err := registry.WriteEntriesForTest(regPath, entries); err != nil {
		t.Fatal(err)
	}

	orig := defaultRegistryPath
	defaultRegistryPath = func() string { return regPath }
	t.Cleanup(func() { defaultRegistryPath = orig })
	return regPath
}

// TestProjectsList_Output verifies that `projects list` prints one row per
// registry entry with correct STATUS and ITEMS columns.
func TestProjectsList_Output(t *testing.T) {
	realProject := makeProjectDBWithSchema(t, 3, 2, 1)
	staleProjectDir := filepath.Join(t.TempDir(), "stale-project")
	if err := os.MkdirAll(filepath.Join(staleProjectDir, ".htmlgraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staleProjectDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove .htmlgraph to make it stale
	if err := os.RemoveAll(filepath.Join(staleProjectDir, ".htmlgraph")); err != nil {
		t.Fatal(err)
	}

	withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: realProject, Name: "real"},
		{ProjectDir: staleProjectDir, Name: "stale"},
	})

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}
	out := buf.String()
	if !strings.Contains(out, "real") {
		t.Errorf("expected 'real' in output, got: %s", out)
	}
	if !strings.Contains(out, "stale") {
		t.Errorf("expected 'stale' in output, got: %s", out)
	}
	if !strings.Contains(out, "exists") {
		t.Errorf("expected STATUS=exists for real project, got: %s", out)
	}
	if !strings.Contains(out, "missing") {
		t.Errorf("expected STATUS=missing for stale project, got: %s", out)
	}
	if !strings.Contains(out, "3f 2b 1s") {
		t.Errorf("expected ITEMS '3f 2b 1s' for real project, got: %s", out)
	}
}

// TestProjectsPrune_RemovesAndSaves verifies prune removes stale entries
// and persists the result.
func TestProjectsPrune_RemovesAndSaves(t *testing.T) {
	realProject := makeProjectDBWithSchema(t, 0, 0, 0)
	staleProjectDir := filepath.Join(t.TempDir(), "stale-project")
	if err := os.MkdirAll(filepath.Join(staleProjectDir, ".htmlgraph"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(staleProjectDir, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Remove .htmlgraph to make it stale
	if err := os.RemoveAll(filepath.Join(staleProjectDir, ".htmlgraph")); err != nil {
		t.Fatal(err)
	}

	regPath := withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: realProject, Name: "real"},
		{ProjectDir: staleProjectDir, Name: "stale"},
	})

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Reload the registry and check the stale project is gone.
	reloaded, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune, got %d: %+v", len(entries), entries)
	}
	if entries[0].ProjectDir != realProject {
		t.Errorf("wrong entry remaining: %s", entries[0].ProjectDir)
	}
	out := buf.String()
	if !strings.Contains(out, "pruned:") {
		t.Errorf("expected 'pruned:' in output, got: %s", out)
	}
	if !strings.Contains(out, "pruned 1 stale projects, kept 1") {
		t.Errorf("expected summary line in output, got: %s", out)
	}
}

// TestProjectsList_NoMigrations ensures `projects list` does not create any
// new tables in foreign project DBs — it must use registry.OpenReadOnly.
func TestProjectsList_NoMigrations(t *testing.T) {
	realProject := makeProjectDBWithSchema(t, 1, 1, 1)
	withRegistryAtAndStale(t, []registry.Entry{{ProjectDir: realProject, Name: "real"}})

	// Snapshot table set before.
	dbPath := filepath.Join(realProject, ".htmlgraph", ".db", "htmlgraph.db")
	before := readTableNames(t, dbPath)

	cmd := projectsCmd()
	cmd.SetOut(new(bytes.Buffer))
	cmd.SetArgs([]string{"list"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}

	// Snapshot table set after.
	after := readTableNames(t, dbPath)
	if len(before) != len(after) {
		t.Fatalf("table set changed: before=%v after=%v", before, after)
	}
}

func readTableNames(t *testing.T, dbPath string) []string {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	rows, err := db.Query("SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var names []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatal(err)
		}
		names = append(names, n)
	}
	return names
}

// TestPruneSince_3d_RemovesOlder verifies that --since 3d removes entries older
// than 3 days while keeping recent ones.
func TestPruneSince_3d_RemovesOlder(t *testing.T) {
	old := time.Now().Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)
	recent := time.Now().Add(-1 * time.Hour).UTC().Format(time.RFC3339)

	// Create actual projects with .htmlgraph directories
	tmpHome := t.TempDir()
	oldProj := filepath.Join(tmpHome, "old-project")
	recentProj := filepath.Join(tmpHome, "recent-project")
	if err := os.MkdirAll(filepath.Join(oldProj, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("create old project: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(recentProj, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("create recent project: %v", err)
	}

	regPath := withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: oldProj, Name: "old", LastSeen: old},
		{ProjectDir: recentProj, Name: "recent", LastSeen: recent},
	})

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune", "--since", "3d"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	reloaded, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after --since prune, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "recent" {
		t.Errorf("wrong entry kept: %q", entries[0].Name)
	}
	out := buf.String()
	if !strings.Contains(out, "pruned 1") {
		t.Errorf("expected 'pruned 1' in output, got: %s", out)
	}
}

// TestPruneTempdirOnly_RemovesTestPaths verifies --tempdir-only removes only
// entries that match Go test tempdir naming pattern.
func TestPruneTempdirOnly_RemovesTestPaths(t *testing.T) {
	// Build a real test-tempdir path so ShouldSkipRegistration returns true.
	base := os.TempDir()
	testPath := filepath.Join(base, "TestPruneTarget999")

	regPath := withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: testPath, Name: "test-pollution"},
		{ProjectDir: "/workspaces/htmlgraph", Name: "real"},
	})

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune", "--tempdir-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	reloaded, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after --tempdir-only prune, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "real" {
		t.Errorf("wrong entry kept: %q", entries[0].Name)
	}
}

// TestPruneDryRun_DoesNotWrite verifies --dry-run prints what would be removed
// without mutating the registry on disk.
func TestPruneDryRun_DoesNotWrite(t *testing.T) {
	old := time.Now().Add(-4 * 24 * time.Hour).UTC().Format(time.RFC3339)

	// Create a project with .htmlgraph directory
	tmpHome := t.TempDir()
	oldProj := filepath.Join(tmpHome, "old-dry-project")
	if err := os.MkdirAll(filepath.Join(oldProj, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("create project: %v", err)
	}

	regPath := withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: oldProj, Name: "old", LastSeen: old},
	})

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune", "--since", "3d", "--dry-run"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Registry on disk should be unchanged.
	reloaded, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("dry-run must not write: expected 1 entry on disk, got %d", len(entries))
	}

	out := buf.String()
	if !strings.Contains(out, "dry-run") {
		t.Errorf("expected 'dry-run' in output, got: %s", out)
	}
	if !strings.Contains(out, "would prune") {
		t.Errorf("expected 'would prune' in output, got: %s", out)
	}
}

// TestPruneTempdirEntries_HonorsEnvVar is a regression test for Finding 1:
// verifies that PruneTempdirEntries does NOT remove non-test entries even
// when HTMLGRAPH_SKIP_REGISTER=1 is set. The env-var opt-out should not leak
// from registration semantics into pruning semantics.
func TestPruneTempdirEntries_HonorsEnvVar(t *testing.T) {
	// Create a real project outside tempdir
	realProj := filepath.Join(t.TempDir(), "real-project")
	if err := os.MkdirAll(filepath.Join(realProj, ".htmlgraph"), 0755); err != nil {
		t.Fatalf("create real project: %v", err)
	}

	regPath := withRegistryAtAndStale(t, []registry.Entry{
		{ProjectDir: realProj, Name: "real-project"},
	})

	// Set the env var that should only affect registration, not pruning
	oldEnv := os.Getenv("HTMLGRAPH_SKIP_REGISTER")
	defer func() {
		if oldEnv == "" {
			os.Unsetenv("HTMLGRAPH_SKIP_REGISTER")
		} else {
			os.Setenv("HTMLGRAPH_SKIP_REGISTER", oldEnv)
		}
	}()
	os.Setenv("HTMLGRAPH_SKIP_REGISTER", "1")

	cmd := projectsCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetArgs([]string{"prune", "--tempdir-only"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// The real project should still be in the registry after --tempdir-only prune
	// because it's not a test temp dir, even though HTMLGRAPH_SKIP_REGISTER=1
	reloaded, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	entries := reloaded.List()
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry after prune with HTMLGRAPH_SKIP_REGISTER=1, got %d: %+v", len(entries), entries)
	}
	if entries[0].Name != "real-project" {
		t.Errorf("wrong entry: expected 'real-project', got %q", entries[0].Name)
	}
}
