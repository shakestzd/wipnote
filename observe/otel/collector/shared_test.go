package collector

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// fakeShared is an in-process stand-in for a shared otel-collect child. It
// answers /healthz with the pid it claims and records /attach and /detach.
type fakeShared struct {
	srv      *http.Server
	port     int
	pid      int
	attached sync.Map
	// detachedSession is the ?session= of the last detach.
	detachedSession atomic.Value
	// healthDelay stalls each /healthz answer, standing in for a busy collector.
	healthDelay atomic.Int64
}

func startFakeShared(t *testing.T, requestedPort, pid int) (*fakeShared, error) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(requestedPort))
	if err != nil {
		return nil, err
	}
	f := &fakeShared{port: ln.Addr().(*net.TCPAddr).Port, pid: pid}
	mux := http.NewServeMux()
	mux.HandleFunc(HealthPath, func(w http.ResponseWriter, _ *http.Request) {
		time.Sleep(time.Duration(f.healthDelay.Load()))
		_ = json.NewEncoder(w).Encode(Health{PID: f.pid, Mode: "shared"})
	})
	mux.HandleFunc(AttachPath, func(w http.ResponseWriter, r *http.Request) {
		f.attached.Store(r.URL.Query().Get("pid"), true)
	})
	mux.HandleFunc(DetachPath, func(w http.ResponseWriter, r *http.Request) {
		f.attached.Delete(r.URL.Query().Get("pid"))
		f.detachedSession.Store(r.URL.Query().Get("session"))
	})
	f.srv = &http.Server{Handler: mux}
	go func() { _ = f.srv.Serve(ln) }()
	t.Cleanup(func() { _ = f.srv.Close() })
	return f, nil
}

// countingSpawn returns a SharedSpawnFn that starts a fakeShared claiming this
// test process's pid, and counts how often it ran.
func countingSpawn(t *testing.T, calls *atomic.Int32, last **fakeShared) SharedSpawnFn {
	var mu sync.Mutex
	return func(_, _ string, requestedPort int) (int, *os.Process, error) {
		calls.Add(1)
		f, err := startFakeShared(t, requestedPort, os.Getpid())
		if err != nil {
			return 0, nil, err
		}
		mu.Lock()
		*last = f
		mu.Unlock()
		self, _ := os.FindProcess(os.Getpid())
		return f.port, self, nil
	}
}

func mkSharedProject(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// Issue #183: 22 sessions spawned 22 collectors. Every launch in one project
// must reuse the one live shared collector.
func TestEnsureShared_ManyLaunchesSpawnOnce(t *testing.T) {
	project := mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	spawn := countingSpawn(t, &calls, &last)

	var wg sync.WaitGroup
	ports := make([]int, 22)
	errs := make([]error, 22)
	for i := range ports {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			rec, err := EnsureShared("wipnote", project, spawn)
			ports[i], errs[i] = rec.Port, err
		}(i)
	}
	wg.Wait()

	if n := calls.Load(); n != 1 {
		t.Fatalf("22 launches spawned %d collectors, want 1", n)
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("launch %d: %v", i, err)
		}
		if ports[i] != last.port {
			t.Fatalf("launch %d got port %d, want shared port %d", i, ports[i], last.port)
		}
	}
	rec, err := ReadSharedRecord(project)
	if err != nil || rec.Port != last.port || rec.PID != os.Getpid() {
		t.Fatalf("lease record = %+v, %v", rec, err)
	}
}

// A dead shared collector is replaced on the SAME port, so sessions already
// pointed at it keep exporting to a live endpoint.
func TestEnsureShared_DeadCollectorRespawnsOnSamePort(t *testing.T) {
	project := mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	spawn := countingSpawn(t, &calls, &last)

	first, err := EnsureShared("wipnote", project, spawn)
	if err != nil {
		t.Fatal(err)
	}
	_ = last.srv.Close() // the collector dies; health probe now fails

	second, err := EnsureShared("wipnote", project, spawn)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("spawn calls = %d, want 2 (one respawn)", calls.Load())
	}
	if second.Port != first.Port {
		t.Fatalf("respawn moved the port %d -> %d", first.Port, second.Port)
	}
}

// A live process on the recorded port that reports another pid is not ours:
// the lease is not trusted and a fresh collector is spawned.
func TestEnsureShared_HealthPIDMismatchIsNotReused(t *testing.T) {
	project := mkSharedProject(t)
	impostor, err := startFakeShared(t, 0, os.Getpid()+1)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSharedRecord(project, SharedRecord{PID: os.Getpid(), Port: impostor.port}); err != nil {
		t.Fatal(err)
	}
	var calls atomic.Int32
	var last *fakeShared
	rec, err := EnsureShared("wipnote", project, countingSpawn(t, &calls, &last))
	if err != nil {
		t.Fatal(err)
	}
	// The first attempt asks for the recorded port, which the impostor holds;
	// the fallback then takes any free port.
	if calls.Load() != 2 || rec.Port == impostor.port {
		t.Fatalf("reused an impostor: calls=%d port=%d impostor=%d", calls.Load(), rec.Port, impostor.port)
	}
}

func TestSharedCollector_SpawnAttachesAndCleanupDetaches(t *testing.T) {
	project := mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	sc := NewSharedCollector(SharedCollectorOpts{Log: io.Discard, SpawnFn: countingSpawn(t, &calls, &last)})

	port, cleanup, err := sc.Spawn("wipnote", "sess1", project)
	if err != nil {
		t.Fatal(err)
	}
	me := strconv.Itoa(os.Getpid())
	if _, ok := last.attached.Load(me); !ok {
		t.Fatalf("launcher pid %s not attached", me)
	}
	pidFile := filepath.Join(project, ".wipnote", "sessions", "sess1", ".collector-pid")
	if pid, _, _, err := ReadCollectorPIDFile(pidFile); err != nil || pid != os.Getpid() {
		t.Fatalf(".collector-pid = %d, %v", pid, err)
	}
	if got := ReadCollectorPort(pidFile); got != port {
		t.Fatalf(".collector-pid port = %d, want %d", got, port)
	}
	if data, _ := os.ReadFile(pidFile); !strings.Contains(string(data), "\nshared\n") {
		t.Fatalf(".collector-pid lacks the shared marker:\n%s", data)
	}
	cleanup()
	cleanup() // idempotent
	if _, ok := last.attached.Load(me); ok {
		t.Fatal("cleanup did not detach the launcher")
	}
	if got, _ := last.detachedSession.Load().(string); got != "sess1" {
		t.Fatalf("detach named session %q, want sess1", got)
	}
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf(".collector-pid not removed: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("spawn calls = %d", calls.Load())
	}
}

func TestSessionEndpointPath(t *testing.T) {
	if got := SessionEndpointPath("abc123"); got != "/s/abc123" {
		t.Fatalf("SessionEndpointPath = %q", got)
	}
	for _, c := range []struct {
		path, sid, rest string
		ok              bool
	}{
		{"/s/abc/v1/logs", "abc", "/v1/logs", true},
		{"/s/a-b_C9/v1/traces", "a-b_C9", "/v1/traces", true},
		{"/s/../v1/logs", "", "", false},
		{"/s/a.b/v1/logs", "", "", false},
		{"/v1/logs", "", "", false},
		{"/s/abc", "", "", false},
	} {
		sid, rest, ok := SplitSessionPath(c.path)
		if ok != c.ok || sid != c.sid || rest != c.rest {
			t.Errorf("SplitSessionPath(%q) = %q %q %v, want %q %q %v", c.path, sid, rest, ok, c.sid, c.rest, c.ok)
		}
	}
}

func TestProbeHealth_Unreachable(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	port := ln.Addr().(*net.TCPAddr).Port
	_ = ln.Close()
	if _, err := ProbeHealth(port, 200*time.Millisecond); err == nil {
		t.Fatal("probe of a closed port succeeded")
	}
}

// syncBuf is a goroutine-safe stderr stand-in.
type syncBuf struct {
	mu sync.Mutex
	b  strings.Builder
}

func (s *syncBuf) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuf) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

// movedPortWatch attaches a session on a fake collector, kills that collector
// and occupies its port so the next re-ensure falls back to another port.
func movedPortWatch(t *testing.T, harness string, logW io.Writer) (w *sharedWatch, project, pidFile string, oldPort int) {
	t.Helper()
	project = mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	sc := NewSharedCollector(SharedCollectorOpts{Log: logW, SpawnFn: countingSpawn(t, &calls, &last), Harness: harness})
	port, cleanup, err := sc.Spawn("wipnote", "sessM", project)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	_ = last.srv.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(port))
	if err != nil {
		t.Skipf("cannot occupy port %d: %v", port, err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	w = &sharedWatch{c: sc, binPath: "wipnote", sessionID: "sessM", projectDir: project, rec: SharedRecord{PID: os.Getpid(), Port: port}, exportPort: port}
	_ = os.Remove(SharedLeasePath(project))
	pidFile = filepath.Join(project, ".wipnote", "sessions", "sessM", ".collector-pid")
	return w, project, pidFile, port
}

func TestSharedWatch_MovedPortWarnsOnceWithRecovery(t *testing.T) {
	var buf syncBuf
	w, _, pidFile, oldPort := movedPortWatch(t, "claude", &buf)
	w.check()
	w.check() // lease now matches; no second warning
	out := buf.String()
	if n := strings.Count(out, "moved to port"); n != 1 {
		t.Fatalf("moved warnings = %d, want 1:\n%s", n, out)
	}
	for _, want := range []string{"telemetry from this session may be lost", "wipnote claude --continue", "exports to " + strconv.Itoa(oldPort)} {
		if !strings.Contains(out, want) {
			t.Errorf("warning lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "codex") {
		t.Errorf("claude warning names codex:\n%s", out)
	}
	if got := ReadSessionExport(pidFile).Port; got != oldPort {
		t.Fatalf("export_port after move = %d, want launch port %d", got, oldPort)
	}
	if got := ReadCollectorPort(pidFile); got == oldPort {
		t.Fatalf("port= line still the old port %d", got)
	}
}

func TestSharedWatch_MovedPortUnknownHarnessNamesBoth(t *testing.T) {
	var buf syncBuf
	w, _, _, _ := movedPortWatch(t, "", &buf)
	w.check()
	out := buf.String()
	if !strings.Contains(out, "wipnote claude --continue") || !strings.Contains(out, "wipnote codex --continue") {
		t.Fatalf("expected both recovery commands:\n%s", out)
	}
}

func TestSharedCollector_SpawnRecordsExportPort(t *testing.T) {
	project := mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	sc := NewSharedCollector(SharedCollectorOpts{Log: io.Discard, SpawnFn: countingSpawn(t, &calls, &last), Harness: "codex"})
	port, cleanup, err := sc.Spawn("wipnote", "sessE", project)
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	pidFile := filepath.Join(project, ".wipnote", "sessions", "sessE", ".collector-pid")
	exp := ReadSessionExport(pidFile)
	if exp.Port != port || exp.Launcher != os.Getpid() || exp.Harness != "codex" {
		t.Fatalf("export record = %+v, want port %d launcher %d codex", exp, port, os.Getpid())
	}
}

// A collector that is alive but answers its health probe slowly is not
// replaced: replacing it moved the port under every running session
// (bug-fa4bb927).
func TestEnsureShared_SlowLiveCollectorIsKept(t *testing.T) {
	oldProbe, oldSlow := sharedProbeTimeout, sharedSlowProbeTimeout
	sharedProbeTimeout, sharedSlowProbeTimeout = 100*time.Millisecond, 2*time.Second
	t.Cleanup(func() { sharedProbeTimeout, sharedSlowProbeTimeout = oldProbe, oldSlow })

	project := mkSharedProject(t)
	var calls atomic.Int32
	var last *fakeShared
	spawn := countingSpawn(t, &calls, &last)
	first, err := EnsureShared("wipnote", project, spawn)
	if err != nil {
		t.Fatal(err)
	}
	last.healthDelay.Store(int64(400 * time.Millisecond))

	second, err := EnsureShared("wipnote", project, spawn)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 1 || second.Port != first.Port {
		t.Fatalf("slow collector replaced: spawns=%d port %d -> %d", calls.Load(), first.Port, second.Port)
	}
}

// Watchdog warnings go to the project's collector log, never to the terminal
// the harness owns: a line written there lands inside its prompt
// (bug-fa4bb927).
func TestSharedWatch_WarningsGoToTheCollectorLog(t *testing.T) {
	w, project, _, _ := movedPortWatch(t, "claude", nil)
	w.check()
	data, err := os.ReadFile(filepath.Join(project, ".wipnote", "logs", "otel-collector.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "moved to port") || !strings.Contains(string(data), "sessM") {
		t.Fatalf("collector log lacks the moved warning:\n%s", data)
	}
}
