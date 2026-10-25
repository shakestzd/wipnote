package main

import (
	"context"
	"database/sql"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/daemon/apply"
)

// blockHydrate replaces the startup hydrate with one that captures the
// daemon's projection handle and waits for release. On release it creates
// hydrate_marker, so an op that ran before the hydrate would fail.
func blockHydrate(t *testing.T) (entered <-chan *sql.DB, release chan struct{}) {
	t.Helper()
	got := make(chan *sql.DB, 1)
	release = make(chan struct{})
	old := hydrateWriterDB
	hydrateWriterDB = func(db *sql.DB, _ string) error {
		got <- db
		<-release
		_, err := db.Exec(`CREATE TABLE hydrate_marker (x INTEGER)`)
		return err
	}
	t.Cleanup(func() { hydrateWriterDB = old })
	return got, release
}

// The daemon binds its socket and serves while the startup hydrate is still
// running (bug-06d2c91d). Reads answer, applied-ack writes are refused at once,
// and async writes wait in the queue and apply after the hydrate.
func TestRunWriterOnly_SocketBoundWhileHydrateRuns(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("WIPNOTE_PROJECT_DIR", root)
	t.Setenv("WIPNOTE_WRITER_IDLE_TIMEOUT", "2s")
	entered, release := blockHydrate(t)

	done := make(chan error, 1)
	go func() { done <- runWriterOnly() }()
	defer func() { <-done }()
	defer func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}()

	var projection *sql.DB
	select {
	case projection = <-entered:
	case <-time.After(10 * time.Second):
		t.Fatal("hydrate never started")
	}
	waitSocketBound(t, daemon.SocketPath(root))

	if _, err := daemon.NewReadClient(root).Ping(context.Background()); err != nil {
		t.Fatalf("ping while hydrating: %v", err)
	}
	start := time.Now()
	if apply.RouteSQL(root, `INSERT INTO hydrate_marker VALUES (?)`, 1) {
		t.Fatal("an applied-ack write was accepted before the hydrate ended")
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Fatalf("applied-ack write waited %v for the hydrate, want an immediate refusal", elapsed)
	}
	if !apply.RouteSQLAsync(root, `INSERT INTO hydrate_marker VALUES (?)`, 2) {
		t.Fatal("async write was not enqueued while hydrating")
	}

	close(release)
	waitMarkerRows(t, projection, 1)
}

func waitSocketBound(t *testing.T, sock string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if c, err := net.Dial("unix", sock); err == nil {
			c.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("socket not bound while the hydrate was running")
}

// waitMarkerRows waits for the async write to land after the hydrate.
func waitMarkerRows(t *testing.T, db *sql.DB, want int) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	var n int
	for time.Now().Before(deadline) {
		if err := db.QueryRow(`SELECT count(*) FROM hydrate_marker`).Scan(&n); err == nil && n == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("hydrate_marker has %d rows, want %d: the queued write did not apply after the hydrate", n, want)
}
