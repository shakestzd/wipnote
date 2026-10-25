package oneshot

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ccxSetup makes a runs folder holding another run's newer log, and points
// the stub and CCX_RUNS_DIR at it.
func ccxSetup(t *testing.T) (runs string) {
	t.Helper()
	runs = t.TempDir()
	other := filepath.Join(runs, "other.jsonl")
	data, err := os.ReadFile(fixture(t, "ccx_other.jsonl"))
	must(t, err)
	must(t, os.WriteFile(other, data, 0o600))
	future := time.Now().Add(time.Hour)
	must(t, os.Chtimes(other, future, future)) // newest by mtime, yet not this run's
	t.Setenv("CCX_RUNS_DIR", runs)
	t.Setenv("STUB_RUNS", runs)
	t.Setenv("STUB_STDOUT", fixture(t, "ccx_own.jsonl"))
	return runs
}

// fixes.test.ts "the ccx follower claims a new log instead of taking the
// newest by mtime": the run follows the log that appeared after it started,
// and its report is ccx's own stdout and stderr, then the run page.
func TestRun_CcxFollowsOwnLog(t *testing.T) {
	p := newProject(t)
	runs := ccxSetup(t)
	res := p.run(p.options("ccx", "task", "fix the build"))
	md := p.file(res.RunID, runstore.FileCleanMD)
	if !strings.Contains(md, "tsconfig.json") || strings.Contains(md, "OTHER RUN") {
		t.Fatalf("clean.md followed the wrong log:\n%s", md)
	}
	dir := p.runDir(res.RunID)
	if out := p.stdout.String(); out != "ccx: task done in 1s\nthe result\nrun page: "+dir+"/run.html\n" {
		t.Fatalf("ccx report %q", out)
	}
	if p.stderr.String() != "ccx warning\n" {
		t.Fatalf("stderr %q", p.stderr.String())
	}
	if last := p.file(res.RunID, runstore.FileLastMD); last != "> ccx: task done in 1s\n\nthe result\n" {
		t.Fatalf("last.md %q", last)
	}
	claims, _ := os.ReadDir(claimsDir())
	var names []string
	for _, c := range claims {
		names = append(names, c.Name())
	}
	if got := strings.Join(names, " "); !strings.Contains(got, ".jsonl") || strings.Contains(got, "other.jsonl") || strings.Contains(got, "launch.lock") {
		t.Fatalf("claims %q in %s (runs %s)", got, claimsDir(), runs)
	}
	if r := p.row(res.RunID); r.Harness != "ccx" || r.Host != "container" {
		t.Fatalf("row %+v", r)
	}
}

// fixes.test.ts ccxRunsDir cases: CCX_RUNS_DIR wins over the setting, the
// setting is used as written, and with neither (no machine default, plan
// decision) or a missing folder the log says so.
func TestCcx_RunsDirSetting(t *testing.T) {
	if got := ccxRunsDir("/from/setting $x"); got != "/from/setting $x" {
		t.Fatalf("setting: %q", got)
	}
	t.Setenv("CCX_RUNS_DIR", "/from/env")
	if got := ccxRunsDir("/from/setting"); got != "/from/env" {
		t.Fatalf("env must win: %q", got)
	}
	t.Setenv("CCX_RUNS_DIR", "")
	if got := ccxRunsDir(""); got != "" {
		t.Fatalf("a machine default crept in: %q", got)
	}
	p := newProject(t)
	t.Setenv("STUB_RUNS", t.TempDir())
	t.Setenv("STUB_STDOUT", fixture(t, "ccx_own.jsonl"))
	res := p.run(p.options("ccx", "task", "x"))
	if log := p.file(res.RunID, runstore.FileCleanLog); !strings.Contains(log, "*ccx runs directory not set (set ccxRunsDir or CCX_RUNS_DIR)*") {
		t.Fatalf("clean.log %q", log)
	}
	t.Setenv("CCX_RUNS_DIR", filepath.Join(t.TempDir(), "missing"))
	res = p.run(p.options("ccx", "task", "x"))
	if log := p.file(res.RunID, runstore.FileCleanLog); !strings.Contains(log, "*ccx runs directory not found: ") {
		t.Fatalf("clean.log %q", log)
	}
}

// startHolder runs a process that stays alive, for a live lock holder or
// unbound mark; its pid is returned.
func startHolder(t *testing.T) int {
	t.Helper()
	c := exec.Command("sleep", "30")
	must(t, c.Start())
	t.Cleanup(func() { _ = c.Process.Kill(); _ = c.Wait() })
	return c.Process.Pid
}

func deadPid(t *testing.T) int {
	t.Helper()
	c := exec.Command("true")
	must(t, c.Run())
	return c.Process.Pid
}

func newFollower(t *testing.T, runs string) (*ccxFollower, *testNotes) {
	t.Helper()
	n := &testNotes{}
	return &ccxFollower{runs: runs, claims: claimsDir(), pid: os.Getpid(), wait: 2 * time.Second, poll: 20 * time.Millisecond, note: n.add}, n
}

type testNotes struct{ got []string }

func (n *testNotes) add(s string) { n.got = append(n.got, s) }
func (n *testNotes) has(s string) bool {
	return strings.Contains(strings.Join(n.got, "\n"), s)
}

// fixes.test.ts "ccx runs take a launch lock until their own log is
// claimed": the lock is taken before the snapshot, so a log that appears
// while another run holds the lock is never this run's.
func TestCcx_LaunchLockBeforeSnapshot(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	runs := t.TempDir()
	lock := filepath.Join(claimsDir(), "launch.lock")
	must(t, os.MkdirAll(lock, 0o700))
	must(t, os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(startHolder(t))), 0o600))
	f, notes := newFollower(t, runs)
	started := make(chan struct{})
	go func() { f.start(context.Background()); close(started) }()
	time.Sleep(150 * time.Millisecond)
	must(t, os.WriteFile(filepath.Join(runs, "early.jsonl"), []byte("{}\n"), 0o600)) // the holder's log
	must(t, os.RemoveAll(lock))
	<-started
	if !notes.has("waiting for another ccx run to start its log") {
		t.Fatalf("notes %q", notes.got)
	}
	must(t, os.WriteFile(filepath.Join(runs, "mine.jsonl"), []byte("{}\n"), 0o600))
	if got := f.claimOnce(); filepath.Base(got) != "mine.jsonl" {
		t.Fatalf("claimed %q, want mine.jsonl", got)
	}
	f.release()
	if _, err := os.Stat(lock); !os.IsNotExist(err) {
		t.Fatal("the lock outlived the claim")
	}
}

// fixes.test.ts "a ccx run ... claims only when no other log is unbound":
// another run's live unbound mark makes this one mark itself, release the
// lock at once and claim nothing; a dead run's mark is cleared.
func TestCcx_UnboundMarksBlockClaims(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	runs := t.TempDir()
	unb := filepath.Join(claimsDir(), "unbound")
	must(t, os.MkdirAll(unb, 0o700))
	holder := filepath.Join(unb, strconv.Itoa(startHolder(t)))
	must(t, os.WriteFile(holder, nil, 0o600))
	f, notes := newFollower(t, runs)
	f.start(context.Background())
	if f.can || !notes.has("another ccx run has a log not yet bound to its pane: no live log for this run") {
		t.Fatalf("can %v notes %q", f.can, notes.got)
	}
	if _, err := os.Stat(filepath.Join(unb, strconv.Itoa(os.Getpid()))); err != nil {
		t.Fatal("no unbound mark for this run")
	}
	if _, err := os.Stat(filepath.Join(claimsDir(), "launch.lock")); !os.IsNotExist(err) {
		t.Fatal("lock kept after finding an unbound run")
	}
	f.finish()
	if _, err := os.Stat(filepath.Join(unb, strconv.Itoa(os.Getpid()))); !os.IsNotExist(err) {
		t.Fatal("this run's mark outlived its command")
	}
	// a dead run's mark does not block, and is cleared
	must(t, os.Remove(holder))
	must(t, os.WriteFile(filepath.Join(unb, strconv.Itoa(deadPid(t))), nil, 0o600))
	f, _ = newFollower(t, runs)
	f.start(context.Background())
	if entries, _ := os.ReadDir(unb); !f.can || len(entries) != 0 {
		t.Fatalf("a dead run's mark blocked the claim (can %v) or stayed (%d)", f.can, len(entries))
	}
	f.finish()
}

// A run whose log never appears gives up after the wait: it marks itself
// unbound before releasing the lock, and never claims a later log.
func TestCcx_GiveUpMarksUnbound(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	runs := t.TempDir()
	f, notes := newFollower(t, runs)
	f.wait = 100 * time.Millisecond
	f.start(context.Background())
	time.Sleep(150 * time.Millisecond)
	if got := f.poll1(); got != "" || !f.gaveUp || !notes.has("ccx did not start its log within") {
		t.Fatalf("claimed %q gaveUp %v notes %q", got, f.gaveUp, notes.got)
	}
	if _, err := os.Stat(filepath.Join(claimsDir(), "unbound", strconv.Itoa(os.Getpid()))); err != nil {
		t.Fatal("gave up without an unbound mark")
	}
	must(t, os.WriteFile(filepath.Join(runs, "late.jsonl"), []byte("{}\n"), 0o600))
	if got := f.poll1(); got != "" {
		t.Fatalf("claimed %q after giving up", got)
	}
	f.finish()
}

// fixes.test.ts "only one run at a time breaks a dead holder's lock, after
// checking its pid again": a dead holder's lock is broken; a live one's is not.
func TestCcx_BreaksDeadHolderLock(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	lock := filepath.Join(claimsDir(), "launch.lock")
	must(t, os.MkdirAll(lock, 0o700))
	must(t, os.WriteFile(filepath.Join(lock, "pid"), []byte(strconv.Itoa(deadPid(t))), 0o600))
	f, _ := newFollower(t, t.TempDir())
	f.start(context.Background())
	if b, _ := os.ReadFile(filepath.Join(lock, "pid")); strings.TrimSpace(string(b)) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock pid %q: the dead holder's lock was not taken over", b)
	}
	if _, err := os.Stat(lock + ".break"); !os.IsNotExist(err) {
		t.Fatal("the .break folder was left behind")
	}
	f.finish()
}

// fixes.test.ts "the ccx claims folder is per OS user, and an unusable one
// stops the wait": the folder is named by uid with mode 0700; when it cannot
// be used the run claims nothing at once.
func TestCcx_PerUserClaimsFolder(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("TMPDIR", tmp)
	if want := filepath.Join(tmp, "codex-herdr-ccx-claims-"+strconv.Itoa(os.Getuid())); claimsDir() != want {
		t.Fatalf("claims folder %q, want %q", claimsDir(), want)
	}
	f, _ := newFollower(t, t.TempDir())
	f.start(context.Background())
	fi, err := os.Stat(claimsDir())
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("claims folder %v %v", fi, err)
	}
	f.finish()
	must(t, os.RemoveAll(claimsDir()))
	must(t, os.WriteFile(claimsDir(), nil, 0o600)) // a file where the folder goes
	f, notes := newFollower(t, t.TempDir())
	done := make(chan struct{})
	go func() { f.start(context.Background()); close(done) }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("an unusable claims folder made the run wait")
	}
	if f.can || !notes.has("ccx claims folder not usable") {
		t.Fatalf("can %v notes %q", f.can, notes.got)
	}
}
