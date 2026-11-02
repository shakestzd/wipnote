package session

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
)

func gitIn(t *testing.T, dir string, args ...string) string {
	t.Helper()
	out, err := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@t"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// gitEnv makes the env's project a git repo with one commit, and records the
// trust pre-seeds spawn asks for.
func gitEnv(t *testing.T) (*env, *[]string) {
	t.Helper()
	e := newEnv(t)
	e.dir, _ = filepath.EvalSymlinks(e.dir)
	gitIn(t, e.dir, "init", "-q", "-b", "main")
	_ = os.WriteFile(filepath.Join(e.dir, "a.txt"), []byte("a\n"), 0o644)
	gitIn(t, e.dir, "add", "a.txt")
	gitIn(t, e.dir, "commit", "-q", "-m", "init")
	var mu sync.Mutex
	seeded := &[]string{}
	e.deps.PreseedTrust = func(dir string) error {
		mu.Lock()
		defer mu.Unlock()
		*seeded = append(*seeded, dir)
		return nil
	}
	return e, seeded
}

func (e *env) wtOpts(name string) SpawnOptions {
	o := e.opts(name)
	o.Root, o.Worktree = e.dir, true
	o.WorkItem = "feat-0a1b2c3d" // every worktree worker shares one item
	return o
}

func (e *env) wtPath(name string) string { return filepath.Join(e.dir, ".claude", "worktrees", name) }

func TestSpawnWorktree_RunsInItsOwnCheckout(t *testing.T) {
	e, seeded := gitEnv(t)
	s, err := Spawn(context.Background(), e.deps, e.wtOpts("w1"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Worktree != e.wtPath("w1") || s.Branch != "delegate/w1" {
		t.Fatalf("spawned %+v", s)
	}
	info, _ := ReadInfo(e.runFolder(s.Run))
	if info.WorkDir != s.Worktree || info.ProjectDir != e.dir || info.Cwd != s.Worktree || info.Worktree != s.Worktree || info.Branch != "delegate/w1" || info.BaseSHA == "" {
		t.Fatalf("info %+v", info)
	}
	if got := gitIn(t, s.Worktree, "rev-parse", "--abbrev-ref", "HEAD"); got != "delegate/w1" {
		t.Fatalf("branch %s", got)
	}
	if len(*seeded) != 1 || (*seeded)[0] != s.Worktree {
		t.Fatalf("trust seeded for %v", *seeded)
	}
	args := strings.Join(BootArgv("wipnote", info, ""), " ")
	if !strings.Contains(args, "--work-dir "+s.Worktree) || !strings.Contains(args, "--project-dir "+e.dir) {
		t.Fatalf("argv %s", args)
	}
}

func TestSpawnWorktree_Refusals(t *testing.T) {
	e, _ := gitEnv(t)
	o := e.wtOpts("w1")
	o.Container = true
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadWorktree) || !strings.Contains(err.Error(), "--container") {
		t.Fatalf("container: %v", err)
	}
	o = e.wtOpts("w2")
	if _, err := Resume(context.Background(), e.deps, o, "w1"); !errors.Is(err, ErrBadWorktree) || !strings.Contains(err.Error(), "--resume") {
		t.Fatalf("resume: %v", err)
	}
	o = e.wtOpts("w3")
	o.Base = "no-such-ref"
	if _, err := Spawn(context.Background(), e.deps, o); err == nil || !strings.Contains(err.Error(), "no-such-ref") {
		t.Fatalf("base: %v", err)
	}
	rows, _ := e.deps.Ledger.ReadAll()
	if _, err := os.Stat(filepath.Join(e.dir, ".claude")); len(rows) != 0 || err == nil && len(e.herdr.Calls()) != 0 || len(e.herdr.Calls()) != 0 {
		t.Fatalf("refusals created something: rows %v calls %v", rows, e.herdr.Calls())
	}
	// A taken branch refuses with its reason and nothing else is made.
	gitIn(t, e.dir, "branch", "delegate/w4")
	if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w4")); err == nil || !strings.Contains(err.Error(), "branch delegate/w4 already exists") {
		t.Fatalf("taken branch: %v", err)
	}
	if rows, _ := e.deps.Ledger.ReadAll(); len(rows) != 0 || len(e.herdr.Calls()) != 0 {
		t.Fatalf("taken branch created something")
	}
}

func TestSpawnWorktree_LiveNameLeavesNoSecondWorktree(t *testing.T) {
	e, _ := gitEnv(t)
	if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w1")); err != nil {
		t.Fatal(err)
	}
	if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w1")); err == nil {
		t.Fatal("a live name was spawned again")
	}
	if got := gitIn(t, e.dir, "worktree", "list", "--porcelain"); strings.Count(got, "worktree ") != 2 {
		t.Fatalf("worktrees:\n%s", got)
	}
}

func TestSpawnWorktree_SameItemInParallelButNotWithout(t *testing.T) {
	e, _ := gitEnv(t)
	for _, n := range []string{"w1", "w2"} {
		if _, err := Spawn(context.Background(), e.deps, e.wtOpts(n)); err != nil {
			t.Fatalf("%s: %v", n, err)
		}
	}
	plain := e.opts("w3")
	plain.WorkItem = "feat-0a1b2c3d"
	if _, err := Spawn(context.Background(), e.deps, plain); !errors.Is(err, ErrLiveWorker) {
		t.Fatalf("without --worktree the item stays one worker: %v", err)
	}
}

func TestSpawnWorktree_CapStillSix(t *testing.T) {
	e, _ := gitEnv(t)
	for i := 1; i <= 6; i++ {
		if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w"+string(rune('0'+i)))); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w7")); !errors.Is(err, ErrAtCap) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(e.wtPath("w7")); err == nil {
		t.Fatal("the refused seventh left a worktree")
	}
	if exec.Command("git", "-C", e.dir, "rev-parse", "--verify", "-q", "refs/heads/delegate/w7").Run() == nil {
		t.Fatal("the refused seventh left a branch")
	}
}

func TestSpawnWorktree_BootFailureRemovesFreshWorktree(t *testing.T) {
	e, _ := gitEnv(t)
	e.herdr.shellAfter = 1 << 20
	o := e.wtOpts("w1")
	o.ShellWait = 20_000_000
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrShellNotReady) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Stat(e.wtPath("w1")); err == nil {
		t.Fatal("a failed boot left its worktree")
	}
}

func TestStopWorktree_RemovesCleanKeepsDirty(t *testing.T) {
	e, _ := gitEnv(t)
	e.herdr.exitOnEnter = true
	for _, n := range []string{"w1", "w2"} {
		s, err := Spawn(context.Background(), e.deps, e.wtOpts(n))
		if err != nil {
			t.Fatal(err)
		}
		e.herdr.setRunning(PaneOf(s), herdr.Proc{PID: 4242, Name: "claude"})
		e.herdr.pane(PaneOf(s)).styled = "> \n"
	}
	e.setAlive(4242, "")
	_ = os.WriteFile(filepath.Join(e.wtPath("w2"), "new.txt"), []byte("x"), 0o644)

	res := stop(t, e, "w1")
	if !res.PaneClosed || res.Kept || res.Worktree != e.wtPath("w1") || res.Branch != "delegate/w1" {
		t.Fatalf("clean stop %+v", res)
	}
	if _, err := os.Stat(e.wtPath("w1")); err == nil {
		t.Fatal("clean worktree not removed")
	}
	res = stop(t, e, "w2")
	if !res.PaneClosed || !res.Kept || res.Worktree != e.wtPath("w2") || res.Branch != "delegate/w2" {
		t.Fatalf("dirty stop %+v", res)
	}
	if _, err := os.Stat(filepath.Join(e.wtPath("w2"), "new.txt")); err != nil {
		t.Fatal("untracked file lost")
	}
}

func TestResumeWorktree_RunsInKeptWorktree(t *testing.T) {
	e, _ := gitEnv(t)
	o := e.wtOpts("w1")
	e.herdr.exitOnEnter = true
	sp, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	e.event(sp.Run, "sid-a", "session_start")
	e.herdr.setRunning(PaneOf(sp), herdr.Proc{PID: 4242, Name: "claude"})
	e.herdr.pane(PaneOf(sp)).styled = "> \n"
	_ = os.WriteFile(filepath.Join(e.wtPath("w1"), "new.txt"), []byte("x"), 0o644)
	if res := stop(t, e, "w1"); !res.Kept {
		t.Fatalf("stop %+v", res)
	}
	r := e.resumeOpts("w2")
	_, info := resume(t, e, r, "w1")
	if info.WorkDir != e.wtPath("w1") || info.Worktree != e.wtPath("w1") || info.Branch != "delegate/w1" || info.Cwd != e.wtPath("w1") {
		t.Fatalf("resumed info %+v", info)
	}
	// A resumed worktree worker is parallel too: its item is not exclusive.
	if _, err := Spawn(context.Background(), e.deps, e.wtOpts("w3")); err != nil {
		t.Fatal(err)
	}
}

func TestSpawnWorktree_ConcurrentSpawns(t *testing.T) {
	e, _ := gitEnv(t)
	names := []string{"c1", "c2", "c3", "c4", "c5", "c6"}
	var wg sync.WaitGroup
	errs := make([]error, len(names))
	for i, n := range names {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = Spawn(context.Background(), e.deps, e.wtOpts(n))
		}()
	}
	wg.Wait()
	panes := map[string]bool{}
	for i, n := range names {
		if errs[i] != nil {
			t.Fatalf("%s: %v", n, errs[i])
		}
		if _, err := os.Stat(e.wtPath(n)); err != nil {
			t.Fatalf("%s worktree: %v", n, err)
		}
	}
	rows, _ := e.deps.Ledger.ReadAll()
	if len(rows) != 6 {
		t.Fatalf("%d ledger rows", len(rows))
	}
	for _, r := range rows {
		_, info, err := loadInfo(e.deps.Store, r.ID)
		if err != nil || info.Pane == "" || panes[info.Pane] {
			t.Fatalf("run %s info %+v err %v (duplicate or missing pane)", r.ID, info, err)
		}
		panes[info.Pane] = true
	}
	if got := workerPanes(t, e); len(got) != 6 {
		t.Fatalf("workers record has %d panes", len(got))
	}
	if n := strings.Count(strings.Join(e.herdr.Calls(), "\n"), "tab create"); n != 1 {
		t.Fatalf("%d workers tabs created", n)
	}
}

func TestSpawnWorktree_ConcurrentSameName(t *testing.T) {
	e, _ := gitEnv(t)
	var wg sync.WaitGroup
	var mu sync.Mutex
	ok := 0
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := Spawn(context.Background(), e.deps, e.wtOpts("same")); err == nil {
				mu.Lock()
				ok++
				mu.Unlock()
			}
		}()
	}
	wg.Wait()
	open, _ := e.deps.Ledger.ReadOpen()
	if rows := open.Interactive(); ok != 1 || len(rows) != 1 {
		t.Fatalf("%d spawns won, %d open rows", ok, len(rows))
	}
}

func TestClaims_TwoWorkersOneItemExpireIndependently(t *testing.T) {
	e, _ := gitEnv(t)
	claims := withClaims(e)
	a, _ := Spawn(context.Background(), e.deps, e.wtOpts("w1"))
	b, _ := Spawn(context.Background(), e.deps, e.wtOpts("w2"))
	e.event(a.Run, "sid-w1", "session_start")
	e.event(b.Run, "sid-w2", "session_start")
	claim(t, claims, "sid-w1", "feat-0a1b2c3d")
	claim(t, claims, "sid-w2", "feat-0a1b2c3d")

	_ = e.herdr.Close(context.Background(), PaneOf(a))
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); ep.Outcome != claimledger.OutcomeExpired {
		t.Fatalf("lost worker's episode %+v", ep)
	}
	if !episodeOf(t, claims, "sid-w2", "feat-0a1b2c3d").IsOpen() {
		t.Fatal("losing one worker expired the other's episode on the same item")
	}
	if r := e.row(b.Run); !r.IsOpen() || r.EndReason == delegationledger.EndGone {
		t.Fatalf("other worker's row %+v", r)
	}
}

func TestSpawn_LinkedWorktreeCallerRunsWhereItIs(t *testing.T) {
	e, seeded := gitEnv(t)
	linked := e.wtPath("caller")
	gitIn(t, e.dir, "worktree", "add", "-q", "-b", "caller", linked)
	o := e.opts("w1")
	o.Root, o.Cwd = e.dir, linked
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	info, _ := ReadInfo(e.runFolder(s.Run))
	if info.WorkDir != linked || info.ProjectDir != e.dir || info.Worktree != "" {
		t.Fatalf("info %+v", info)
	}
	if len(*seeded) != 1 || (*seeded)[0] != linked {
		t.Fatalf("trust seeded for %v", *seeded)
	}
	// Other side: a caller in the main checkout is unchanged.
	o = e.opts("w2")
	o.Root, o.Cwd = e.dir, e.dir
	s, err = Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	if info, _ := ReadInfo(e.runFolder(s.Run)); info.WorkDir != "" || info.ProjectDir != "" || len(*seeded) != 1 {
		t.Fatalf("main checkout info %+v seeded %v", info, *seeded)
	}
}

func TestSpawnWorktree_RefusesCodex(t *testing.T) {
	e, _ := gitEnv(t)
	o := e.wtOpts("w1")
	o.Harness, o.Profile = HarnessCodex, ""
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrBadWorktree) || !strings.Contains(err.Error(), "Claude") {
		t.Fatalf("err %v", err)
	}
}
