package main

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

var dataUpdatedRe = regexp.MustCompile(`data-updated="[^"]*"`)

// staleFixture builds a git repo with one track and returns repo root, .wipnote dir and track ID.
func staleFixture(t *testing.T) (root, hgDir, trackID string) {
	t.Helper()
	root, hgDir = setupResetTest(t)
	projectDirFlag = root
	t.Cleanup(func() { projectDirFlag = "" })
	trackID = testSetupTrack(t, hgDir)
	return root, hgDir, trackID
}

func staleGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", root, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func staleInitRepo(t *testing.T, root string) {
	t.Helper()
	staleGit(t, root, "init", "-q")
	// wipnote's own commits run plain git, so the identity must be in the repo.
	staleGit(t, root, "config", "user.name", "t")
	staleGit(t, root, "config", "user.email", "t@t")
	staleGit(t, root, "config", "commit.gpgsign", "false")
	staleGit(t, root, "add", "-A")
	staleGit(t, root, "commit", "-q", "-m", "fixture")
}

// makeItem creates a feature and backdates its data-updated; returns its ID.
func makeItem(t *testing.T, hgDir, trackID, title, updated string) string {
	t.Helper()
	before, _ := filepath.Glob(filepath.Join(hgDir, "features", "feat-*.html"))
	seen := map[string]bool{}
	for _, f := range before {
		seen[f] = true
	}
	if err := testCreate("feature", title, trackID, "medium", false, false); err != nil {
		t.Fatalf("create: %v", err)
	}
	after, _ := filepath.Glob(filepath.Join(hgDir, "features", "feat-*.html"))
	for _, f := range after {
		if seen[f] {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		b = dataUpdatedRe.ReplaceAll(b, []byte(`data-updated="`+updated+`"`))
		if err := os.WriteFile(f, b, 0o644); err != nil {
			t.Fatal(err)
		}
		n, err := htmlparse.ParseFile(f)
		if err != nil {
			t.Fatal(err)
		}
		return n.ID
	}
	t.Fatal("no new feature file")
	return ""
}

func staleStatusOf(t *testing.T, hgDir, id string) models.NodeStatus {
	t.Helper()
	n, err := htmlparse.ParseFile(filepath.Join(hgDir, "features", id+".html"))
	if err != nil {
		t.Fatal(err)
	}
	return n.Status
}

func runStaleCmd(t *testing.T, args ...string) string {
	t.Helper()
	cmd := staleCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs(args)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("stale %v: %v\n%s", args, err, buf.String())
	}
	return buf.String()
}

func TestStale_DryRunChangesNothing(t *testing.T) {
	root, hgDir, trackID := staleFixture(t)
	id := makeItem(t, hgDir, trackID, "Old one", "2020-01-01T00:00:00")
	staleInitRepo(t, root)
	head := staleGit(t, root, "rev-parse", "HEAD")

	out := runStaleCmd(t, "--older-than", "90d")
	if !strings.Contains(out, id) {
		t.Errorf("dry-run should list %s:\n%s", id, out)
	}
	if got := staleStatusOf(t, hgDir, id); got != models.StatusTodo {
		t.Errorf("status changed in dry-run: %s", got)
	}
	if staleGit(t, root, "rev-parse", "HEAD") != head {
		t.Error("dry-run made a commit")
	}
	if staleGit(t, root, "status", "--porcelain") != "" {
		t.Error("dry-run dirtied the tree")
	}
}

func TestStale_ApplyMarksAndCommitsOnce(t *testing.T) {
	root, hgDir, trackID := staleFixture(t)
	a := makeItem(t, hgDir, trackID, "Old A", "2020-01-01T00:00:00")
	b := makeItem(t, hgDir, trackID, "Old B", "2020-02-01T00:00:00")
	staleInitRepo(t, root)
	before := staleGit(t, root, "rev-list", "--count", "HEAD")

	runStaleCmd(t, "--older-than", "90d", "--apply")

	for _, id := range []string{a, b} {
		if got := staleStatusOf(t, hgDir, id); got != models.StatusStale {
			t.Errorf("%s status = %s, want stale", id, got)
		}
	}
	after := staleGit(t, root, "rev-list", "--count", "HEAD")
	if before == after {
		t.Fatal("no commit made")
	}
	if staleGit(t, root, "rev-list", "--count", "HEAD~1") != before {
		t.Errorf("expected exactly one commit; before=%s after=%s", before, after)
	}
	files := staleGit(t, root, "show", "--name-only", "--format=", "HEAD")
	if !strings.Contains(files, a) || !strings.Contains(files, b) {
		t.Errorf("commit should contain both items:\n%s", files)
	}
}

func TestStale_SkipsRecentAndDone(t *testing.T) {
	root, hgDir, trackID := staleFixture(t)
	recent := makeItem(t, hgDir, trackID, "Recent", "2999-01-01T00:00:00")
	done := makeItem(t, hgDir, trackID, "Done old", "2020-01-01T00:00:00")
	if err := runWiSetStatus("feature", done, "done"); err != nil {
		t.Fatalf("set done: %v", err)
	}
	// SetStatus refreshed data-updated; backdate again.
	f := filepath.Join(hgDir, "features", done+".html")
	b, _ := os.ReadFile(f)
	_ = os.WriteFile(f, dataUpdatedRe.ReplaceAll(b, []byte(`data-updated="2020-01-01T00:00:00"`)), 0o644)
	staleInitRepo(t, root)

	out := runStaleCmd(t, "--older-than", "90d", "--apply")
	if strings.Contains(out, recent) || strings.Contains(out, done) {
		t.Errorf("recent/done items listed:\n%s", out)
	}
	if got := staleStatusOf(t, hgDir, recent); got != models.StatusTodo {
		t.Errorf("recent status = %s", got)
	}
	if got := staleStatusOf(t, hgDir, done); got != models.StatusDone {
		t.Errorf("done status = %s", got)
	}
}

func TestReset_AcceptsStale(t *testing.T) {
	root, hgDir, trackID := staleFixture(t)
	id := makeItem(t, hgDir, trackID, "Old reset", "2020-01-01T00:00:00")
	staleInitRepo(t, root)
	runStaleCmd(t, "--apply")
	if got := staleStatusOf(t, hgDir, id); got != models.StatusStale {
		t.Fatalf("precondition: status = %s", got)
	}
	if _, err := executeReset("feature", id); err != nil {
		t.Fatalf("reset stale: %v", err)
	}
	if got := staleStatusOf(t, hgDir, id); got != models.StatusTodo {
		t.Errorf("status after reset = %s, want todo", got)
	}
}
