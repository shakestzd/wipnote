package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/internal/childproc"
	"github.com/shakestzd/wipnote/internal/registry"
)

// Issue #189: a request for a project whose child is still cold-starting gets
// a prompt 503 + Retry-After instead of hanging, and the spawn keeps running.
func TestProxyHandler_ColdStartAnswers503Warming(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "slow-wipnote")
	script := "#!/bin/sh\nsleep 5\necho \"wipnote-serve-ready port=1 pid=$$\"\nexec sleep 30\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	regPath := filepath.Join(t.TempDir(), "projects.json")
	orig := defaultRegistryPath
	defaultRegistryPath = func() string { return regPath }
	t.Cleanup(func() { defaultRegistryPath = orig })
	reg, err := registry.Load(regPath)
	if err != nil {
		t.Fatal(err)
	}
	reg.Upsert(projectDir, "p", "")
	if err := reg.Save(); err != nil {
		t.Fatal(err)
	}
	entries := reg.List()
	if len(entries) != 1 {
		t.Fatalf("registry has %d entries, want 1", len(entries))
	}
	id := entries[0].ID

	origWait := serveSpawnWait
	serveSpawnWait = 150 * time.Millisecond
	t.Cleanup(func() { serveSpawnWait = origWait })

	sup := childproc.NewSupervisor(childproc.Options{BinPath: bin, SpawnTimeout: 30 * time.Second})
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		sup.Shutdown(ctx)
	}()

	rec := httptest.NewRecorder()
	start := time.Now()
	proxyHandler(sup).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/p/"+id+"/api/stats", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d (%s), want 503", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("Retry-After") == "" {
		t.Fatal("503 warming response has no Retry-After")
	}
	if took := time.Since(start); took > 3*time.Second {
		t.Fatalf("request held open %s during cold start", took)
	}
}

// The supervisor's liveness probe reads unhealthy when the projection cannot
// answer a query.
func TestServeChildHealthHandler(t *testing.T) {
	database, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		t.Fatal(err)
	}
	h := serveChildHealthHandler(database)

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, serveChildHealthPath, nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("healthy projection: status %d", rec.Code)
	}

	database.Close()
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, serveChildHealthPath, nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("closed projection: status %d, want 503", rec.Code)
	}
}
