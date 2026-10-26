package gitwatch

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const closedPRArgv = "gh pr list --state closed --json headRefName,state --limit 100"

// staleRepo has one worktree per stale reason plus two that are not stale:
//
//	merged  merged into main with a merge commit
//	squash  squash-merged into main
//	gone    pushed, then its remote branch deleted
//	idle    last commit 30 days ago
//	fresh   a new unpushed commit (its PR was closed unmerged, per gh)
//	empty   created at main, no commits of its own
//	inside  a worktree under the repo itself (path reported repo-relative)
func staleRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	add := func(branch, at string) string {
		dir := filepath.Join(r.root, "wt-"+branch)
		r.git("worktree", "add", "-q", "-b", branch, dir, at)
		return dir
	}
	r.commitIn(add("merged", "main"), "m.txt", "", 0)
	r.git("merge", "-q", "--no-ff", "-m", "merge merged", "merged")
	sq := add("squash", "main")
	r.commitIn(sq, "s1.txt", "", 0)
	r.commitIn(sq, "s2.txt", "", 0)
	r.git("merge", "-q", "--squash", "squash")
	r.gitIn(r.work, nil, "commit", "-q", "-m", "squash squash")
	r.git("push", "-q", "origin", "main")
	gone := add("gone", "main")
	r.commitIn(gone, "g.txt", "", 0)
	r.gitIn(gone, nil, "push", "-q", "-u", "origin", "gone")
	r.git("push", "-q", "origin", "--delete", "gone")
	r.git("fetch", "-q", "--prune")
	r.commitIn(add("idle", "main"), "i.txt", "", 30*24*time.Hour)
	r.commitIn(add("fresh", "main"), "f.txt", "", 0)
	add("empty", "main")
	r.git("worktree", "add", "-q", "-b", "inside", filepath.Join(r.work, ".trees", "inside"), "main")
	return r
}

func closedPRs(out string, err error) splitRunner {
	return splitRunner{Git: ExecRunner{}, GH: &FakeRunner{Results: map[string]FakeResult{
		closedPRArgv: {Stdout: out, Err: err},
	}}}
}

func byBranch(wts []Worktree) map[string]Worktree {
	m := map[string]Worktree{}
	for _, w := range wts {
		m[w.Branch] = w
	}
	return m
}

func TestReadWorktrees_StaleReasons(t *testing.T) {
	r := staleRepo(t)
	run := closedPRs(`[{"headRefName":"fresh","state":"CLOSED"},{"headRefName":"merged","state":"MERGED"}]`, nil)
	wts, notes, err := ReadWorktrees(bg, run, r.work, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	got := byBranch(wts)
	want := map[string][]string{
		"main":   nil,
		"merged": {StaleMerged},
		"squash": {StaleMerged},
		"gone":   {StaleUpstreamGone},
		"idle":   {StaleIdle},
		"fresh":  {StalePRClosed},
		"empty":  nil,
		"inside": nil,
	}
	if len(got) != len(want) {
		t.Fatalf("worktrees = %+v", wts)
	}
	for branch, reasons := range want {
		if !slices.Equal(got[branch].Stale, reasons) {
			t.Errorf("%s: stale = %q, want %q", branch, got[branch].Stale, reasons)
		}
	}
	if len(notes) != 0 {
		t.Errorf("notes = %q", notes)
	}
	if got["idle"].IdleDays < 29 {
		t.Errorf("idle days = %d", got["idle"].IdleDays)
	}
}

func TestReadWorktrees_PathsAreNotHostPaths(t *testing.T) {
	r := staleRepo(t)
	wts, _, err := ReadWorktrees(bg, closedPRs(`[]`, nil), r.work, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	data, _ := json.Marshal(wts)
	if strings.Contains(string(data), r.root) {
		t.Errorf("absolute host path in output: %s", data)
	}
	got := byBranch(wts)
	if got["main"].Path != "." || !got["main"].Main {
		t.Errorf("main worktree = %+v", got["main"])
	}
	if got["inside"].Path != ".trees/inside" {
		t.Errorf("in-repo worktree path = %q, want repo-relative", got["inside"].Path)
	}
	if got["fresh"].Path != "wt-fresh" {
		t.Errorf("outside worktree path = %q, want basename", got["fresh"].Path)
	}
}

func TestReadWorktrees_StaleDaysFromConfig(t *testing.T) {
	r := staleRepo(t)
	cfg := filepath.Join(os.Getenv("HOME"), ".wipnote", "config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"gitwatch":{"stale_days":60}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	wts, _, err := ReadWorktrees(bg, closedPRs(`[]`, nil), r.work, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s := byBranch(wts)["idle"].Stale; len(s) != 0 {
		t.Errorf("30 days is not idle with stale_days 60, got %q", s)
	}
	if got := StaleDays(r.work); got != 60 {
		t.Errorf("StaleDays = %d, want 60", got)
	}
}

func TestStaleDays_Default(t *testing.T) {
	isolateEnv(t)
	if got := StaleDays(t.TempDir()); got != DefaultStaleDays {
		t.Errorf("StaleDays = %d, want %d", got, DefaultStaleDays)
	}
}

func TestReadWorktrees_NoGhSkipsPRClosedWithANote(t *testing.T) {
	r := staleRepo(t)
	wts, notes, err := ReadWorktrees(bg, closedPRs("", ErrNotAvailable), r.work, "main", time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if s := byBranch(wts)["fresh"].Stale; len(s) != 0 {
		t.Errorf("without gh, fresh has no reason, got %q", s)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], StalePRClosed) {
		t.Errorf("want one pr_closed note, got %q", notes)
	}
}

func TestReadWorktrees_DeletesNothing(t *testing.T) {
	r := staleRepo(t)
	before := r.git("worktree", "list", "--porcelain") + r.git("branch", "-a")
	if _, _, err := ReadWorktrees(bg, closedPRs(`[]`, nil), r.work, "main", time.Now()); err != nil {
		t.Fatal(err)
	}
	if after := r.git("worktree", "list", "--porcelain") + r.git("branch", "-a"); after != before {
		t.Errorf("worktrees or branches changed:\nbefore %s\nafter  %s", before, after)
	}
}

func TestReadWorktrees_NotARepoIsError(t *testing.T) {
	isolateEnv(t)
	_, _, err := ReadWorktrees(bg, ExecRunner{}, t.TempDir(), "main", time.Now())
	if err == nil {
		t.Error("outside a repository must be an error")
	}
	_, _, err = ReadWorktrees(bg, &FakeRunner{}, "", "main", time.Now())
	if err == nil || errors.Is(err, ErrNotAvailable) {
		t.Errorf("fake with no git results: %v", err)
	}
}

func TestParseWorktrees(t *testing.T) {
	out := "worktree /r/main\nHEAD aaa\nbranch refs/heads/main\n\n" +
		"worktree /r/wt-a\nHEAD bbb\nbranch refs/heads/a\nlocked reason here\n\n" +
		"worktree /r/wt-b\nHEAD ccc\ndetached\nprunable gitdir file points to non-existent location\n"
	got := parseWorktrees(out)
	want := []rawWorktree{
		{path: "/r/main", branch: "main", main: true},
		{path: "/r/wt-a", branch: "a", locked: true},
		{path: "/r/wt-b", prunable: true},
	}
	if !slices.Equal(got, want) {
		t.Errorf("parseWorktrees = %+v\nwant %+v", got, want)
	}
}
