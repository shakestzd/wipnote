package childproc

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// writeFakeChild writes a POSIX fake _serve-child running body.
func writeFakeChild(t *testing.T, body string) string {
	t.Helper()
	requireExecCapableTmpdir(t)
	path := filepath.Join(t.TempDir(), "fake-wipnote")
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"+body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// serverPort returns the port of an httptest server.
func serverPort(t *testing.T, srv *httptest.Server) int {
	t.Helper()
	return srv.Listener.Addr().(*net.TCPAddr).Port
}

// Issue #189: a request that gives up while the child is still cold-starting
// must neither kill the child nor make the next request start over. Before,
// the handshake deadline derived from the request context, so every short
// client timeout killed the hydrating child and the next request began a new
// cold start that the same timeout killed again.
func TestGetOrSpawn_CallerTimeoutDoesNotRestartColdStart(t *testing.T) {
	spawns := filepath.Join(t.TempDir(), "spawns")
	bin := writeFakeChild(t, "echo x >> "+spawns+"\n"+
		"sleep 0.6\n"+
		"echo \"wipnote-serve-ready port=12350 pid=$$\"\n"+
		"exec sleep 30\n")
	sup := NewSupervisor(Options{BinPath: bin, SpawnTimeout: testSpawnTimeout})
	defer sup.Shutdown(context.Background())
	dir := t.TempDir()

	short, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := sup.GetOrSpawn(short, "slow", dir); !errors.Is(err, ErrSpawnPending) {
		t.Fatalf("short caller: err = %v, want ErrSpawnPending", err)
	}

	ctx, cancel2 := context.WithTimeout(context.Background(), testCtxTimeout)
	defer cancel2()
	c, err := sup.GetOrSpawn(ctx, "slow", dir)
	if err != nil {
		t.Fatalf("second caller: %v", err)
	}
	if c.Port != 12350 {
		t.Fatalf("port = %d, want 12350", c.Port)
	}
	data, _ := os.ReadFile(spawns)
	if n := strings.Count(string(data), "x"); n != 1 {
		t.Fatalf("child was started %d times, want 1 (the abandoned request restarted the cold start)", n)
	}
}

// Issue #183: a child that is alive but no longer answers is evicted after
// consecutive failed probes, so the next request respawns it.
func TestHealthProbe_EvictsUnresponsiveChild(t *testing.T) {
	var unhealthy atomic.Bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if unhealthy.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer srv.Close()

	bin := writeFakeChild(t, "echo \"wipnote-serve-ready port="+itoa(serverPort(t, srv))+" pid=$$\"\nexec sleep 30\n")
	sup := NewSupervisor(Options{BinPath: bin, SpawnTimeout: testSpawnTimeout,
		HealthPath: "/api/health", HealthMaxMisses: 2})
	defer sup.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), testCtxTimeout)
	defer cancel()
	c, err := sup.GetOrSpawn(ctx, "probe", t.TempDir())
	if err != nil {
		t.Fatalf("GetOrSpawn: %v", err)
	}
	client := &http.Client{Timeout: time.Second}
	present := func() bool {
		sup.mu.Lock()
		defer sup.mu.Unlock()
		return sup.children["probe"] == c
	}

	sup.probeHealthOnce(ctx, client)
	if !present() {
		t.Fatal("healthy child was evicted")
	}
	unhealthy.Store(true)
	sup.probeHealthOnce(ctx, client)
	if !present() {
		t.Fatal("child evicted after one miss; HealthMaxMisses is 2")
	}
	sup.probeHealthOnce(ctx, client)
	if present() {
		t.Fatal("unresponsive child still in the map after 2 misses")
	}
	select {
	case <-c.exitC:
	case <-time.After(testCtxTimeout):
		t.Fatal("evicted child's process was not killed")
	}
}

// Issue #183: the proxy must not wait forever on a child that accepts the
// connection but never responds.
func TestProxy_BoundsWaitForChildHeaders(t *testing.T) {
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	defer srv.Close()
	defer close(release)

	bin := writeFakeChild(t, "echo \"wipnote-serve-ready port="+itoa(serverPort(t, srv))+" pid=$$\"\nexec sleep 30\n")
	sup := NewSupervisor(Options{BinPath: bin, SpawnTimeout: testSpawnTimeout,
		ProxyHeaderTimeout: 200 * time.Millisecond})
	defer sup.Shutdown(context.Background())
	ctx, cancel := context.WithTimeout(context.Background(), testCtxTimeout)
	defer cancel()
	c, err := sup.GetOrSpawn(ctx, "hung", t.TempDir())
	if err != nil {
		t.Fatalf("GetOrSpawn: %v", err)
	}

	rec := httptest.NewRecorder()
	start := time.Now()
	c.Proxy.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/stats", nil))
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", rec.Code)
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("proxy waited %s for a hung child", took)
	}
}
