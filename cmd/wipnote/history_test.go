package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestHistoryResolvesFilePath verifies resolveHistoryPath maps each work-item
// prefix to the correct subdirectory under .wipnote/.
func TestHistoryResolvesFilePath(t *testing.T) {
	t.Parallel()

	// Build a temporary .wipnote tree with one file per type.
	root := t.TempDir()
	hgDir := filepath.Join(root, ".wipnote")

	dirs := map[string]string{
		"feat-abc12345": "features",
		"bug-abc12345":  "bugs",
		"spk-abc12345":  "spikes",
		"plan-abc12345": "plans",
		"trk-abc12345":  "tracks",
	}

	// Create directories and stub files.
	for id, sub := range dirs {
		dir := filepath.Join(hgDir, sub)
		if err := os.MkdirAll(dir, 0755); err != nil {
			t.Fatalf("mkdir %s: %v", dir, err)
		}
		ext := ".html"
		if sub == "plans" {
			ext = ".yaml"
		}
		f := filepath.Join(dir, id+ext)
		if err := os.WriteFile(f, []byte("stub"), 0644); err != nil {
			t.Fatalf("write %s: %v", f, err)
		}
	}

	tests := []struct {
		id      string
		wantDir string
		wantExt string
	}{
		{"feat-abc12345", "features", ".html"},
		{"bug-abc12345", "bugs", ".html"},
		{"spk-abc12345", "spikes", ".html"},
		{"plan-abc12345", "plans", ".yaml"},
		{"trk-abc12345", "tracks", ".html"},
	}

	for _, tt := range tests {
		t.Run(tt.id, func(t *testing.T) {
			t.Parallel()
			got, err := resolveHistoryPath(hgDir, tt.id)
			if err != nil {
				t.Fatalf("resolveHistoryPath(%q) error: %v", tt.id, err)
			}
			want := filepath.Join(hgDir, tt.wantDir, tt.id+tt.wantExt)
			if got != want {
				t.Errorf("resolveHistoryPath(%q) = %q, want %q", tt.id, got, want)
			}
		})
	}
}

// TestHistoryMissingFile verifies a clear error when neither the primary nor
// archive path exists.
func TestHistoryMissingFile(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	hgDir := filepath.Join(root, ".wipnote")
	if err := os.MkdirAll(hgDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	_, err := resolveHistoryPath(hgDir, "feat-deadbeef")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "feat-deadbeef") {
		t.Errorf("error should mention the id; got: %v", err)
	}
}

// seedRepo creates a git repo with two commits to an HTML file and returns the
// repo root path.
func seedRepo(t *testing.T) (repoRoot string, filePath string) {
	t.Helper()

	dir := t.TempDir()

	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Tester",
			"GIT_AUTHOR_EMAIL=tester@example.com",
			"GIT_COMMITTER_NAME=Tester",
			"GIT_COMMITTER_EMAIL=tester@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%v: %v\n%s", args, err, out)
		}
	}

	run("git", "init", "-b", "main")
	run("git", "config", "user.email", "tester@example.com")
	run("git", "config", "user.name", "Tester")

	hgDir := filepath.Join(dir, ".wipnote", "features")
	if err := os.MkdirAll(hgDir, 0755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}

	filePath = filepath.Join(hgDir, "feat-test0001.html")
	if err := os.WriteFile(filePath, []byte("<html>v1</html>"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "first commit")

	if err := os.WriteFile(filePath, []byte("<html>v2</html>"), 0644); err != nil {
		t.Fatalf("write: %v", err)
	}
	run("git", "add", ".")
	run("git", "commit", "-m", "second commit")

	return dir, filePath
}

// TestHistoryRunsGitLog verifies that runHistoryLog returns log lines for both
// commits on a seeded repo.
func TestHistoryRunsGitLog(t *testing.T) {
	t.Parallel()

	repoRoot, _ := seedRepo(t)
	hgDir := filepath.Join(repoRoot, ".wipnote")

	path, err := resolveHistoryPath(hgDir, "feat-test0001")
	if err != nil {
		t.Fatalf("resolveHistoryPath: %v", err)
	}

	entries, err := runHistoryLog(repoRoot, path)
	if err != nil {
		t.Fatalf("runHistoryLog: %v", err)
	}
	if len(entries) < 2 {
		t.Fatalf("expected at least 2 log entries, got %d", len(entries))
	}

	subjects := make([]string, len(entries))
	for i, e := range entries {
		subjects[i] = e.Subject
	}
	joined := strings.Join(subjects, " | ")
	if !strings.Contains(joined, "first commit") {
		t.Errorf("expected 'first commit' in subjects; got: %s", joined)
	}
	if !strings.Contains(joined, "second commit") {
		t.Errorf("expected 'second commit' in subjects; got: %s", joined)
	}
}

// TestResolveHistoryRoot_LinkedWorktreePrefersCwd guards the regression the
// second roborev round flagged: when .wipnote lives in the main checkout
// but the user runs `history` from a linked worktree, resolveHistoryRoot
// must return the LINKED worktree's toplevel so `git log` sees branch-local
// history — NOT the main checkout's HEAD.
//
// Submodule fallback is exercised by running from a completely separate
// repository: git-common-dir differs, and the helper must fall back to the
// .wipnote owner rather than log the unrelated repo.
func TestResolveHistoryRoot_LinkedWorktreePrefersCwd(t *testing.T) {
	// Main checkout with one commit and a .wipnote file.
	mainRoot, _ := seedRepo(t)

	// Absolute main-root (resolving symlinks so comparisons below are stable
	// across macOS /tmp -> /private/tmp redirects).
	mainAbs, err := filepath.EvalSymlinks(mainRoot)
	if err != nil {
		t.Fatalf("eval main symlinks: %v", err)
	}

	// git worktree add <worktree> -b branch-linked
	wtParent := t.TempDir()
	wtDir := filepath.Join(wtParent, "linked-worktree")
	runGit := func(dir string, args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Tester",
			"GIT_AUTHOR_EMAIL=tester@example.com",
			"GIT_COMMITTER_NAME=Tester",
			"GIT_COMMITTER_EMAIL=tester@example.com",
		)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v (in %s): %v\n%s", args, dir, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	runGit(mainRoot, "worktree", "add", wtDir, "-b", "branch-linked")

	wtAbs, err := filepath.EvalSymlinks(wtDir)
	if err != nil {
		t.Fatalf("eval worktree symlinks: %v", err)
	}

	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })

	chdir := func(t *testing.T, dir string) {
		t.Helper()
		if err := os.Chdir(dir); err != nil {
			t.Fatalf("chdir %s: %v", dir, err)
		}
	}

	t.Run("cwd=linked-worktree => returns worktree toplevel", func(t *testing.T) {
		chdir(t, wtDir)
		got, err := resolveHistoryRoot(mainRoot)
		if err != nil {
			t.Fatalf("resolveHistoryRoot: %v", err)
		}
		gotAbs, _ := filepath.EvalSymlinks(got)
		if gotAbs != wtAbs {
			t.Errorf("linked worktree case: resolveHistoryRoot = %q, want %q", gotAbs, wtAbs)
		}
	})

	t.Run("cwd=main-checkout => returns main toplevel", func(t *testing.T) {
		chdir(t, mainRoot)
		got, err := resolveHistoryRoot(mainRoot)
		if err != nil {
			t.Fatalf("resolveHistoryRoot: %v", err)
		}
		gotAbs, _ := filepath.EvalSymlinks(got)
		if gotAbs != mainAbs {
			t.Errorf("main checkout case: resolveHistoryRoot = %q, want %q", gotAbs, mainAbs)
		}
	})

	t.Run("cwd=unrelated-repo => falls back to .wipnote owner", func(t *testing.T) {
		// Build a completely separate git repo so git-common-dir differs.
		otherRoot := t.TempDir()
		runGit(otherRoot, "init", "-b", "main")
		runGit(otherRoot, "config", "user.email", "tester@example.com")
		runGit(otherRoot, "config", "user.name", "Tester")
		if err := os.WriteFile(filepath.Join(otherRoot, "unrelated.txt"), []byte("hi"), 0644); err != nil {
			t.Fatalf("write unrelated: %v", err)
		}
		runGit(otherRoot, "add", ".")
		runGit(otherRoot, "commit", "-m", "unrelated commit")

		chdir(t, otherRoot)
		got, err := resolveHistoryRoot(mainRoot)
		if err != nil {
			t.Fatalf("resolveHistoryRoot: %v", err)
		}
		gotAbs, _ := filepath.EvalSymlinks(got)
		if gotAbs != mainAbs {
			t.Errorf("submodule-fallback case: resolveHistoryRoot = %q, want %q (should NOT escape to unrelated repo)", gotAbs, mainAbs)
		}
	})
}

// TestResolveHistoryRoot_SymlinkedPaths guards the regression where
// resolveHistoryRoot compared raw git-common-dir strings without symlink
// resolution. If cwd and the .wipnote owner reach the same repo through
// different symlinked paths (macOS /tmp vs /private/tmp, or a container
// mount shadowing the host path), the equality check fails and history
// incorrectly falls back to the .wipnote owner.
//
// Setup: create a repo at a real path, then reference it through a symlink.
// Expectation: gitCommonDir yields the same canonical path for both so the
// linked-worktree case still takes the cwd-preferred branch.
func TestResolveHistoryRoot_SymlinkedPaths(t *testing.T) {
	mainRoot, _ := seedRepo(t)
	mainAbs, err := filepath.EvalSymlinks(mainRoot)
	if err != nil {
		t.Fatalf("eval main: %v", err)
	}

	// Symlink the repo under a sibling path so the two access paths differ.
	linkParent := t.TempDir()
	linkPath := filepath.Join(linkParent, "via-symlink")
	if err := os.Symlink(mainRoot, linkPath); err != nil {
		t.Skipf("symlink not supported on this platform: %v", err)
	}

	// gitCommonDir must canonicalize — both paths refer to the same repo.
	direct, err := gitCommonDir(mainRoot)
	if err != nil {
		t.Fatalf("gitCommonDir direct: %v", err)
	}
	viaLink, err := gitCommonDir(linkPath)
	if err != nil {
		t.Fatalf("gitCommonDir via link: %v", err)
	}
	if direct != viaLink {
		t.Errorf("git-common-dir should canonicalize to the same path:\n  direct: %s\n  link:   %s", direct, viaLink)
	}

	// End-to-end: cwd = link path, owner = real path (or vice versa). The
	// linked-worktree branch must fire even though the two strings differ.
	origCwd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origCwd) })
	if err := os.Chdir(linkPath); err != nil {
		t.Fatalf("chdir link: %v", err)
	}

	got, err := resolveHistoryRoot(mainRoot)
	if err != nil {
		t.Fatalf("resolveHistoryRoot: %v", err)
	}
	gotAbs, _ := filepath.EvalSymlinks(got)
	if gotAbs != mainAbs {
		t.Errorf("symlinked cwd should resolve to main repo:\n  got:  %s\n  want: %s", gotAbs, mainAbs)
	}
}

// TestHistoryJSONOutput verifies that --json flag produces a parseable array
// of HistoryEntry objects.
func TestHistoryJSONOutput(t *testing.T) {
	t.Parallel()

	repoRoot, _ := seedRepo(t)
	hgDir := filepath.Join(repoRoot, ".wipnote")

	path, err := resolveHistoryPath(hgDir, "feat-test0001")
	if err != nil {
		t.Fatalf("resolveHistoryPath: %v", err)
	}

	entries, err := runHistoryLog(repoRoot, path)
	if err != nil {
		t.Fatalf("runHistoryLog: %v", err)
	}

	data, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}

	var parsed []historyEntry
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	if len(parsed) < 2 {
		t.Fatalf("expected at least 2 entries in JSON array, got %d", len(parsed))
	}
	for _, e := range parsed {
		if e.SHA == "" {
			t.Error("entry missing SHA")
		}
		if e.Subject == "" {
			t.Error("entry missing Subject")
		}
	}
}
