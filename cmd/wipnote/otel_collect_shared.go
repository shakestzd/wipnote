package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/collector"
	"github.com/shakestzd/wipnote/observe/otel/sink/ndjson"
)

// Shared collector timing. Variables so tests can shorten them.
var (
	// sharedTick is how often the shared collector prunes dead launchers,
	// closes idle session files and checks it still owns the lease.
	sharedTick = 2 * time.Second
	// sharedEmptyGrace is how long the collector stays up with no live
	// launcher attached, which also covers the gap between handshake and the
	// first attach.
	sharedEmptyGrace = 30 * time.Second
	// sharedSinkIdle closes a session's events.ndjson after this long without
	// traffic; the next request reopens it in append mode.
	sharedSinkIdle = 10 * time.Minute
)

// runSharedOtelCollect serves every session of projectDir on one port
// (issue #183). Sessions export to /s/<session-id>/v1/<signal>. It exits on
// SIGTERM, when no attached launcher has been alive for sharedEmptyGrace, or
// when the lease names another live collector.
func runSharedOtelCollect(projectDir, listenAddr string) error {
	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", listenAddr, err)
	}
	sinks := newSessionSinks(projectDir, ln.Addr().(*net.TCPAddr).Port)
	attached := newAttachSet(time.Now(), collector.PIDAlive)
	srv := &http.Server{
		Handler:           buildSharedMux(sinks, attached),
		ReadHeaderTimeout: 10 * time.Second,
	}
	if _, err := fmt.Fprintf(os.Stdout, "wipnote-otel-ready port=%d\n", sinks.port); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	_ = os.Stdout.Sync()
	// It outlives the launcher that started it: a hangup or a closed pipe
	// from that launcher must not take every other session's telemetry down.
	signal.Ignore(syscall.SIGHUP, syscall.SIGPIPE)
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "otel-collect --shared serve: %v\n", err)
		}
	}()
	return awaitSharedShutdown(srv, sinks, attached, projectDir)
}

// buildSharedMux routes /s/<sid>/v1/* to that session's sink and serves the
// health and attach endpoints.
func buildSharedMux(sinks *sessionSinks, attached *attachSet) *http.ServeMux {
	base := &collectorHandler{registry: newAdapterRegistry(), lastActivity: &atomic.Int64{}}
	mux := http.NewServeMux()
	mux.Handle(collector.HealthPath, healthHandler(func() collector.Health {
		return collector.Health{PID: os.Getpid(), Mode: "shared", Sessions: sinks.open(), Attached: attached.count()}
	}))
	mux.HandleFunc(collector.AttachPath, launcherPIDHandler(attached.add, nil))
	mux.HandleFunc(collector.DetachPath, launcherPIDHandler(func(pid int) { attached.remove(pid) }, sinks.closeSession))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		sid, rest, ok := collector.SplitSessionPath(r.URL.Path)
		if !ok {
			http.Error(w, "shared collector: export to /s/<session-id>", http.StatusNotFound)
			return
		}
		snk, err := sinks.get(sid)
		if err != nil {
			http.Error(w, "open session log failed", http.StatusInternalServerError)
			return
		}
		base.serveSignal(w, r, rest, snk)
	})
	return mux
}

// launcherPIDHandler applies fn to the ?pid= of a POST, and onSession (when
// set) to a valid ?session=, so a detaching launcher's session log is flushed
// and closed at once.
func launcherPIDHandler(fn func(int), onSession func(string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		pid, err := strconv.Atoi(r.URL.Query().Get("pid"))
		if err != nil || pid <= 0 {
			http.Error(w, "pid required", http.StatusBadRequest)
			return
		}
		fn(pid)
		if sid := r.URL.Query().Get("session"); onSession != nil && collector.ValidSessionID(sid) {
			onSession(sid)
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func awaitSharedShutdown(srv *http.Server, sinks *sessionSinks, attached *attachSet, projectDir string) error {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	ticker := time.NewTicker(sharedTick)
	defer ticker.Stop()
	for {
		select {
		case <-sigCh:
			return shutdownShared(srv, sinks)
		case now := <-ticker.C:
			if attached.emptyFor(now) >= sharedEmptyGrace || supersededShared(projectDir) {
				return shutdownShared(srv, sinks)
			}
			sinks.closeIdle(now, sharedSinkIdle)
		}
	}
}

// supersededShared reports whether the lease names another live collector,
// so a collector that lost the lease (for example after a launch judged it
// dead) steps aside instead of running beside its replacement.
func supersededShared(projectDir string) bool {
	rec, err := collector.ReadSharedRecord(projectDir)
	return err == nil && rec.PID != os.Getpid() && collector.PIDAlive(rec.PID)
}

func shutdownShared(srv *http.Server, sinks *sessionSinks) error {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	sinks.closeAll()
	return nil
}

// attachSet is the set of launcher pids the shared collector serves.
type attachSet struct {
	mu         sync.Mutex
	pids       map[int]bool
	emptySince time.Time
	alive      func(int) bool
}

func newAttachSet(now time.Time, alive func(int) bool) *attachSet {
	return &attachSet{pids: map[int]bool{}, emptySince: now, alive: alive}
}

func (a *attachSet) add(pid int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pids[pid] = true
}

func (a *attachSet) remove(pid int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.pids, pid)
	if len(a.pids) == 0 {
		a.emptySince = time.Now()
	}
}

func (a *attachSet) count() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.pids)
}

// emptyFor drops launchers that are no longer alive and returns how long the
// set has had none (0 while any is attached).
func (a *attachSet) emptyFor(now time.Time) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	hadAny := len(a.pids) > 0
	for pid := range a.pids {
		if !a.alive(pid) {
			delete(a.pids, pid)
		}
	}
	if len(a.pids) > 0 {
		return 0
	}
	if hadAny {
		a.emptySince = now
	}
	return now.Sub(a.emptySince)
}

// sessionSinks holds one open events.ndjson sink per active session.
type sessionSinks struct {
	projectDir string
	port       int
	mu         sync.Mutex
	bySession  map[string]*sessionSink
	started    map[string]bool
}

type sessionSink struct {
	snk  *ndjson.Sink
	last time.Time
}

func newSessionSinks(projectDir string, port int) *sessionSinks {
	return &sessionSinks{projectDir: projectDir, port: port, bySession: map[string]*sessionSink{}, started: map[string]bool{}}
}

// get returns sid's sink, opening it (and writing its collector_start event
// the first time this process sees sid) when needed.
func (s *sessionSinks) get(sid string) (*ndjson.Sink, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.bySession[sid]; ok {
		e.last = time.Now()
		return e.snk, nil
	}
	if err := os.MkdirAll(filepath.Join(s.projectDir, ".wipnote", "sessions", sid), 0o755); err != nil {
		return nil, err
	}
	snk, err := ndjson.New(s.projectDir, sid)
	if err != nil {
		return nil, err
	}
	if !s.started[sid] {
		s.started[sid] = true
		if err := writeCollectorStartEvent(snk, sid, s.port); err == nil {
			_ = snk.Flush()
		}
	}
	s.bySession[sid] = &sessionSink{snk: snk, last: time.Now()}
	return snk, nil
}

func (s *sessionSinks) open() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.bySession)
}

// closeIdle closes sinks with no traffic for maxIdle.
func (s *sessionSinks) closeIdle(now time.Time, maxIdle time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for sid, e := range s.bySession {
		if now.Sub(e.last) >= maxIdle {
			_ = e.snk.Close()
			delete(s.bySession, sid)
		}
	}
}

// closeSession flushes and closes sid's sink; a later request reopens it.
func (s *sessionSinks) closeSession(sid string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if e, ok := s.bySession[sid]; ok {
		_ = e.snk.Close()
		delete(s.bySession, sid)
	}
}

func (s *sessionSinks) closeAll() {
	s.closeIdle(time.Now().Add(time.Hour*24*365), 0)
}
