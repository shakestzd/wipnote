package main

import (
	"context"
	"net/http"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/observe/otel/sink/ndjson"
)

// Issue #183: a launcher-managed collector (--no-idle-timeout) exits once its
// launcher dies, instead of living on for the 24h idle ceiling.
func TestAwaitShutdown_ExitsWhenOrphaned(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, ".wipnote", "sessions", "sess-orphan"), 0o755); err != nil {
		t.Fatal(err)
	}
	snk, err := ndjson.New(dir, "sess-orphan")
	if err != nil {
		t.Fatalf("ndjson.New: %v", err)
	}
	var parent atomic.Int64
	parent.Store(4242)
	origPID, origEvery := collectorParentPID, orphanCheckInterval
	collectorParentPID = func() int { return int(parent.Load()) }
	orphanCheckInterval = 20 * time.Millisecond
	t.Cleanup(func() { collectorParentPID, orphanCheckInterval = origPID, origEvery })

	var last atomic.Int64
	last.Store(time.Now().UnixMilli())
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- awaitShutdown(ctx, cancel, &http.Server{}, snk, &last, true) }()

	select {
	case <-done:
		t.Fatal("collector exited while its launcher was alive")
	case <-time.After(150 * time.Millisecond):
	}
	parent.Store(1) // launcher died; reparented to init / launchd
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("orphaned collector did not shut down")
	}
}
