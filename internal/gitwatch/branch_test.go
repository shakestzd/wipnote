package gitwatch

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var bg = context.Background()

// feature branch: 1 pushed + 2 local commits on top of a main that then
// moved on by one commit at origin; one tracked edit and one untracked file.
func featureRepo(t *testing.T) *testRepo {
	r := newTestRepo(t)
	r.git("checkout", "-q", "-b", "feat")
	r.commit("a.txt", "", 0)
	r.git("push", "-q", "-u", "origin", "feat")
	r.commit("b.txt", "", 0)
	r.commit("c.txt", "", 0)
	r.git("checkout", "-q", "main")
	r.commit("main2.txt", "", 0)
	r.git("push", "-q", "origin", "main")
	r.git("checkout", "-q", "feat")
	if err := os.WriteFile(filepath.Join(r.work, "a.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.work, "new.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r
}

func TestReadBranch_AheadBehindDirtyUnpushed(t *testing.T) {
	r := featureRepo(t)
	b, err := ReadBranch(bg, ExecRunner{}, r.work, "main")
	if err != nil {
		t.Fatal(err)
	}
	want := Branch{Name: "feat", Base: "origin/main", Ahead: 3, Behind: 1, Dirty: 2, Unpushed: 2,
		Upstream: "origin/feat", Sync: SyncAhead}
	if *b != want {
		t.Errorf("branch = %+v\nwant     %+v", *b, want)
	}
}

func TestReadBranch_CleanAndInSync(t *testing.T) {
	r := newTestRepo(t)
	b, err := ReadBranch(bg, ExecRunner{}, r.work, "")
	if err != nil {
		t.Fatal(err)
	}
	want := Branch{Name: "main", Base: "origin/main", Upstream: "origin/main", Sync: SyncUpToDate}
	if *b != want {
		t.Errorf("branch = %+v, want %+v", *b, want)
	}
}

// With no upstream every commit not on any remote is unpushed.
func TestReadBranch_LocalOnlyUnpushed(t *testing.T) {
	r := newTestRepo(t)
	r.git("checkout", "-q", "-b", "solo")
	r.commit("s1.txt", "", 0)
	r.commit("s2.txt", "", 0)
	b, err := ReadBranch(bg, ExecRunner{}, r.work, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b.Unpushed != 2 || b.Ahead != 2 || b.Sync != SyncLocalOnly || b.Upstream != "" {
		t.Errorf("local-only branch = %+v", *b)
	}
}

func TestReadBranch_UpstreamGone(t *testing.T) {
	r := newTestRepo(t)
	r.git("checkout", "-q", "-b", "gone")
	r.commit("g.txt", "", 0)
	r.git("push", "-q", "-u", "origin", "gone")
	r.git("push", "-q", "origin", "--delete", "gone")
	r.git("fetch", "-q", "--prune")
	b, err := ReadBranch(bg, ExecRunner{}, r.work, "main")
	if err != nil {
		t.Fatal(err)
	}
	if b.Sync != SyncDeletedAtRemote {
		t.Errorf("sync = %q, want %q", b.Sync, SyncDeletedAtRemote)
	}
}

func TestResolveBase(t *testing.T) {
	r := newTestRepo(t)
	if got, err := ResolveBase(bg, ExecRunner{}, r.work, ""); err != nil || got != "origin/main" {
		t.Errorf("default base = %q, %v; want origin/main", got, err)
	}
	r.git("branch", "dev")
	if got, err := ResolveBase(bg, ExecRunner{}, r.work, "dev"); err != nil || got != "dev" {
		t.Errorf("local-only base = %q, %v; want dev", got, err)
	}
	if _, err := ResolveBase(bg, ExecRunner{}, r.work, "nope"); err == nil {
		t.Error("missing base must be an error")
	}
}

func TestAheadBehind_ForEachRef(t *testing.T) {
	r := featureRepo(t)
	if !gitAtLeast(t, 2, 41) {
		t.Skip("git < 2.41 has no %(ahead-behind)")
	}
	var calls []string
	ahead, behind, err := AheadBehind(bg, recordRunner{&calls}, r.work, "origin/main")
	if err != nil || ahead != 3 || behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, %v; want 3/1", ahead, behind, err)
	}
	for _, c := range calls {
		if strings.Contains(c, "rev-list") {
			t.Errorf("new git must not fall back to rev-list: %q", calls)
		}
	}
}

func TestAheadBehind_OldGitFallsBackToRevList(t *testing.T) {
	r := featureRepo(t)
	var calls []string
	ahead, behind, err := AheadBehind(bg, oldGitRunner{&calls}, r.work, "origin/main")
	if err != nil || ahead != 3 || behind != 1 {
		t.Fatalf("ahead/behind = %d/%d, %v; want 3/1", ahead, behind, err)
	}
	if !strings.Contains(strings.Join(calls, "\n"), "rev-list --left-right --count origin/main...HEAD") {
		t.Errorf("want the rev-list fallback, calls %q", calls)
	}
}

func TestReadBranch_OldGit(t *testing.T) {
	r := featureRepo(t)
	var calls []string
	b, err := ReadBranch(bg, oldGitRunner{&calls}, r.work, "main")
	if err != nil || b.Ahead != 3 || b.Behind != 1 {
		t.Fatalf("old git branch = %+v, %v", b, err)
	}
}

func TestAheadBehind_MissingBaseIsError(t *testing.T) {
	r := newTestRepo(t)
	if _, _, err := AheadBehind(bg, ExecRunner{}, r.work, "origin/nope"); err == nil {
		t.Error("missing base must be an error")
	}
}

func TestDetermineSyncStatus(t *testing.T) {
	cases := []struct {
		track, upstream string
		want            SyncStatus
	}{
		{"gone", "origin/x", SyncDeletedAtRemote},
		{"ahead 2, behind 3", "origin/x", SyncNotInSync},
		{"ahead 2", "origin/x", SyncAhead},
		{"behind 1", "origin/x", SyncBehind},
		{"", "origin/x", SyncUpToDate},
		{"", "", SyncLocalOnly},
		{"weird", "origin/x", SyncUnknown},
	}
	for _, c := range cases {
		if got := determineSyncStatus(c.track, c.upstream); got != c.want {
			t.Errorf("determineSyncStatus(%q, %q) = %q, want %q", c.track, c.upstream, got, c.want)
		}
	}
}

func TestParseAheadBehindField(t *testing.T) {
	if got := parseAheadBehindField("3 1"); got != (aheadBehind{ahead: 3, behind: 1, valid: true}) {
		t.Errorf("3 1 -> %+v", got)
	}
	for _, bad := range []string{"", "3", "a b", "1 2 3"} {
		if parseAheadBehindField(bad).valid {
			t.Errorf("%q must be invalid", bad)
		}
	}
}

func TestParseStatusV2(t *testing.T) {
	out := "# branch.oid abc\n# branch.head feat\n# branch.upstream origin/feat\n# branch.ab +2 -1\n" +
		"1 .M N... 100644 100644 100644 a a a.txt\n? new.txt\n"
	st := parseStatusV2(out)
	if st.head != "feat" || st.upstream != "origin/feat" || !st.hasAB || st.ahead != 2 || st.behind != 1 || st.dirty != 2 {
		t.Errorf("status = %+v", st)
	}
	gone := parseStatusV2("# branch.head g\n# branch.upstream origin/g\n")
	if gone.hasAB || gone.track() != "gone" {
		t.Errorf("upstream with no ab line is gone: %+v", gone)
	}
}

// recordRunner is ExecRunner that records each argv.
type recordRunner struct{ Calls *[]string }

func (o recordRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	*o.Calls = append(*o.Calls, strings.Join(args, " "))
	return ExecRunner{}.Run(ctx, name, args...)
}

// gitAtLeast reports whether the installed git is at least major.minor.
func gitAtLeast(t *testing.T, major, minor int) bool {
	t.Helper()
	out, _, _, err := ExecRunner{}.Run(bg, "git", "version")
	if err != nil {
		return false
	}
	var maj, min int
	if _, err := fmt.Sscanf(string(out), "git version %d.%d", &maj, &min); err != nil {
		return false
	}
	return maj > major || maj == major && min >= minor
}
