package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	corearch "github.com/shakestzd/wipnote/core/arch"
)

const missingSHA = "0123456789abcdef0123456789abcdef01234567"

// orphanedCommitFixture turns dir into a git repo with one commit on main and
// one commit on a deleted branch. It returns (headSHA, orphanSHA): the orphan
// still exists as an object but is not reachable from HEAD (GH-#173).
func orphanedCommitFixture(t *testing.T, dir string) (string, string) {
	t.Helper()
	run := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = dir
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	run("init", "-b", "main")
	run("config", "user.email", "test@test.com")
	run("config", "user.name", "Test")
	write("a.txt", "a\n")
	run("add", "a.txt")
	run("commit", "-m", "main commit")
	head := run("rev-parse", "HEAD")

	run("checkout", "-q", "-b", "scratch")
	write("b.txt", "b\n")
	run("add", "b.txt")
	run("commit", "-m", "orphan commit")
	orphan := run("rev-parse", "HEAD")
	run("checkout", "-q", "main")
	run("branch", "-D", "scratch")
	return head, orphan
}

func addVerifiedCard(t *testing.T, slug string) {
	t.Helper()
	if err := runArch(t,
		"add", slug,
		"--kind", "invariant",
		"--created-by", "agent",
		"--body", "Body for "+slug+".",
		"--paths", "internal/"+slug+"/**",
	); err != nil {
		t.Fatalf("add %s: %v", slug, err)
	}
}

func cardVerifiedAt(t *testing.T, dir, slug string) string {
	t.Helper()
	store, err := corearch.NewStore(filepath.Join(dir, ".wipnote"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	card, err := store.Get(slug)
	if err != nil {
		t.Fatalf("get %s: %v", slug, err)
	}
	return card.VerifiedAt
}

func TestGitCommitReachable_Classifies(t *testing.T) {
	dir := t.TempDir()
	head, orphan := orphanedCommitFixture(t, dir)
	for _, tc := range []struct {
		name string
		root string
		sha  string
		want commitStatus
	}{
		{"head is ok", dir, head, commitOK},
		{"orphan is unreachable", dir, orphan, commitUnreachable},
		{"unknown sha is missing", dir, missingSHA, commitMissing},
		{"non-git dir is unchecked", t.TempDir(), head, commitUnchecked},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := gitCommitReachable(tc.root, tc.sha); got != tc.want {
				t.Errorf("gitCommitReachable(%s) = %v, want %v", tc.sha, got, tc.want)
			}
		})
	}
}

func TestArchEdit_VerifiedAtRejectsMissingSHA(t *testing.T) {
	dir := setupArchTestDir(t)
	head, orphan := orphanedCommitFixture(t, dir)
	addVerifiedCard(t, "card-a")

	err := runArch(t, "edit", "card-a", "--verified-at", missingSHA)
	if err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("edit with a missing SHA should be rejected, got: %v", err)
	}
	if got := cardVerifiedAt(t, dir, "card-a"); got != "" {
		t.Errorf("rejected edit must not write verified_at, got %q", got)
	}
	if err := runArch(t, "edit", "card-a", "--verified-at", missingSHA, "--force"); err != nil {
		t.Fatalf("--force should bypass the check: %v", err)
	}
	if got := cardVerifiedAt(t, dir, "card-a"); got != missingSHA {
		t.Errorf("--force verified_at = %q, want %q", got, missingSHA)
	}
	// Unreachable-but-existing and reachable SHAs are accepted.
	if err := runArch(t, "edit", "card-a", "--verified-at", orphan); err != nil {
		t.Errorf("orphaned SHA should be accepted with a warning: %v", err)
	}
	if err := runArch(t, "edit", "card-a", "--verified-at", head); err != nil {
		t.Errorf("HEAD SHA should be accepted: %v", err)
	}
	if err := runArch(t, "edit", "card-a", "--verified-at="); err != nil {
		t.Errorf("clearing verified_at should be accepted: %v", err)
	}
}

func TestArchValidate_VerifiedAtResolution(t *testing.T) {
	dir := setupArchTestDir(t)
	head, orphan := orphanedCommitFixture(t, dir)
	addVerifiedCard(t, "card-a")
	addVerifiedCard(t, "card-b")
	if err := runArch(t, "edit", "card-b", "--verified-at", head); err != nil {
		t.Fatalf("pin card-b: %v", err)
	}

	// Missing commit: ERROR, non-zero exit, for both the all and the one form.
	if err := runArch(t, "edit", "card-a", "--verified-at", missingSHA, "--force"); err != nil {
		t.Fatalf("force-pin card-a: %v", err)
	}
	if err := runArch(t, "validate"); err == nil {
		t.Fatal("validate should fail when a verified_at commit does not exist")
	} else if !strings.Contains(err.Error(), "1 card(s) failed validation") {
		t.Errorf("unexpected error: %v", err)
	}
	if err := runArch(t, "validate", "card-a"); err == nil || !strings.Contains(err.Error(), "verified_at") {
		t.Errorf("validate card-a should fail on verified_at, got: %v", err)
	}
	if err := runArch(t, "validate", "card-b"); err != nil {
		t.Errorf("validate card-b (reachable) should pass: %v", err)
	}

	// Unreachable commit: WARN only, exit zero.
	if err := runArch(t, "edit", "card-a", "--verified-at", orphan); err != nil {
		t.Fatalf("pin card-a to orphan: %v", err)
	}
	if err := runArch(t, "validate"); err != nil {
		t.Errorf("validate should only warn on an unreachable verified_at: %v", err)
	}
}

func TestArchRepair_CommitMapRepinsVerifiedAt(t *testing.T) {
	dir := setupArchTestDir(t)
	head, orphan := orphanedCommitFixture(t, dir)
	addVerifiedCard(t, "card-a")
	addVerifiedCard(t, "card-b")
	addVerifiedCard(t, "card-c")
	if err := runArch(t, "edit", "card-a", "--verified-at", missingSHA, "--force"); err != nil {
		t.Fatalf("pin card-a: %v", err)
	}
	if err := runArch(t, "edit", "card-b", "--verified-at", orphan); err != nil {
		t.Fatalf("pin card-b: %v", err)
	}
	if err := runArch(t, "edit", "card-c", "--verified-at", head); err != nil {
		t.Fatalf("pin card-c: %v", err)
	}

	mapPath := filepath.Join(t.TempDir(), "commit-map")
	content := "old new\n" + missingSHA + " " + head + "\n\n# comment\n" + orphan + " " + head + "\n"
	if err := os.WriteFile(mapPath, []byte(content), 0o644); err != nil {
		t.Fatalf("write commit map: %v", err)
	}

	if err := runArch(t, "repair", "--commit-map", mapPath, "--dry-run"); err != nil {
		t.Fatalf("repair --dry-run: %v", err)
	}
	if got := cardVerifiedAt(t, dir, "card-a"); got != missingSHA {
		t.Errorf("dry-run must not rewrite verified_at, got %q", got)
	}

	if err := runArch(t, "repair", "--commit-map", mapPath); err != nil {
		t.Fatalf("repair --commit-map: %v", err)
	}
	for _, slug := range []string{"card-a", "card-b", "card-c"} {
		if got := cardVerifiedAt(t, dir, slug); got != head {
			t.Errorf("%s verified_at = %q, want %q", slug, got, head)
		}
	}
	if err := runArch(t, "validate"); err != nil {
		t.Errorf("validate after repair should pass: %v", err)
	}
}

func TestLoadCommitMap_RejectsMalformedLine(t *testing.T) {
	mapPath := filepath.Join(t.TempDir(), "commit-map")
	if err := os.WriteFile(mapPath, []byte("abc\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if _, err := loadCommitMap(mapPath); err == nil || !strings.Contains(err.Error(), "expected \"old new\"") {
		t.Errorf("malformed line should be rejected, got: %v", err)
	}
}
