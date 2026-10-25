package oneshot

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/render"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ccx writes its own stream-json run log into its runs folder; its stdout
// is a status line and the result. The follower finds the run's log: a
// *.jsonl that was not there when the run started and that no other run has
// claimed (an atomic mkdir per log name), so overlapping runs never follow
// the same log and no mtime is compared.
//
// The protocol is the mod's (panescript.ts CCX_RUN), on the same per-user
// claims folder, so a delegate run and a mod run on one machine coordinate:
// a run may claim only while it holds the launch lock and no other run is
// unbound (in flight, its log not yet claimed). A holder releases the lock
// once it has its log, or after the wait, marking itself unbound first so its
// late log is never taken. A run that finds an unbound one marks itself,
// releases at once and claims nothing. A dead holder's lock is broken by its
// pid, by one run at a time.

// DefaultCcxWait is how long a run waits for its log (the mod's 30 s).
const DefaultCcxWait = 30 * time.Second

// claimsDir is the per-OS-user claims folder, as the mod names it.
func claimsDir() string {
	return filepath.Join(os.TempDir(), "codex-herdr-ccx-claims-"+strconv.Itoa(os.Getuid()))
}

// ccxRunsDir is CCX_RUNS_DIR, else the ccxRunsDir setting; there is no
// machine-specific default.
func ccxRunsDir(setting string) string {
	if v := os.Getenv("CCX_RUNS_DIR"); v != "" {
		return v
	}
	return setting
}

type ccxFollower struct {
	runs, claims string
	pid          int
	wait, poll   time.Duration
	note         func(string)

	held, can, gaveUp bool
	before            map[string]bool
	file              string
	t0                time.Time
}

func (f *ccxFollower) lock() string    { return filepath.Join(f.claims, "launch.lock") }
func (f *ccxFollower) unbDir() string  { return filepath.Join(f.claims, "unbound") }
func (f *ccxFollower) myMark() string  { return filepath.Join(f.unbDir(), strconv.Itoa(f.pid)) }
func (f *ccxFollower) mark()           { _ = os.WriteFile(f.myMark(), nil, 0o600) }
func (f *ccxFollower) lockPid() int    { return readPid(filepath.Join(f.lock(), "pid")) }
func (f *ccxFollower) usable() bool    { return isDir(f.claims) && syscall.Access(f.claims, 2) == nil }
func (f *ccxFollower) claimable() bool { return f.file == "" && !f.gaveUp }

// start takes the launch lock, checks for unbound runs and snapshots the
// logs already there; the command starts after it.
func (f *ccxFollower) start(ctx context.Context) {
	f.t0 = time.Now()
	if f.runs == "" {
		f.note("ccx runs directory not set (set ccxRunsDir or CCX_RUNS_DIR)")
		f.gaveUp = true
		return
	}
	_ = os.MkdirAll(f.claims, 0o700)
	_ = os.MkdirAll(f.unbDir(), 0o700)
	ok := f.usable() && f.acquire(ctx)
	if !ok {
		f.note("ccx claims folder not usable: " + f.claims + ": no live log for this run")
	}
	f.can = ok
	if ok && f.otherUnbound() {
		f.can = false
		f.mark()
		f.release()
		f.note("another ccx run has a log not yet bound to its pane: no live log for this run")
	}
	f.before = jsonlNames(f.runs)
	if !isDir(f.runs) {
		f.note(fmt.Sprintf("ccx runs directory not found: %s (set ccxRunsDir or CCX_RUNS_DIR)", f.runs))
	}
	f.gaveUp = !f.can
}

// acquire waits for the launch lock with no time limit (a claimer's hold is
// bounded by its own wait). A failed mkdir with no lock there is not
// contention: after 50 tries the folder counts as unusable.
func (f *ccxFollower) acquire(ctx context.Context) bool {
	tries, waited := 0, false
	for {
		if err := os.Mkdir(f.lock(), 0o700); err == nil {
			f.held = os.WriteFile(filepath.Join(f.lock(), "pid"), []byte(strconv.Itoa(f.pid)+"\n"), 0o600) == nil
			return f.held
		}
		if !isDir(f.lock()) {
			if tries++; tries < 50 {
				continue
			}
			return false
		}
		if f.breakDead() {
			continue
		}
		if !waited {
			f.note("waiting for another ccx run to start its log")
			waited = true
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(100 * time.Millisecond):
		}
	}
}

// breakDead removes a dead holder's lock; only one run at a time may (the
// .break folder), and it reads the pid again first, so a successor's lock
// is never removed.
func (f *ccxFollower) breakDead() bool {
	brk := f.lock() + ".break"
	p := f.lockPid()
	if p > 0 && !alive(p) && os.Mkdir(brk, 0o700) == nil {
		if f.lockPid() == p {
			_ = os.RemoveAll(f.lock())
		}
		_ = os.Remove(brk)
		return true
	}
	if fi, err := os.Stat(brk); err == nil && time.Since(fi.ModTime()) > time.Minute {
		_ = os.Remove(brk)
	}
	return false
}

func (f *ccxFollower) release() {
	if f.lockPid() == f.pid {
		_ = os.RemoveAll(f.lock())
	}
}

// otherUnbound reports a live unbound run; dead runs' marks are removed.
func (f *ccxFollower) otherUnbound() bool {
	entries, _ := os.ReadDir(f.unbDir())
	for _, e := range entries {
		p, err := strconv.Atoi(e.Name())
		if err == nil && alive(p) {
			return true
		}
		_ = os.Remove(filepath.Join(f.unbDir(), e.Name()))
	}
	return false
}

// claimOnce claims the first new log no other run has claimed.
func (f *ccxFollower) claimOnce() string {
	for _, name := range sortedNames(jsonlNames(f.runs)) {
		if f.before[name] {
			continue
		}
		if os.Mkdir(filepath.Join(f.claims, name), 0o700) == nil {
			return filepath.Join(f.runs, name)
		}
	}
	return ""
}

// poll1 is one claim step of the follower loop; it returns the claimed log.
func (f *ccxFollower) poll1() string {
	if f.claimable() {
		f.file = f.claimOnce()
	}
	if f.file != "" {
		f.release()
	}
	if f.claimable() && time.Since(f.t0) >= f.wait {
		f.mark()
		f.release()
		f.gaveUp = true
		f.note(fmt.Sprintf("ccx did not start its log within %d s: no live log for this run", int(f.wait/time.Second)))
	}
	return f.file
}

// finish runs once the command has ended.
func (f *ccxFollower) finish() {
	f.release()
	_ = os.Remove(f.myMark())
}

// follow renders the claimed log's complete lines as they come, until done
// closes; then the rest, a trailing partial line and unanswered calls too.
func (f *ccxFollower) follow(done <-chan struct{}, l *runLogs) {
	sf := render.NewCcxFilter()
	var off int64
	for {
		final := isClosed(done)
		if path := f.poll1(); path != "" {
			off = renderNew(path, off, final, sf, l)
		}
		if final {
			_, _ = l.md.WriteString(sf.Close())
			break
		}
		time.Sleep(f.poll)
	}
}

func renderNew(path string, off int64, final bool, sf *render.StreamFilter, l *runLogs) int64 {
	data, err := os.ReadFile(path)
	if err != nil || int64(len(data)) <= off {
		return off
	}
	chunk := data[off:]
	if !final {
		chunk = chunk[:bytes.LastIndexByte(chunk, '\n')+1]
	}
	for _, line := range strings.SplitAfter(string(chunk), "\n") {
		if line == "" {
			continue
		}
		log, md := sf.Line(strings.TrimSuffix(line, "\n"))
		_, _ = l.log.WriteString(log)
		_, _ = l.md.WriteString(md)
	}
	return off + int64(len(chunk))
}

// runCcx runs ccx task: stdout to out.txt, stderr to raw.log (the mod's
// err.log, copied there), the follower beside it; last.md is the stdout with
// its status line quoted.
func runCcx(ctx context.Context, rf *runstore.RunFolder, job Job) int {
	l, err := openLogs(rf)
	if err != nil {
		return 1
	}
	defer l.close()
	f := &ccxFollower{runs: job.CcxRuns, claims: claimsDir(), pid: os.Getpid(), wait: time.Duration(job.CcxWaitMS) * time.Millisecond,
		poll: 300 * time.Millisecond, note: l.note}
	if f.wait <= 0 {
		f.wait = DefaultCcxWait
	}
	f.start(ctx)
	defer f.finish()
	out, err := os.OpenFile(filepath.Join(rf.Dir(), runstore.FileOut), os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return startFailed(l, err)
	}
	defer out.Close()
	cmd := command(ctx, job)
	cmd.Stdout, cmd.Stderr = out, l.raw
	if err := cmd.Start(); err != nil {
		return startFailed(l, err)
	}
	done, followed := make(chan struct{}), make(chan struct{})
	go func() { f.follow(done, l); close(followed) }()
	exit := exitOf(cmd.Wait(), cmd.ProcessState)
	close(done)
	<-followed
	stdout, _, _ := rf.ReadFrom(runstore.FileOut, 0)
	_ = rf.WriteFile(runstore.FileLastMD, []byte(render.Ccx(nil, stdout).Last))
	return exit
}

func isClosed(c <-chan struct{}) bool {
	select {
	case <-c:
		return true
	default:
		return false
	}
}

func readPid(path string) int {
	b, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// alive is kill -0, as the mod checks a pid.
func alive(pid int) bool { return pid > 0 && syscall.Kill(pid, 0) == nil }

func isDir(path string) bool {
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

func jsonlNames(dir string) map[string]bool {
	names := map[string]bool{}
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".jsonl") {
			names[e.Name()] = true
		}
	}
	return names
}

func sortedNames(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// sweepClaims removes ccx claims older than keep, as the mod's sweep does.
func sweepClaims(keep time.Duration, now time.Time) {
	dir := claimsDir()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if fi, err := e.Info(); err == nil && now.Sub(fi.ModTime()) > keep {
			_ = os.RemoveAll(filepath.Join(dir, e.Name()))
		}
	}
}
