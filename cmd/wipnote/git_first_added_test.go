package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// gitAt runs a git command in dir with a fixed author and committer date, so
// every commit in a test has a distinct, known timestamp.
func gitAt(t *testing.T, dir string, when time.Time, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	stamp := when.Format(time.RFC3339)
	cmd.Env = append(os.Environ(), "GIT_AUTHOR_DATE="+stamp, "GIT_COMMITTER_DATE="+stamp)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// writeRepoFile writes content to rel inside dir.
func writeRepoFile(t *testing.T, dir, rel, content string) {
	t.Helper()
	full := filepath.Join(dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// templateHTML is a work-item body that differs between items only in the id,
// like the files `wipnote <kind> create` writes from one template.
func templateHTML(id string) string {
	var b strings.Builder
	b.WriteString(`<article id="` + id + `" data-type="feature" data-status="todo">` + "\n")
	for i := 0; i < 40; i++ {
		b.WriteString(`<section class="template-line">shared template text line</section>` + "\n")
	}
	b.WriteString("</article>\n")
	return b.String()
}

var (
	day1 = time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC)
	day2 = time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC)
	day3 = time.Date(2026, 1, 3, 10, 0, 0, 0, time.UTC)
)

// A new work item created from the shared template looks like a copy of an
// older item to `git log --follow`, which then reports the OLDER item's
// creation date. The batch walk must report the new item's own first add.
func TestBatchGitFileTimestamps_CopyFromOtherItemUsesOwnAdd(t *testing.T) {
	repo := setupGitRepo(t)
	writeRepoFile(t, repo, ".wipnote/features/feat-old.html", templateHTML("feat-old"))
	gitAt(t, repo, day1, "add", "-A")
	gitAt(t, repo, day1, "commit", "-m", "create feat-old")
	writeRepoFile(t, repo, ".wipnote/features/feat-new.html", templateHTML("feat-new"))
	gitAt(t, repo, day2, "add", "-A")
	gitAt(t, repo, day2, "commit", "-m", "create feat-new")

	newPath := filepath.Join(repo, ".wipnote/features/feat-new.html")
	follow, err := gitFirstAdded(repo, newPath)
	if err != nil {
		t.Fatal(err)
	}
	if !follow.Equal(day1) {
		t.Skipf("this git does not treat the template copy as a copy (follow=%v); nothing to compare", follow)
	}

	got := batchGitFileTimestamps(repo, []string{newPath})[newPath]
	if !got.created.Equal(day2) {
		t.Fatalf("created = %v, want %v (the commit that created feat-new)", got.created, day2)
	}
}

// A real rename chain (the .htmlgraph -> .erinn -> .wipnote moves in this
// repo's history) must still resolve to the original add.
func TestBatchGitFileTimestamps_FollowsDirectoryRenameChain(t *testing.T) {
	repo := setupGitRepo(t)
	writeRepoFile(t, repo, ".htmlgraph/bugs/bug-1.html", templateHTML("bug-1"))
	gitAt(t, repo, day1, "add", "-A")
	gitAt(t, repo, day1, "commit", "-m", "create bug-1")
	gitAt(t, repo, day2, "mv", ".htmlgraph", ".erinn")
	gitAt(t, repo, day2, "commit", "-m", "rename to .erinn")
	gitAt(t, repo, day3, "mv", ".erinn", ".wipnote")
	gitAt(t, repo, day3, "commit", "-m", "rename to .wipnote")

	p := filepath.Join(repo, ".wipnote/bugs/bug-1.html")
	got, ok := batchGitFileTimestamps(repo, []string{p})[p]
	if !ok {
		t.Fatal("renamed file missing from batch")
	}
	if !got.created.Equal(day1) {
		t.Fatalf("created = %v, want %v (original add before both renames)", got.created, day1)
	}
	if !got.updated.Equal(day3) {
		t.Fatalf("updated = %v, want %v (last commit touching the path)", got.updated, day3)
	}
}

// A path deleted and later re-added keeps its FIRST add, like --follow.
func TestBatchGitFileTimestamps_ReAddKeepsFirstAdd(t *testing.T) {
	repo := setupGitRepo(t)
	writeRepoFile(t, repo, "bugs/bug-2.html", "v1\n")
	gitAt(t, repo, day1, "add", "-A")
	gitAt(t, repo, day1, "commit", "-m", "add")
	gitAt(t, repo, day2, "rm", "-q", "bugs/bug-2.html")
	gitAt(t, repo, day2, "commit", "-m", "delete")
	writeRepoFile(t, repo, "bugs/bug-2.html", "v2\n")
	gitAt(t, repo, day3, "add", "-A")
	gitAt(t, repo, day3, "commit", "-m", "re-add")

	p := filepath.Join(repo, "bugs/bug-2.html")
	got := batchGitFileTimestamps(repo, []string{p})[p]
	if !got.created.Equal(day1) {
		t.Fatalf("created = %v, want %v", got.created, day1)
	}
}

// The batch must cost a constant number of git processes, not one per file.
// That per-file cost was the 15-minute daemon startup (bug-06d2c91d).
func TestBatchGitFileTimestamps_ConstantGitCalls(t *testing.T) {
	repo := setupGitRepo(t)
	var paths []string
	for i := 0; i < 30; i++ {
		rel := filepath.Join(".wipnote", "bugs", "bug-"+string(rune('a'+i%26))+string(rune('a'+i/26))+".html")
		writeRepoFile(t, repo, rel, "x\n")
		paths = append(paths, filepath.Join(repo, rel))
	}
	gitAt(t, repo, day1, "add", "-A")
	gitAt(t, repo, day1, "commit", "-m", "add all")

	var calls atomic.Int64
	old := gitTimestampCmd
	gitTimestampCmd = func(dir string, args ...string) ([]byte, error) {
		calls.Add(1)
		return old(dir, args...)
	}
	defer func() { gitTimestampCmd = old }()

	batch := batchGitFileTimestamps(repo, paths)
	if len(batch) != len(paths) {
		t.Fatalf("batch has %d entries, want %d", len(batch), len(paths))
	}
	if n := calls.Load(); n > 3 {
		t.Fatalf("batch ran %d git processes for %d files, want at most 3 (rev-parse + two walks)", n, len(paths))
	}
}

func TestParseFirstAddedWalk(t *testing.T) {
	out := strings.Join([]string{
		"@@2026-01-01T10:00:00Z",
		"A\told/a.html",
		"A\tb.html",
		"@@2026-01-02T10:00:00Z",
		"R100\told/a.html\tnew/a.html",
		"A\tb.html",
		"@@2026-01-03T10:00:00Z",
		"R087\tgone.html\tc.html",
	}, "\n")
	got := parseFirstAddedWalk(out)
	want := map[string]time.Time{
		"old/a.html": day1,
		"new/a.html": day1, // inherits through the rename
		"b.html":     day1, // first add wins over a re-add
		"c.html":     day3, // rename from an unseen source dates from the rename
	}
	for p, w := range want {
		if !got[p].Equal(w) {
			t.Errorf("%s = %v, want %v", p, got[p], w)
		}
	}
}
