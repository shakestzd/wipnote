package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/commitqueue"
)

// setupIgnoredWipnoteRepo builds a real git repo under /tmp whose repo-level
// .gitignore ignores the whole .wipnote/ tree (the GH#172 misconfiguration)
// and returns the repo root plus the repo-relative path of an untracked
// work-item artifact written under it.
func setupIgnoredWipnoteRepo(t *testing.T) (repoRoot, artifactRel string) {
	t.Helper()
	tmpParent, err := os.MkdirTemp("/tmp", "wipnote-gitignore-*")
	if err != nil {
		t.Fatalf("MkdirTemp /tmp: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(tmpParent) })
	repoRoot = setupWorktreeGitRepoIn(t, tmpParent)
	gitMustCommitInitial(t, repoRoot)
	if err := os.WriteFile(filepath.Join(repoRoot, ".gitignore"), []byte(".wipnote/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	artifactRel = filepath.Join(".wipnote", "features", "feat-ignored.html")
	abs := filepath.Join(repoRoot, artifactRel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(abs, []byte(`<article id="feat-ignored" data-status="done"></article>`), 0o644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}
	return repoRoot, artifactRel
}

func TestIsGitIgnored(t *testing.T) {
	repoRoot, artifactRel := setupIgnoredWipnoteRepo(t)

	rule, ignored := isGitIgnored(repoRoot, artifactRel)
	if !ignored {
		t.Fatalf("%s should be ignored by the .wipnote/ rule", artifactRel)
	}
	if !strings.HasSuffix(rule, ".gitignore:1:.wipnote/") {
		t.Fatalf("rule = %q, want <source>:1:.wipnote/", rule)
	}
	if rule, ignored := isGitIgnored(repoRoot, "README.md"); ignored {
		t.Fatalf("tracked README.md must not be ignored, got rule %q", rule)
	}
	if _, ignored := isGitIgnored(t.TempDir(), artifactRel); ignored {
		t.Fatal("a non-git directory must read as not ignored")
	}
}

func TestOutboxCommitterClassifiesIgnoredPath(t *testing.T) {
	repoRoot, artifactRel := setupIgnoredWipnoteRepo(t)

	err := outboxCommitter(commitqueue.Intent{
		RepoRoot:   repoRoot,
		RelPaths:   []string{artifactRel},
		Message:    "wipnote: complete feat-ignored",
		WorkItemID: "feat-ignored",
	})
	if !errors.Is(err, commitqueue.ErrPathIgnored) {
		t.Fatalf("committer should classify an ignored path with ErrPathIgnored, got: %v", err)
	}
	if !strings.Contains(err.Error(), ".gitignore:1:.wipnote/") || !strings.Contains(err.Error(), ".wipnote/sessions/") {
		t.Fatalf("error should name the rule and the sessions-only remediation, got: %v", err)
	}
}

func TestRecordCommitIntentWarnsWhenPathIgnored(t *testing.T) {
	repoRoot, artifactRel := setupIgnoredWipnoteRepo(t)
	tmp := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(tmp, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig })

	var errBuf bytes.Buffer
	origStderr := stderr
	stderr = &errBuf
	t.Cleanup(func() { stderr = origStderr })

	if err := recordCommitIntent(repoRoot, []string{artifactRel}, "wipnote: complete feat-ignored", "feat-ignored", "complete"); err != nil {
		t.Fatalf("recordCommitIntent must still record the intent, got: %v", err)
	}
	ob, _ := openCommitOutbox(repoRoot)
	if pending, _ := ob.Pending(); len(pending) != 1 {
		t.Fatalf("expected the intent to be recorded despite the warning, got %d pending", len(pending))
	}
	if !strings.Contains(errBuf.String(), "ignored by git rule") {
		t.Fatalf("expected an enqueue-time gitignore warning, got: %q", errBuf.String())
	}
}

func TestFailIfPendingDeferredArtifactCommits_IgnoredPathExplainsGitignore(t *testing.T) {
	projectRoot := setupGateTestProject(t)
	initWorktreeGitRepo(t, projectRoot)
	if err := os.WriteFile(filepath.Join(projectRoot, ".gitignore"), []byte(".wipnote/\n"), 0o644); err != nil {
		t.Fatalf("write .gitignore: %v", err)
	}
	tmpOutbox := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(tmpOutbox, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig })

	ob, _ := openCommitOutbox(projectRoot)
	if err := ob.Append(commitqueue.Intent{
		RepoRoot:   projectRoot,
		RelPaths:   []string{".wipnote/features/feat-gate.html"},
		Message:    "wipnote: complete feat-gate",
		WorkItemID: "feat-gate",
		Action:     "complete",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	err := failIfPendingDeferredArtifactCommits(projectRoot, "feat-gate", &strings.Builder{})
	if err == nil {
		t.Fatal("ignored artifact intent must still block the gate")
	}
	if strings.Contains(err.Error(), "wipnote commit-queue flush") {
		t.Fatalf("gate must not recommend flush for an ignored path, got: %v", err)
	}
	if !strings.Contains(err.Error(), "ignored by git rule") || !strings.Contains(err.Error(), ".wipnote/sessions/") {
		t.Fatalf("gate should explain the gitignore misconfiguration, got: %v", err)
	}
}

func TestWarnIfRepoGitignoreIgnoresWipnote(t *testing.T) {
	cases := []struct {
		name      string
		gitignore string // "" = no file
		warn      bool
	}{
		{"no gitignore", "", false},
		{"sessions only", "# ok\n.wipnote/sessions/\n*.log\n", false},
		{"trailing slash", "node_modules/\n.wipnote/\n", true},
		{"bare", ".wipnote\n", true},
		{"anchored", "/.wipnote/\n", true},
		{"star", ".wipnote/*\n!.wipnote/features/\n", true},
		{"padded", "   .wipnote/   \n", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			if tc.gitignore != "" {
				if err := os.WriteFile(filepath.Join(root, ".gitignore"), []byte(tc.gitignore), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			var buf bytes.Buffer
			got := warnIfRepoGitignoreIgnoresWipnote(&buf, root)
			if got != tc.warn {
				t.Fatalf("warned = %v, want %v (output %q)", got, tc.warn, buf.String())
			}
			if tc.warn && !strings.Contains(buf.String(), ".wipnote/sessions/") {
				t.Fatalf("warning should suggest the sessions-only pattern, got %q", buf.String())
			}
		})
	}
}

// TestCommitQueueFlush_PrintsIgnoredIntentPerLine drives the real flush
// command against a repo that ignores .wipnote/ and asserts the intent is
// reported (not retried, not dead-lettered) and its Attempts stay at zero.
func TestCommitQueueFlush_PrintsIgnoredIntentPerLine(t *testing.T) {
	repoRoot, artifactRel := setupIgnoredWipnoteRepo(t)
	isolateProjectDir(t, repoRoot)
	projectDirFlag = repoRoot
	t.Cleanup(func() { projectDirFlag = "" })
	tmp := t.TempDir()
	orig := commitOutboxPath
	commitOutboxPath = func(string) (string, error) { return filepath.Join(tmp, "commit-outbox.ndjson"), nil }
	t.Cleanup(func() { commitOutboxPath = orig })

	ob, _ := openCommitOutbox(repoRoot)
	if err := ob.Append(commitqueue.Intent{
		RepoRoot: repoRoot, RelPaths: []string{artifactRel},
		Message: "wipnote: complete feat-ignored", WorkItemID: "feat-ignored", Action: "complete",
	}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	cmd := commitQueueCmd()
	var out strings.Builder
	cmd.SetOut(&out)
	cmd.SetErr(&out)
	cmd.SetArgs([]string{"flush", "--max-attempts", "1"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("flush: %v\n%s", err, out.String())
	}
	if !strings.Contains(out.String(), "ignored=1") || !strings.Contains(out.String(), "ignored (not retried, not dead-lettered): feat-ignored") {
		t.Fatalf("flush should report the ignored intent per line, got:\n%s", out.String())
	}
	if strings.Contains(out.String(), "dead-lettered=1") {
		t.Fatalf("ignored intent must not dead-letter even at --max-attempts 1:\n%s", out.String())
	}
	pending, _ := ob.Pending()
	if len(pending) != 1 || pending[0].Attempts != 0 {
		t.Fatalf("ignored intent should remain queued with Attempts=0, got %+v", pending)
	}
	// Sanity: git really does refuse the path, so the classification is honest.
	if out, err := exec.Command("git", "-C", repoRoot, "add", "--", artifactRel).CombinedOutput(); err == nil {
		t.Fatalf("expected git add to refuse the ignored path, got success:\n%s", out)
	}
}
