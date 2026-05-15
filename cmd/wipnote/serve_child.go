package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	dbpkg "github.com/shakestzd/wipnote/internal/db"
	"github.com/shakestzd/wipnote/internal/db/writequeue"
	"github.com/shakestzd/wipnote/internal/otel/indexer"
	"github.com/shakestzd/wipnote/internal/otel/retention"
	sqls "github.com/shakestzd/wipnote/internal/otel/sink/sqlite"
	"github.com/shakestzd/wipnote/internal/registry"
	"github.com/shakestzd/wipnote/internal/storage"
	"github.com/spf13/cobra"
)

// writerService is the dashboard's instance of the slice-6 writer
// transport. It is constructed once per `wipnote serve` child process
// and shared by every in-process producer (the NDJSON indexer today;
// the OTLP HTTP receiver and sub-agent auto-ingest paths follow in
// slices 7 and beyond).
//
// Holding both the queue and the underlying Writer here lets the
// collector-status handler expose live depth + state without reaching
// into producer-local state. Nil-safe: an unset writerService means
// the dashboard is running without an index-update channel (e.g.
// during unit tests of buildSingleProjectMux that pass database=nil).
var writerService struct {
	queue *writequeue.Queue
	sink  *sqls.QueuedSink
}

// serveChildCmd is the hidden internal subcommand the parent wipnote
// server spawns for each project in multi-project mode. It is NOT intended
// for direct invocation — end users run `wipnote serve`, which forks this
// command as a child process per project.
//
// The child binds to an ephemeral port (--port 0), prints exactly one
// handshake line to stdout so the parent supervisor can discover the port,
// and then redirects stdout/stderr to a per-project log file before the
// HTTP server begins accepting traffic. This guarantees the supervisor's
// scanner never sees stray startup logs between the handshake and the
// supervisor's stdout-drain goroutine attaching.
func serveChildCmd() *cobra.Command {
	var port int
	cmd := &cobra.Command{
		Use:    "_serve-child",
		Hidden: true,
		Short:  "Internal: single-project HTTP server spawned by parent (do not invoke directly)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runServeChild(port)
		},
	}
	cmd.Flags().IntVar(&port, "port", 0, "TCP port (0 = ephemeral)")
	return cmd
}

// runServeChild opens the project DB, builds the single-project mux, binds
// the listener, prints the handshake, redirects stdio, and serves HTTP.
func runServeChild(port int) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return fmt.Errorf("locate .wipnote: %w", err)
	}

	dbPath, err := storage.CanonicalDBPath(filepath.Dir(wipnoteDir))
	if err != nil {
		return fmt.Errorf("resolve db path: %w", err)
	}
	if err := storage.EnsureDBDir(dbPath); err != nil {
		return fmt.Errorf("ensure db dir: %w", err)
	}
	database, err := dbpkg.Open(dbPath)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	// database is closed when the process exits; no defer Close — Serve blocks.

	// Slice 6 writer service (plan-ae0c37b2): one Writer + one queue
	// per project DB. Every in-process producer (indexer, future OTLP
	// receiver, sub-agent auto-ingest) submits SignalSink batches
	// through this queue instead of opening its own writable handle.
	// This is the architectural fix for the SQLITE_BUSY contention the
	// plan targets — see plan q-service-owner for the post-launch
	// `wipnote daemon` graduation path.
	if writer, err := sqls.NewWriter(dbPath); err != nil {
		fmt.Fprintf(os.Stderr, "writer service init: %v\n", err)
	} else {
		q := writequeue.New(writequeue.Config{
			Capacity: writequeue.DefaultCapacity,
			OnError: func(err error) {
				// Slice-10 contention observability: BUSY classification
				// already lands at the WriteBatch boundary in
				// internal/otel/sink/sqlite/writer.go under the writer_service
				// subsystem (which captures the actual SQL contention
				// site). Counting again here would double-bill the same
				// event. We keep the OnError hook as the log-only path
				// so operators can correlate the queue depth surfaced via
				// /api/collector-status with worker errors.
				log.Printf("writequeue: op error: %v", err)
			},
		})
		if startErr := q.Start(context.Background()); startErr != nil {
			fmt.Fprintf(os.Stderr, "writer queue start: %v\n", startErr)
			_ = writer.Close()
		} else {
			writerService.queue = q
			writerService.sink = sqls.NewQueued(q, writer)
		}
	}

	mux := buildSingleProjectMux(database, wipnoteDir)

	// NDJSON→SQLite indexer (unconditional per Q5 cutover decision).
	// The indexer now routes every SignalSink batch through the slice-6
	// writer queue rather than holding its own writable handle. Canonical
	// persistence is upstream of this path — the indexer reads NDJSON
	// produced by per-session collectors, so user work is durable on
	// disk before any submit hits the queue (canonical-first contract).
	if writerService.sink != nil {
		idxr := indexer.New(wipnoteDir, writerService.sink).WithDB(database)
		ctx := context.Background()
		go idxr.Start(ctx)
		// /api/indexer/status — per-file health for observability (Q7).
		mux.Handle("/api/indexer/status", indexerStatusHandler(idxr))
	}

	// /api/collector-status — slice-6 diagnostic surface. Returns writer
	// queue depth/state/rates so the dashboard can show backpressure.
	mux.Handle("/api/collector-status", collectorWriterStatusHandler())

	ln, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
	if err != nil {
		return fmt.Errorf("listen: %w", err)
	}
	assigned := ln.Addr().(*net.TCPAddr).Port

	// Handshake: MUST be the first output of this process. The parent
	// supervisor (internal/childproc, slice 2) reads exactly one line
	// matching `wipnote-serve-ready port=<N> pid=<P>` with a 5s deadline.
	// Any prior stdout write — log line, deprecation warning, anything —
	// corrupts the scanner. Do not add prints above this line.
	if _, err := fmt.Printf("wipnote-serve-ready port=%d pid=%d\n", assigned, os.Getpid()); err != nil {
		return fmt.Errorf("write handshake: %w", err)
	}
	if err := os.Stdout.Sync(); err != nil {
		// Non-fatal: the parent has already read the line via its pipe.
		_ = err
	}

	// Redirect stdout/stderr to a per-project log file so subsequent logs
	// (auto-ingest, handler errors, etc.) don't leak through the supervisor's
	// drain goroutine to the parent's terminal.
	projectID := registry.ComputeID(filepath.Dir(wipnoteDir))
	logsDir := filepath.Join(wipnoteDir, "logs")
	_ = os.MkdirAll(logsDir, 0o755)
	logPath := filepath.Join(logsDir, fmt.Sprintf("serve-%s.log", projectID))
	if f, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err == nil {
		os.Stdout = f
		os.Stderr = f
	}

	// Auto-ingest transcripts on startup and every 60s, scoped to this
	// project via the explicit wipnoteDir argument (not CWD). After the
	// first ingest cycle completes we kick off a one-time ai-title backfill
	// so it observes any newly-ingested legacy sessions instead of writing
	// its `.done` marker against an empty sessions table.
	go autoIngestLoop(database, wipnoteDir, func() {
		startAITitleBackfill(context.Background(), database, wipnoteDir)
	})

	// Retention job: archive sessions older than WIPNOTE_SESSION_RETAIN_DAYS
	// (default 30) at startup and every 24h. Dry-run via WIPNOTE_RETENTION_DRYRUN=1.
	retention.StartLoop(context.Background(), database, wipnoteDir)

	return (&http.Server{Handler: mux}).Serve(ln)
}

// indexerStatusHandler returns an HTTP handler for GET /api/indexer/status.
// The response body is a JSON object with per-session file health metrics.
func indexerStatusHandler(idxr *indexer.Indexer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		status := idxr.Status()
		w.Header().Set("Content-Type", "application/json")
		if err := json.NewEncoder(w).Encode(map[string]any{"files": status}); err != nil {
			http.Error(w, "encode error", http.StatusInternalServerError)
		}
	})
}
