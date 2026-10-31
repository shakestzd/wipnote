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

// cloneWithIdentity clones origin into dir and sets a commit identity.
func cloneWithIdentity(t *testing.T, tmp, origin, name string) string {
	t.Helper()
	dir := filepath.Join(tmp, name)
	gitIn(t, tmp, "clone", "-q", origin, dir)
	gitIn(t, dir, "config", "user.email", "t@t.com")
	gitIn(t, dir, "config", "user.name", "T")
	return dir
}

// seedTrackedSession commits one session file in repo and pushes it.
func seedTrackedSession(t *testing.T, repo string) {
	t.Helper()
	sess := filepath.Join(repo, ".wipnote", "sessions", "s1")
	if err := os.MkdirAll(sess, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sess, "events.ndjson"), []byte("telemetry\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, repo, "add", "-A")
	gitIn(t, repo, "commit", "-q", "-m", "add sessions")
	gitIn(t, repo, "push", "-q", "origin", "HEAD")
}

// untrackedAndPulled returns clone B's .wipnote dir after clone A untracked
// sessions and B pulled, so git has deleted the session file from B.
func untrackedAndPulled(t *testing.T) (wipnoteDir, sessionFile string) {
	t.Helper()
	tmp, err := os.MkdirTemp("/tmp", "wipnote-sess-restore-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	origin := filepath.Join(tmp, "origin.git")
	gitIn(t, tmp, "init", "--bare", "-q", origin)
	a := cloneWithIdentity(t, tmp, origin, "a")
	seedTrackedSession(t, a)
	b := cloneWithIdentity(t, tmp, origin, "b")
	gitIn(t, a, "rm", "-r", "--cached", "-q", ".wipnote/sessions")
	gitIn(t, a, "commit", "-q", "-m", "untrack sessions", "-m", "Wipnote-Untrack: sessions")
	gitIn(t, a, "push", "-q", "origin", "HEAD")
	gitIn(t, b, "pull", "-q")
	return filepath.Join(b, ".wipnote"), filepath.Join(b, ".wipnote", "sessions", "s1", "events.ndjson")
}

// TestRestoreUntrackedSessions: clone A untracks sessions, clone B pulls (git
// deletes the files), the pass restores them, a second run is a no-op.
func TestRestoreUntrackedSessions(t *testing.T) {
	wd, file := untrackedAndPulled(t)
	if _, err := os.Stat(file); err == nil {
		t.Fatal("precondition: pull should have deleted the session file")
	}
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

// TestRestoreUntrackedSessions_NoTrailer: an ordinary commit that deleted a
// session file is not an untrack commit. Nothing is restored, no sentinel.
func TestRestoreUntrackedSessions_NoTrailer(t *testing.T) {
	tmp, err := os.MkdirTemp("/tmp", "wipnote-sess-notrailer-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
	origin := filepath.Join(tmp, "origin.git")
	gitIn(t, tmp, "init", "--bare", "-q", origin)
	a := cloneWithIdentity(t, tmp, origin, "a")
	seedTrackedSession(t, a)
	gitIn(t, a, "rm", "-q", ".wipnote/sessions/s1/events.ndjson")
	gitIn(t, a, "commit", "-q", "-m", "wipnote: sync 1 items")

	wd := filepath.Join(a, ".wipnote")
	n, err := restoreUntrackedSessions(wd)
	if err != nil || n != 0 {
		t.Fatalf("n=%d err=%v, want 0,nil", n, err)
	}
	if _, err := os.Stat(filepath.Join(wd, "sessions", "s1", "events.ndjson")); err == nil {
		t.Error("deleted session file must not be restored")
	}
	if _, err := os.Stat(filepath.Join(wd, "migrations", sessionsRestoreSentinel)); err == nil {
		t.Error("no sentinel may be written without a trailer commit")
	}
}
