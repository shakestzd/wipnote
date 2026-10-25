package main

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/worktree"
)

func TestGC_PreservesDirtyWorktree_WithSnapshot(t *testing.T) {
	repo := setupWorktreeGitRepo(t)
	spikeID := "spk-deadbeef"
	worktreePath := filepath.Join(repo, ".claude", "worktrees", spikeID)
	createGCWorktree(t, repo, worktreePath, "yolo-"+spikeID)

	tracked := filepath.Join(worktreePath, "tracked.txt")
	if err := os.WriteFile(tracked, []byte("seed\n"), 0o644); err != nil {
		t.Fatalf("write tracked seed: %v", err)
	}
	commitGCWorktreeFile(t, worktreePath, "tracked.txt")

	f, err := os.OpenFile(tracked, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open tracked file: %v", err)
	}
	if _, err := f.WriteString("dirty change\n"); err != nil {
		t.Fatalf("write tracked change: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("close tracked file: %v", err)
	}

	state, err := worktree.InspectCleanupState(repo, worktreePath)
	if err != nil {
		t.Fatalf("InspectCleanupState: %v", err)
	}
	if !state.HasTrackedChanges {
		t.Fatal("expected tracked changes to block removal")
	}
	if state.Removable() {
		t.Fatal("dirty worktree must not be removable")
	}

	state, err = worktree.SnapshotPreservedWorktree(repo, worktreePath, spikeID, state)
	if err != nil {
		t.Fatalf("SnapshotPreservedWorktree: %v", err)
	}
	if state.SnapshotRef == "" {
		t.Fatal("expected retained snapshot ref")
	}
	if _, err := os.Stat(worktreePath); err != nil {
		t.Fatalf("worktree should be preserved, stat err=%v", err)
	}
}

func createGCWorktree(t *testing.T, repoRoot, worktreePath, branch string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(worktreePath), 0o755); err != nil {
		t.Fatalf("mkdir parent: %v", err)
	}
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "add", worktreePath, "-b", branch)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree add: %v\n%s", err, out)
	}
}

func lockGCWorktree(t *testing.T, repoRoot, worktreePath string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repoRoot, "worktree", "lock", "--reason", "test-live", worktreePath)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git worktree lock: %v\n%s", err, out)
	}
}

func commitGCWorktreeFile(t *testing.T, worktreePath, path string) {
	t.Helper()
	cmd := exec.Command("git", "-C", worktreePath, "add", path)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git add %s: %v\n%s", path, err, out)
	}
	cmd = exec.Command("git", "-C", worktreePath, "commit", "-m", "seed tracked file")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git commit tracked file: %v\n%s", err, out)
	}
}

func assertFeatureStatus(t *testing.T, database *sql.DB, id, want string) {
	t.Helper()
	var got string
	if err := database.QueryRow(`SELECT status FROM features WHERE id = ?`, id).Scan(&got); err != nil {
		t.Fatalf("feature status %s: %v", id, err)
	}
	if got != want {
		t.Fatalf("feature %s status = %q, want %q", id, got, want)
	}
}
