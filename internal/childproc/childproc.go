// Package childproc supervises per-project HtmlGraph server child processes.
//
// The parent HtmlGraph server ("htmlgraph serve") owns exactly one
// Supervisor. When a request arrives at /p/<id>/..., the parent asks the
// supervisor to GetOrSpawn a child for that project; the supervisor either
// returns an existing warm child or forks `htmlgraph _serve-child` as a
// new process, reads the handshake line ("htmlgraph-serve-ready port=N
// pid=P") to discover the ephemeral port, and stores a pre-built
// httputil.ReverseProxy pointing at 127.0.0.1:<N>.
//
// # Concurrency model
//
// Per-project cold spawns use sync.Once to ensure that concurrent requests
// for the same new project join a single exec.Command call instead of
// racing to spawn multiple children for the same project. Warm lookups
// take the supervisor mutex only briefly to read the map.
//
// # Lifecycle
//
// Each child has two goroutines owned by the supervisor:
//
//  1. A stdout drain that reads the handshake line and then copies the
//     rest of stdout to io.Discard. The child also redirects its own
//     stdout to a log file immediately after the handshake — this drain
//     is belt-and-suspenders so the pipe buffer never fills and blocks
//     the child.
//
//  2. A cmd.Wait reaper that closes exitC and removes the child from the
//     supervisor map when the process exits. The reaper is the single
//     point of truth for child liveness; crash recovery is triggered by
//     the next GetOrSpawn call observing an absent entry.
//
// A RunIdleReaper goroutine ticks every 60s and SIGTERMs any child whose
// LastRequest atomic is older than idleTimeout.
//
// # Crash recovery
//
// If a child dies (segfault, OOM, explicit SIGKILL), the reaper goroutine
// removes its entry from the map. The next request through the proxy
// calls GetOrSpawn, which observes the missing entry and spawns a fresh
// child. No per-request health check is required.
package childproc

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"
)

// Options configures a Supervisor.
type Options struct {
	// BinPath is the absolute path to the htmlgraph binary. If empty, the
	// supervisor uses os.Executable() so it forks its own image. Tests
	// override this to point at a test-built binary.
	BinPath string
	// IdleTimeout is how long a child can sit without handling a request
	// before the idle reaper kills it. Zero disables idle reaping.
	IdleTimeout time.Duration
	// SpawnTimeout bounds the handshake read. A child that fails to print
	// the ready line within this window is killed and GetOrSpawn returns
	// an error.
	SpawnTimeout time.Duration
}

// DefaultIdleTimeout is the IdleTimeout used when Options.IdleTimeout is
// zero.
const DefaultIdleTimeout = 10 * time.Minute

// DefaultSpawnTimeout is the SpawnTimeout used when Options.SpawnTimeout
// is zero.
const DefaultSpawnTimeout = 5 * time.Second

// Child represents one running htmlgraph _serve-child process.
type Child struct {
	ID          string
	ProjectDir  string
	Port        int
	PID         int
	LastRequest atomic.Int64 // unix seconds
	Proxy       *httputil.ReverseProxy

	cmd    *exec.Cmd
	cancel context.CancelFunc
	exitC  chan struct{}
}

// Supervisor owns the child process map and spawn lifecycle.
type Supervisor struct {
	mu          sync.Mutex
	children    map[string]*Child
	spawnGroups map[string]*sync.Once

	binPath      string
	idleTimeout  time.Duration
	spawnTimeout time.Duration
}

// NewSupervisor constructs a Supervisor with the given options.
func NewSupervisor(opts Options) *Supervisor {
	binPath := opts.BinPath
	if binPath == "" {
		if exe, err := os.Executable(); err == nil {
			binPath = exe
		}
	}
	idle := opts.IdleTimeout
	if idle == 0 {
		idle = DefaultIdleTimeout
	}
	spawn := opts.SpawnTimeout
	if spawn == 0 {
		spawn = DefaultSpawnTimeout
	}
	return &Supervisor{
		children:     make(map[string]*Child),
		spawnGroups:  make(map[string]*sync.Once),
		binPath:      binPath,
		idleTimeout:  idle,
		spawnTimeout: spawn,
	}
}

// handshakeRE matches exactly the line the _serve-child subcommand prints
// as its first and only handshake output:
//
//	htmlgraph-serve-ready port=<N> pid=<P>
//
// Any prior or intermixed output is a bug in the child — the scanner
// fails closed.
var handshakeRE = regexp.MustCompile(`^htmlgraph-serve-ready port=(\d+) pid=(\d+)$`)

// GetOrSpawn returns an existing warm Child for projectID or spawns a new
// one. Concurrent callers for the same projectID share a single
// exec.Command invocation via sync.Once.
func (s *Supervisor) GetOrSpawn(ctx context.Context, projectID, projectDir string) (*Child, error) {
	// Fast path: warm child.
	s.mu.Lock()
	if c, ok := s.children[projectID]; ok {
		s.mu.Unlock()
		c.LastRequest.Store(time.Now().Unix())
		return c, nil
	}
	// Acquire (or reuse) the per-project spawn gate.
	once, ok := s.spawnGroups[projectID]
	if !ok {
		once = &sync.Once{}
		s.spawnGroups[projectID] = once
	}
	s.mu.Unlock()

	var spawnErr error
	once.Do(func() {
		child, err := s.spawnLocked(ctx, projectID, projectDir)
		if err != nil {
			spawnErr = err
			// Clear the sync.Once so the next caller retries. sync.Once
			// itself is not resettable — we delete the map entry so the
			// next GetOrSpawn allocates a fresh one.
			s.mu.Lock()
			delete(s.spawnGroups, projectID)
			s.mu.Unlock()
			return
		}
		s.mu.Lock()
		s.children[projectID] = child
		delete(s.spawnGroups, projectID)
		s.mu.Unlock()
	})
	if spawnErr != nil {
		return nil, spawnErr
	}

	s.mu.Lock()
	c, ok := s.children[projectID]
	s.mu.Unlock()
	if !ok {
		// Another goroutine's Once completed but the child already died.
		// Retry once.
		return s.GetOrSpawn(ctx, projectID, projectDir)
	}
	c.LastRequest.Store(time.Now().Unix())
	return c, nil
}

// spawnLocked forks htmlgraph _serve-child for the given project. The
// caller must NOT hold s.mu — this function takes child-level locks only.
func (s *Supervisor) spawnLocked(ctx context.Context, projectID, projectDir string) (*Child, error) {
	if s.binPath == "" {
		return nil, errors.New("childproc: BinPath is empty and os.Executable() failed")
	}

	// Lifetime context — cancelling this kills the child.
	childCtx, cancel := context.WithCancel(context.Background())

	// Arg order is load-bearing: --project-dir is a persistent root flag
	// and must appear BEFORE the subcommand, not after.
	cmd := exec.CommandContext(childCtx, s.binPath,
		"--project-dir", projectDir,
		"_serve-child",
		"--port", "0",
	)

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		cancel()
		return nil, fmt.Errorf("stdout pipe: %w", err)
	}
	// Discard stderr — the child also redirects its own stderr after the
	// handshake, but prior to that a failed exec could write here.
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("start child: %w", err)
	}

	// Read the handshake line synchronously with a deadline.
	handshakeCtx, handshakeCancel := context.WithTimeout(ctx, s.spawnTimeout)
	defer handshakeCancel()

	type handshakeResult struct {
		port int
		pid  int
		err  error
	}
	hsC := make(chan handshakeResult, 1)

	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 64*1024)

	go func() {
		if !scanner.Scan() {
			if err := scanner.Err(); err != nil {
				hsC <- handshakeResult{err: fmt.Errorf("scan handshake: %w", err)}
				return
			}
			hsC <- handshakeResult{err: errors.New("child closed stdout before handshake")}
			return
		}
		line := scanner.Text()
		m := handshakeRE.FindStringSubmatch(line)
		if m == nil {
			hsC <- handshakeResult{err: fmt.Errorf("invalid handshake: %q", line)}
			return
		}
		port, _ := strconv.Atoi(m[1])
		pid, _ := strconv.Atoi(m[2])
		hsC <- handshakeResult{port: port, pid: pid}
	}()

	var hs handshakeResult
	select {
	case hs = <-hsC:
	case <-handshakeCtx.Done():
		_ = cmd.Process.Kill()
		cancel()
		return nil, fmt.Errorf("handshake timeout after %s", s.spawnTimeout)
	}
	if hs.err != nil {
		_ = cmd.Process.Kill()
		cancel()
		return nil, hs.err
	}

	// Continue draining stdout in the background so the child never
	// blocks on a full pipe. The child should redirect its own stdout
	// to a log file immediately after the handshake, but if it does not,
	// this goroutine keeps reads draining.
	go func() {
		_, _ = io.Copy(io.Discard, stdout)
	}()

	// Build the reverse proxy.
	target, _ := url.Parse(fmt.Sprintf("http://127.0.0.1:%d", hs.port))
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = 100 * time.Millisecond // SSE-friendly
	proxy.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, err error) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = fmt.Fprintf(w, "child unreachable: %v", err)
	}
	// ModifyResponse rewrites absolute-path Location headers so that
	// 3xx redirects from the child preserve the /p/<id>/ project prefix.
	// Without this, a 301 from the child (e.g. "/plans" → "/plans/")
	// sends the browser to the bare path ("/plans/") at the parent server,
	// stripping the project context and causing subsequent API calls to
	// hit /api/plans (404) instead of /p/<id>/api/plans.
	proxy.ModifyResponse = func(resp *http.Response) error {
		loc := resp.Header.Get("Location")
		if loc != "" && strings.HasPrefix(loc, "/") && !strings.HasPrefix(loc, "/p/") {
			resp.Header.Set("Location", "/p/"+projectID+loc)
		}
		return nil
	}

	child := &Child{
		ID:         projectID,
		ProjectDir: projectDir,
		Port:       hs.port,
		PID:        hs.pid,
		Proxy:      proxy,
		cmd:        cmd,
		cancel:     cancel,
		exitC:      make(chan struct{}),
	}
	child.LastRequest.Store(time.Now().Unix())

	// Reaper goroutine: waits for the process and removes it from the
	// supervisor map when it exits (normal shutdown, crash, idle reap).
	go func() {
		_ = cmd.Wait()
		close(child.exitC)
		s.mu.Lock()
		if existing, ok := s.children[projectID]; ok && existing == child {
			delete(s.children, projectID)
		}
		s.mu.Unlock()
		cancel()
	}()

	return child, nil
}

// Shutdown SIGTERMs all children in parallel, waits with a deadline, then
// SIGKILLs any survivors. Safe to call multiple times.
func (s *Supervisor) Shutdown(ctx context.Context) {
	s.mu.Lock()
	children := make([]*Child, 0, len(s.children))
	for _, c := range s.children {
		children = append(children, c)
	}
	s.mu.Unlock()

	var wg sync.WaitGroup
	for _, c := range children {
		wg.Add(1)
		go func(c *Child) {
			defer wg.Done()
			_ = c.cmd.Process.Signal(syscall.SIGTERM)
			select {
			case <-c.exitC:
			case <-ctx.Done():
				_ = c.cmd.Process.Kill()
				<-c.exitC
			}
		}(c)
	}
	wg.Wait()
}

// RunIdleReaper ticks every 60s (or more frequently if idleTimeout is
// short, for tests) and kills any child whose LastRequest is older than
// idleTimeout. Blocks until ctx is cancelled.
func (s *Supervisor) RunIdleReaper(ctx context.Context) {
	tick := 60 * time.Second
	if s.idleTimeout > 0 && s.idleTimeout/4 < tick {
		tick = s.idleTimeout / 4
		if tick < 10*time.Millisecond {
			tick = 10 * time.Millisecond
		}
	}
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.reapIdleOnce()
		}
	}
}

// reapIdleOnce inspects all children and kills any whose LastRequest is
// older than idleTimeout. Exported for tests that want to trigger a single
// pass without running the goroutine.
func (s *Supervisor) reapIdleOnce() {
	if s.idleTimeout == 0 {
		return
	}
	cutoff := time.Now().Add(-s.idleTimeout).Unix()

	s.mu.Lock()
	var toKill []*Child
	for _, c := range s.children {
		if c.LastRequest.Load() < cutoff {
			toKill = append(toKill, c)
		}
	}
	s.mu.Unlock()

	for _, c := range toKill {
		_ = c.cmd.Process.Signal(syscall.SIGTERM)
		// Escalate to SIGKILL if the child hasn't exited within 100ms.
		// SIGTERM may be delivered cleanly but cmd.Wait can block on stdout pipe
		// drain under CPU contention (CI runners), preventing map cleanup. SIGKILL
		// forces immediate process death and pipe close.
		go func(c *Child) {
			select {
			case <-c.exitC:
				// Process exited cleanly; nothing to do.
			case <-time.After(100 * time.Millisecond):
				_ = c.cmd.Process.Kill()
			}
		}(c)
	}
}

// Children returns a snapshot of the current child map. For tests and
// observability. Do not mutate the returned slice.
func (s *Supervisor) Children() []*Child {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*Child, 0, len(s.children))
	for _, c := range s.children {
		out = append(out, c)
	}
	return out
}
