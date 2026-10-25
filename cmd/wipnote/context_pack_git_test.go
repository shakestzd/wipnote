package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrackGitHistory_CommitsAndFiles(t *testing.T) {
	repo := t.TempDir()
	initGitRepo(t, repo)
	commit := func(file, msg string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Dir(filepath.Join(repo, file)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(repo, file), []byte(msg), 0o644); err != nil {
			t.Fatal(err)
		}
		mustGit(t, repo, "add", "-A")
		mustGit(t, repo, "commit", "-q", "-m", msg)
	}
	commit("core/a.go", "feat: first (feat-aaaa1111)")
	commit("core/a.go", "fix: second\n\nRefs: feat-bbbb2222")
	commit("cmd/b.go", "chore: unrelated work")
	commit(".wipnote/features/feat-aaaa1111.html", "chore: artifact (feat-aaaa1111)")

	commits, files := trackGitHistory(repo, []string{"feat-aaaa1111", "feat-bbbb2222"}, 10)

	if len(commits) != 3 {
		t.Fatalf("want 3 linked commits, got %d: %+v", len(commits), commits)
	}
	if commits[1].FeatureID != "feat-bbbb2222" || commits[1].Message != "fix: second" {
		t.Errorf("commit 1 = %+v", commits[1])
	}
	if len(files) != 1 {
		t.Fatalf("want only core/a.go (artifact paths skipped), got %+v", files)
	}
	if f := files[0]; f.Path != "core/a.go" || f.Touches != 2 || f.Features != 2 {
		t.Errorf("file entry = %+v", f)
	}
}

func TestTrackGitHistory_NoFeaturesOrRepo(t *testing.T) {
	if c, f := trackGitHistory(t.TempDir(), []string{"feat-aaaa1111"}, 10); c != nil || f != nil {
		t.Errorf("non-repo: want nil, got %v %v", c, f)
	}
	if c, f := trackGitHistory(t.TempDir(), nil, 10); c != nil || f != nil {
		t.Errorf("no features: want nil, got %v %v", c, f)
	}
}
