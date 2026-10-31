package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestRestoreUntrackedSessions: clone A untracks sessions, clone B pulls (git
// deletes the files), the pass restores them, a second run is a no-op.
func TestRestoreUntrackedSessions(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "wipnote-sess-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })

	origin := filepath.Join(tmp, "origin.git")
	gitIn(t, tmp, "init", "--bare", "-q", origin)
	a := filepath.Join(tmp, "a")
	gitIn(t, tmp, "clone", "-q", origin, a)
	for _, k := range [][]string{{"user.email", "t@t.com"}, {"user.name", "T"}} {
		gitIn(t, a, append([]string{"config"}, k...)...)
	}
	sess := filepath.Join(a, ".wipnote", "sessions", "s1")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sess, "events.ndjson"), []byte("telemetry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, a, "add", "-A")
	gitIn(t, a, "commit", "-q", "-m", "add sessions")
	gitIn(t, a, "push", "-q", "origin", "HEAD")

	b := filepath.Join(tmp, "b")
	gitIn(t, tmp, "clone", "-q", origin, b)
	for _, k := range [][]string{{"user.email", "t@t.com"}, {"user.name", "T"}} {
		gitIn(t, b, append([]string{"config"}, k...)...)
	}

	gitIn(t, a, "rm", "-r", "--cached", "-q", ".wipnote/sessions")
	gitIn(t, a, "commit", "-q", "-m", "untrack sessions")
	gitIn(t, a, "push", "-q", "origin", "HEAD")
	gitIn(t, b, "pull", "-q")

	file := filepath.Join(b, ".wipnote", "sessions", "s1", "events.ndjson")
	if _, err := os.Stat(file); err == nil {
		t.Fatal("precondition: pull should have deleted the session file")
	}

	wd := filepath.Join(b, ".wipnote")
	n, err := restoreUntrackedSessions(wd)
	if err != nil || n != 1 {
		t.Fatalf("first run: n=%d err=%v, want 1,nil", n, err)
	}
	if data, err := os.ReadFile(file); err != nil || string(data) != "telemetry\n" {
		t.Fatalf("restored file: %q, %v", data, err)
	}
	if _, err := os.Stat(filepath.Join(wd, "migrations", sessionsRestoreSentinel)); err != nil {
		t.Fatalf("sentinel missing: %v", err)
	}

	os.Remove(file)
	n, err = restoreUntrackedSessions(wd)
	if err != nil || n != 0 {
		t.Fatalf("second run: n=%d err=%v, want 0,nil", n, err)
	}
	if _, err := os.Stat(file); err == nil {
		t.Error("second run must be a no-op")
	}
}
