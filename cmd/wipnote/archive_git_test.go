package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/graph"
)

func archGit(t *testing.T, repoRoot string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", repoRoot}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func dirtySource(t *testing.T, repoRoot string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(repoRoot, "README.md"), []byte("edited\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// (a) A dirty source file does not block a tracked store, and the archive commits.
func TestArchive_SourceDirtyTrackedStoreCommits(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")
	commitAll(t, repoRoot)
	dirtySource(t, repoRoot)
	before := archGit(t, repoRoot, "rev-list", "--count", "HEAD")

	if err := runArchive(true, defaultArchiveAgeDays); err != nil {
		t.Fatalf("runArchive: %v", err)
	}
	if after := archGit(t, repoRoot, "rev-list", "--count", "HEAD"); after == before {
		t.Errorf("expected an archive commit, HEAD count stayed %s", after)
	}
	if !strings.Contains(archGit(t, repoRoot, "ls-files", ".wipnote/archive"), "features.html") {
		t.Errorf("ledger not committed")
	}
}

// (b) A store the project ignores is archived on disk with no commit and no index entry.
func TestArchive_IgnoredStoreNoCommit(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"), []byte("/.wipnote/*\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	archGit(t, repoRoot, "add", ".gitignore")
	archGit(t, repoRoot, "commit", "-m", "ignore store")
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")
	dirtySource(t, repoRoot)
	before := archGit(t, repoRoot, "rev-list", "--count", "HEAD")

	if err := runArchive(true, defaultArchiveAgeDays); err != nil {
		t.Fatalf("runArchive: %v", err)
	}
	if fileExists(t, filepath.Join(wipnoteDir, "features", "feat-old00001.html")) {
		t.Errorf("feature file not archived")
	}
	if !fileExists(t, graph.ArchiveLedgerPath(wipnoteDir, "features")) {
		t.Errorf("ledger not written")
	}
	if after := archGit(t, repoRoot, "rev-list", "--count", "HEAD"); after != before {
		t.Errorf("archive committed against the project's ignore rule")
	}
	if idx := archGit(t, repoRoot, "ls-files", ".wipnote"); idx != "" {
		t.Errorf("store files were added to the index: %q", idx)
	}
}

// (c) A store with no git repo above it archives without error.
func TestArchive_NoGitRepoArchivesOnDisk(t *testing.T) {
	root := t.TempDir()
	wipnoteDir := filepath.Join(root, ".wipnote")
	for _, sub := range []string{"features", "bugs", "spikes", "tracks"} {
		if err := os.MkdirAll(filepath.Join(wipnoteDir, sub), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")

	if err := runArchive(true, defaultArchiveAgeDays); err != nil {
		t.Fatalf("runArchive: %v", err)
	}
	if !fileExists(t, graph.ArchiveLedgerPath(wipnoteDir, "features")) {
		t.Errorf("ledger not written")
	}
}

// (d) A tracked store with uncommitted .wipnote/ changes still refuses.
func TestArchive_TrackedStoreDirtyStillRefuses(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")
	commitAll(t, repoRoot)
	writeDoneFeature(t, wipnoteDir, "feat-old00002", "Newer file", 61*24*time.Hour, "")

	err := runArchive(true, defaultArchiveAgeDays)
	if err == nil || !strings.Contains(err.Error(), "uncommitted") {
		t.Fatalf("expected uncommitted refusal, got %v", err)
	}
}

// (e) A tracked store hidden by an exclude rule (the conductor worktree
// layout) still commits.
func TestArchive_ExcludedButTrackedStoreCommits(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")
	commitAll(t, repoRoot)
	exclude := filepath.Join(repoRoot, ".git", "info", "exclude")
	if err := os.MkdirAll(filepath.Dir(exclude), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exclude, []byte("\n.wipnote/\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := archGit(t, repoRoot, "rev-list", "--count", "HEAD")

	if err := runArchive(true, defaultArchiveAgeDays); err != nil {
		t.Fatalf("runArchive: %v", err)
	}
	if after := archGit(t, repoRoot, "rev-list", "--count", "HEAD"); after == before {
		t.Errorf("expected an archive commit in the excluded-but-tracked layout")
	}
}

// Commit policy none: archive on disk, no commit.
func TestArchive_PolicyNoneNoCommit(t *testing.T) {
	repoRoot, wipnoteDir := setupArchiveRepo(t)
	writeDoneFeature(t, wipnoteDir, "feat-old00001", "Old feature", 60*24*time.Hour, "")
	commitAll(t, repoRoot)
	t.Setenv("WIPNOTE_ARTIFACT_COMMIT_POLICY", "none")
	before := archGit(t, repoRoot, "rev-list", "--count", "HEAD")

	if err := runArchive(true, defaultArchiveAgeDays); err != nil {
		t.Fatalf("runArchive: %v", err)
	}
	if after := archGit(t, repoRoot, "rev-list", "--count", "HEAD"); after != before {
		t.Errorf("policy none still committed")
	}
}
