package hooks

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// stubRouteSQLAsync overrides the package-level daemon enqueue seam for the
// duration of a test and restores it on cleanup. ret is what the stub returns;
// the returned *bool reports whether the stub was actually invoked (so a test
// can prove RouteHookWrite tried the daemon path first).
func stubRouteSQLAsync(t *testing.T, ret bool) *bool {
	t.Helper()
	called := false
	prev := routeSQLAsync
	routeSQLAsync = func(projectRoot, sqlStmt string, args ...any) bool {
		called = true
		return ret
	}
	t.Cleanup(func() { routeSQLAsync = prev })
	return &called
}

// fileExists reports whether path exists, without creating it.
func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// TestRouteHookWrite_DaemonPath asserts that when the enqueue-only daemon route
// succeeds, RouteHookWrite returns success and opens NO direct writable handle.
// Proof of "no direct open": WIPNOTE_DB_PATH points at a path that does not yet
// exist; the bounded fallback (OpenHookDBWithBusyTimeout) would create+migrate
// that file, so its continued ABSENCE after the call means the direct path was
// never taken.
func TestRouteHookWrite_DaemonPath(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "should-not-be-created.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)

	called := stubRouteSQLAsync(t, true) // daemon enqueue succeeds

	ok := RouteHookWrite("posttooluse", dir, "sess-daemon",
		`INSERT INTO sessions (session_id, agent_assigned, status) VALUES (?, ?, ?)`,
		"s1", "agent-1", "active")

	if !ok {
		t.Fatal("RouteHookWrite returned false on a successful daemon enqueue")
	}
	if !*called {
		t.Fatal("RouteHookWrite did not attempt the daemon enqueue path first")
	}
	// The direct fallback would have created this file; its absence proves no
	// direct writable handle was opened.
	if fileExists(dbPath) {
		t.Fatalf("direct DB handle was opened (%s exists) despite successful daemon enqueue", dbPath)
	}
}

// TestRouteHookWrite_NoDaemon_DropsAtOnce pins the daemon-miss contract
// (feat-174dc958) against the REAL daemon seam: with no writer socket the
// write is dropped, the call returns at once, and no file database is created
// in its place. Contrast TestRouteHookWrite_DaemonPath, where the daemon takes
// the write and RouteHookWrite reports it.
func TestRouteHookWrite_NoDaemon_DropsAtOnce(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(dir, "should-not-be-created.db")
	t.Setenv("WIPNOTE_DB_PATH", dbPath)

	start := time.Now()
	ok := RouteHookWrite("posttooluse", dir, "sess-nodaemon",
		`INSERT INTO sessions (session_id, agent_assigned, status) VALUES (?, ?, ?)`,
		"x", "agent-1", "active")
	elapsed := time.Since(start)

	if ok {
		t.Fatal("RouteHookWrite reported success with no daemon to take the write")
	}
	if elapsed >= 250*time.Millisecond {
		t.Fatalf("RouteHookWrite took %v with no daemon; a miss must return at once", elapsed)
	}
	if fileExists(dbPath) {
		t.Fatalf("a daemon miss created the file database %s", dbPath)
	}
}
