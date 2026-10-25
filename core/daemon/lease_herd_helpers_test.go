package daemon

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"testing"
	"time"
)

// herd is a set of test-child servers started together on one project. The
// test only ever signals processes in its own herd.
type herd struct {
	t    *testing.T
	root string
	mu   sync.Mutex
	cmds map[int]*exec.Cmd
	done map[int]chan struct{}
	code map[int]int
}

// newHerdRoot makes a short project root (socket paths have a length cap).
func newHerdRoot(t *testing.T) string {
	t.Helper()
	root, err := os.MkdirTemp("", "lh")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(root) })
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

// startHerd starts n children that block on the barrier, then releases them
// together. extraEnv is added to each child's environment.
func startHerd(t *testing.T, root string, n int, idle time.Duration, extraEnv ...string) *herd {
	t.Helper()
	h := &herd{t: t, root: root, cmds: map[int]*exec.Cmd{}, done: map[int]chan struct{}{}, code: map[int]int{}}
	_ = os.Remove(barrierPath(root))
	for i := 0; i < n; i++ {
		h.spawn(idle, extraEnv)
	}
	t.Cleanup(h.stop)
	if err := os.WriteFile(barrierPath(root), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	return h
}

func (h *herd) spawn(idle time.Duration, extraEnv []string) {
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	cmd.Env = append(os.Environ(), childModeEnv+"=serve", childRootEnv+"="+h.root,
		childIdleEnv+"="+idle.String())
	cmd.Env = append(cmd.Env, extraEnv...)
	if err := cmd.Start(); err != nil {
		h.t.Fatalf("start child: %v", err)
	}
	pid := cmd.Process.Pid
	ch := make(chan struct{})
	h.mu.Lock()
	h.cmds[pid], h.done[pid] = cmd, ch
	h.mu.Unlock()
	go func() {
		_ = cmd.Wait()
		h.mu.Lock()
		h.code[pid] = cmd.ProcessState.ExitCode()
		h.mu.Unlock()
		close(ch)
	}()
}

// alive returns the pids of children that have not exited.
func (h *herd) alive() []int {
	h.mu.Lock()
	defer h.mu.Unlock()
	var out []int
	for pid, ch := range h.done {
		select {
		case <-ch:
		default:
			out = append(out, pid)
		}
	}
	return out
}

// waitAlive waits until at most want children are alive or budget ends, and
// returns the pids still alive.
func (h *herd) waitAlive(want int, budget time.Duration) []int {
	deadline := time.Now().Add(budget)
	for {
		alive := h.alive()
		if len(alive) <= want || time.Now().After(deadline) {
			return alive
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// waitExit waits for one child to exit and reports whether it did.
func (h *herd) waitExit(pid int, budget time.Duration) bool {
	h.mu.Lock()
	ch := h.done[pid]
	h.mu.Unlock()
	select {
	case <-ch:
		return true
	case <-time.After(budget):
		return false
	}
}

// stop sends SIGTERM to every child of this herd that is still alive and
// waits for it. It never signals any other process.
func (h *herd) stop() {
	for _, pid := range h.alive() {
		h.mu.Lock()
		cmd := h.cmds[pid]
		h.mu.Unlock()
		_ = cmd.Process.Signal(syscall.SIGTERM)
		if !h.waitExit(pid, 5*time.Second) {
			_ = cmd.Process.Kill()
			h.waitExit(pid, 5*time.Second)
		}
	}
}

// socketOwnerPID pings the project socket and returns the serving pid, or 0.
func socketOwnerPID(root string) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	res, err := NewReadClient(root).Ping(ctx)
	if err != nil {
		return 0
	}
	return res.PID
}

// waitSocketOwner waits for the socket to answer and returns its pid.
func waitSocketOwner(root string, budget time.Duration) int {
	deadline := time.Now().Add(budget)
	for time.Now().Before(deadline) {
		if pid := socketOwnerPID(root); pid != 0 {
			return pid
		}
		time.Sleep(20 * time.Millisecond)
	}
	return 0
}

// assertOneServer checks the herd property: exactly one child survives, it
// holds the lease, and it serves the socket.
func assertOneServer(t *testing.T, h *herd, round int) {
	t.Helper()
	sockPID := waitSocketOwner(h.root, 10*time.Second)
	h.waitAlive(1, 5*time.Second)
	time.Sleep(300 * time.Millisecond) // let any late duplicate show itself
	alive := h.alive()
	leasePID := leaseOwnerPIDForTest(h.root)
	if len(alive) != 1 || sockPID != alive[0] || leasePID != alive[0] {
		t.Fatalf("round %d: %d servers alive %v; socket pid %d; lease pid %d; want exactly 1 that holds both",
			round, len(alive), alive, sockPID, leasePID)
	}
}
