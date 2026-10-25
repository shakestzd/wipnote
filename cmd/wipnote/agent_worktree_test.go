package main

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func setupAgentGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Init git repo
	cmds := [][]string{
		{"git", "init"},
		{"git", "config", "user.email", "test@test.com"},
		{"git", "config", "user.name", "Test"},
	}
	for _, args := range cmds {
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("%v failed: %s", args, out)
		}
	}

	// Create initial commit
	f, _ := os.Create(filepath.Join(dir, "README.md"))
	f.WriteString("# Test")
	f.Close()
	exec.Command("git", "-C", dir, "add", ".").Run()
	exec.Command("git", "-C", dir, "commit", "-m", "initial").Run()

	return dir
}

func TestEnsureForAgent_BranchesFromTrackBranch(t *testing.T) {
	dir := setupAgentGitRepo(t)

	// Create a track branch first (not via worktree, just as a branch)
	exec.Command("git", "-C", dir, "branch", "track-abc").Run()

	// Now create agent worktree branching from track-abc
	path, err := EnsureForAgent("track-abc", "task1", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForAgent: %v", err)
	}

	// Verify path
	expected := filepath.Join(dir, ".claude", "worktrees", "track-abc", "agent-task1")
	if path != expected {
		t.Errorf("expected path %s, got %s", expected, path)
	}

	// Verify branch name uses flat naming scheme
	out, _ := exec.Command("git", "-C", path, "rev-parse", "--abbrev-ref", "HEAD").Output()
	branch := strings.TrimSpace(string(out))
	if branch != "agent-track-abc-task1" {
		t.Errorf("expected branch agent-track-abc-task1, got %s", branch)
	}
}

func TestEnsureForAgent_PathNamingConvention(t *testing.T) {
	dir := setupAgentGitRepo(t)

	// Create a track branch
	exec.Command("git", "-C", dir, "branch", "mytrack").Run()

	path, err := EnsureForAgent("mytrack", "my-task", dir, io.Discard)
	if err != nil {
		t.Fatalf("EnsureForAgent: %v", err)
	}

	// Check the worktree path follows naming convention
	if !strings.Contains(path, "mytrack") || !strings.Contains(path, "agent-my-task") {
		t.Errorf("path doesn't follow naming convention: %s", path)
	}
}

func TestEnsureForAgent_FailsWithoutTrackBranch(t *testing.T) {
	dir := setupAgentGitRepo(t)

	// Don't create track branch — agent should fail
	_, err := EnsureForAgent("nonexistent-track", "task1", dir, io.Discard)
	if err == nil {
		t.Error("expected error when track branch doesn't exist")
	}
}

func TestEnsureForAgent_ReusesExistingPath(t *testing.T) {
	dir := setupAgentGitRepo(t)

	// Create a track branch
	exec.Command("git", "-C", dir, "branch", "track-reuse").Run()

	// Create agent worktree
	path1, err := EnsureForAgent("track-reuse", "task1", dir, io.Discard)
	if err != nil {
		t.Fatalf("first EnsureForAgent: %v", err)
	}

	// Try to create the same agent worktree again
	path2, err := EnsureForAgent("track-reuse", "task1", dir, io.Discard)
	if err != nil {
		t.Fatalf("second EnsureForAgent: %v", err)
	}

	if path1 != path2 {
		t.Errorf("reused worktree paths don't match: %s vs %s", path1, path2)
	}
}
