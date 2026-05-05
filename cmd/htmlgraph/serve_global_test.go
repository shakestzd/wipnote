package main

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/htmlgraph/internal/registry"
)

// globalTestProject creates a tmpdir project with a .htmlgraph dir. Unlike
// the pre-doorway version, it does NOT populate a SQLite schema because
// the doorway server no longer opens project DBs. A bare .htmlgraph/
// directory is enough for registry.Upsert to accept the path.
// A .git directory is also created so looksLikeRealProject passes the
// git-ancestor check introduced by the registry hardening (bug-cc41e3d2).
func globalTestProject(t *testing.T) string {
	t.Helper()
	tmp := t.TempDir()
	hgDir := filepath.Join(tmp, ".htmlgraph")
	if err := os.MkdirAll(hgDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Touch an empty DB file so registry.Upsert doesn't skip this entry
	// in environments that might perform a light existence check.
	f, err := os.Create(filepath.Join(hgDir, "htmlgraph.db"))
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
	// Create a .git directory so looksLikeRealProject passes.
	if err := os.MkdirAll(filepath.Join(tmp, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	return tmp
}

// setupGlobalRegistry points registry.DefaultPath at a tmpdir file and
// registers the given project dirs. Returns the registry file path. Note
// that registry.DefaultPath is resolved via os.UserHomeDir so we need to
// override HOME — t.Setenv handles cleanup automatically.
func setupGlobalRegistry(t *testing.T, projectDirs ...string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	// Clear XDG_DATA_HOME so DefaultPath falls back to the HOME-derived path
	// (TestMain sets XDG_DATA_HOME for suite-wide isolation, but these tests
	// control the registry path explicitly via HOME).
	t.Setenv("XDG_DATA_HOME", "")
	regPath := registry.DefaultPath()
	if err := os.MkdirAll(filepath.Dir(regPath), 0o755); err != nil {
		t.Fatal(err)
	}
	reg, _ := registry.Load(regPath)
	for _, dir := range projectDirs {
		reg.Upsert(dir, filepath.Base(dir), "")
	}
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	return regPath
}

// expectedID returns the 8-char SHA256 prefix of dir, matching the ID
// format the registry uses for stable project identity.
func expectedID(dir string) string {
	h := sha256.Sum256([]byte(dir))
	return hex.EncodeToString(h[:])[:8]
}

// TestDoorwayModeEndpoint verifies /api/mode returns {"mode":"global"} —
// the doorway server is always in global mode now; single mode only
// exists inside the child mux.
func TestDoorwayModeEndpoint(t *testing.T) {
	setupGlobalRegistry(t)
	srv := httptest.NewServer(buildGlobalMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/mode")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatal(err)
	}
	if body["mode"] != "global" {
		t.Errorf("mode: got %v, want global", body["mode"])
	}
	// Doorway /api/mode must not include projects — that's /api/projects.
	if _, ok := body["projects"]; ok {
		t.Errorf("/api/mode should not include projects field")
	}
}

// TestDoorwayProjectsEndpoint verifies /api/projects returns registry
// entries with stable IDs and no count fields.
func TestDoorwayProjectsEndpoint(t *testing.T) {
	p1 := globalTestProject(t)
	p2 := globalTestProject(t)
	setupGlobalRegistry(t, p1, p2)

	srv := httptest.NewServer(buildGlobalMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	var list []projectSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 projects, got %d", len(list))
	}

	// Verify field set matches the doorway schema (no featureCount etc.)
	var raw []map[string]any
	resp2, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if err := json.NewDecoder(resp2.Body).Decode(&raw); err != nil {
		t.Fatal(err)
	}
	forbidden := []string{"featureCount", "bugCount", "spikeCount"}
	for _, item := range raw {
		for _, f := range forbidden {
			if _, ok := item[f]; ok {
				t.Errorf("/api/projects entry should not include %s", f)
			}
		}
	}
}

// TestDoorwayStableIDs verifies the ID format matches the registry's
// SHA256-prefix derivation so /p/<id>/ lookup is consistent.
func TestDoorwayStableIDs(t *testing.T) {
	p1 := globalTestProject(t)
	setupGlobalRegistry(t, p1)

	srv := httptest.NewServer(buildGlobalMux())
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/projects")
	if err != nil {
		t.Fatal(err)
	}
	var list []projectSummary
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()

	if len(list) != 1 {
		t.Fatalf("expected 1, got %d", len(list))
	}
	if list[0].ID != expectedID(p1) {
		t.Errorf("ID mismatch: got %s, want %s", list[0].ID, expectedID(p1))
	}
}

// TestDoorwayRegistryRefresh verifies /api/projects re-reads the registry
// on every request so newly-registered projects appear without a restart.
func TestDoorwayRegistryRefresh(t *testing.T) {
	p1 := globalTestProject(t)
	regPath := setupGlobalRegistry(t, p1)

	srv := httptest.NewServer(buildGlobalMux())
	defer srv.Close()

	fetch := func() []projectSummary {
		resp, err := http.Get(srv.URL + "/api/projects")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out []projectSummary
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if got := fetch(); len(got) != 1 {
		t.Fatalf("first fetch: expected 1, got %d", len(got))
	}

	p2 := globalTestProject(t)
	reg, _ := registry.Load(regPath)
	reg.Upsert(p2, filepath.Base(p2), "")
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}

	if got := fetch(); len(got) != 2 {
		t.Errorf("after refresh: expected 2, got %d", len(got))
	}
}

// TestDoorwayNeverOpensDBs is the architectural correctness check for
// the cross-project isolation guarantee. It creates a project DB with a
// known schema, hits every doorway route, and verifies the DB file was
// never touched (mtime unchanged). This encodes the invariant that the
// parent server holds zero SQLite handles.
func TestDoorwayNeverOpensDBs(t *testing.T) {
	p1 := globalTestProject(t)
	setupGlobalRegistry(t, p1)

	dbPath := filepath.Join(p1, ".htmlgraph", "htmlgraph.db")

	// Initialize a minimal schema so we can detect any mutation.
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE features (id TEXT PRIMARY KEY)`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	before, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}

	srv := httptest.NewServer(buildGlobalMux())
	defer srv.Close()

	// Hit every doorway route. None of these should touch the DB.
	urls := []string{
		"/api/mode",
		"/api/projects",
		"/", // landing SPA
	}
	for _, u := range urls {
		resp, err := http.Get(srv.URL + u)
		if err != nil {
			t.Errorf("GET %s: %v", u, err)
			continue
		}
		resp.Body.Close()
	}

	after, err := os.Stat(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("project DB mtime changed — doorway opened a handle")
	}
	if before.Size() != after.Size() {
		t.Errorf("project DB size changed — doorway mutated schema")
	}
}
