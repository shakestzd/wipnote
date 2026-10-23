package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/core/daemon/apply"
	"github.com/shakestzd/wipnote/core/daemon/readsrv"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/db/writequeue"
	"github.com/shakestzd/wipnote/core/hooks"
	otelreceiver "github.com/shakestzd/wipnote/observe/otel/receiver"
	"github.com/shakestzd/wipnote/observe/otel/retention"
	"github.com/spf13/cobra"
)

// serveChildCmd is the hidden per-project writer daemon. core/daemon spawns it
// as `_serve-child --headless` (spawnHeadlessWriter, EnsureDaemon) for hooks,
// CLI commands and the launchers; it is never invoked by hand. The name and
// the --headless flag are kept for those spawn sites and for running writers
// started by older binaries: the HTTP dashboard this command once also served
// was removed, so every invocation runs the writer.
func serveChildCmd() *cobra.Command {
	var headless bool
	cmd := &cobra.Command{
		Use:    "_serve-child",
		Hidden: true,
		Short:  "Internal: per-project writer daemon (do not invoke directly)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWriterOnly()
		},
	}
	cmd.Flags().BoolVar(&headless, "headless", true, "Accepted for the existing spawn sites; the writer always runs headless")
	return cmd
}

// runWriterOnly is the headless writer-only serve_child path (plan-bb91616a
// slice-2, feat-075c110d MVP-2). It is ADDITIVE: it shares no code path with
// runServeChild's HTTP setup and changes no existing write path.
//
// Lifecycle:
//  1. Acquire the O_EXCL single-owner lease. A racing loser gets
//     daemon.ErrLeaseHeld and exits 0 (the existing owner serves the socket).
//  2. Open the writable DB (same dbpkg.Open + receiver.NewWriter the default
//     serve_child uses) and start a writequeue.Queue — the SAME single-writer
//     mechanism. Every op submitted over the socket funnels through it.
//  3. Bind the per-project Unix socket and serve until interrupted.
//
// MVP-2 wires NO real op appliers (callers cut over in MVP-3/4); the listener
// uses daemon.RejectingApplier so any op_type is acked error rather than
// mis-applied. This proves the transport end-to-end without touching dbgate.
func runWriterOnly() error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return fmt.Errorf("locate .wipnote: %w", err)
	}
	projectRoot := filepath.Dir(wipnoteDir)

	lease, err := daemon.AcquireLease(projectRoot)
	if err != nil {
		if err == daemon.ErrLeaseHeld {
			// Another process already owns the writer — nothing to do.
			fmt.Fprintln(os.Stderr, "wipnote: writer already owned; exiting")
			return nil
		}
		return fmt.Errorf("acquire writer lease: %w", err)
	}
	defer lease.Release()

	writeDB, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		return fmt.Errorf("open ephemeral compatibility db: %w", err)
	}
	if err := hydrateCompatibilityDB(writeDB, wipnoteDir); err != nil {
		writeDB.Close()
		return fmt.Errorf("hydrate compatibility db: %w", err)
	}
	defer writeDB.Close()

	// The OTel signals Writer BORROWS the single writable handle above rather
	// than opening its own pool (feat-075c110d). With MaxOpenConns=1 it cannot
	// pin a lifetime connection (that would starve the applier + maintenance),
	// so NewWriterFromDB acquires the pool connection per-batch and releases it
	// — the pool serialization is the single-writer guarantee.
	writer, err := otelreceiver.NewWriterFromDB(writeDB)
	if err != nil {
		return fmt.Errorf("writer service init: %w", err)
	}
	defer writer.Close()

	q := writequeue.New(writequeue.Config{
		Capacity: writequeue.DefaultCapacity,
		OnError:  func(err error) { log.Printf("writequeue: op error: %v", err) },
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := q.Start(ctx); err != nil {
		return fmt.Errorf("writer queue start: %w", err)
	}
	defer q.Stop(drainGrace)

	// Graceful shutdown (feat-075c110d lifecycle hardening): trap SIGTERM/
	// SIGINT and cancel the serve context. Cancellation closes the listener
	// (Serve returns), stops the maintenance loops, and fires the deferred
	// ln.Close() — unlinking .wipnote/writer.sock — and the deferred
	// lease.Release() — removing .wipnote/writer.pid. Draining of in-flight
	// ops happens in the deferred q.Stop(drainGrace) above. Without this the
	// headless writer left both the stale socket and the lease behind on
	// SIGTERM, blocking the dashboard's serve_child and the next writer.
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(sigC)
	go func() {
		select {
		case <-sigC:
			cancel()
		case <-ctx.Done():
		}
	}()

	// Per-project background maintenance (MOVED from runServeChild in
	// feat-075c110d increment 2). These are the writable workloads that don't
	// fit the socket/writequeue op API and previously ran inside the HTTP
	// serve_child against its own writable handle — the source of the
	// serve_child↔writer contention. They now run HERE, against the daemon's
	// single writable handle / queue, so the HTTP serve_child can be strictly
	// read-only.
	startWriterMaintenance(ctx, wipnoteDir)

	// Wire the real derived-op Applier. writer.DB() is the SAME single
	// writable handle (writeDB, MaxOpenConns=1) the OTel sink, indexer, and
	// maintenance loops use, so all socket-delivered writes serialize on the
	// one connection — the structural single-writer invariant.
	// Wire the read side (feat-f6759e37). The Reader answers work-item queries
	// from CANONICAL state — the .wipnote HTML files — not from writeDB, so it
	// shares nothing with the write path above and cannot queue behind it. That
	// is the whole point: hooks are fresh processes that cannot afford the
	// canonical parse, and this daemon is where that parse gets amortised, so
	// hooks stop needing the derived index to answer work-item questions.
	readCache := readsrv.NewCache(wipnoteDir)

	ln, err := daemon.NewListener(daemon.ListenerConfig{
		SocketPath: daemon.SocketPath(projectRoot),
		Queue:      q,
		Applier:    apply.NewApplier(writer.DB()),
		Reader:     readsrv.Reader(readCache),
		OwnerPID:   os.Getpid(),
	})
	if err != nil {
		return fmt.Errorf("bind writer socket: %w", err)
	}
	defer ln.Close()

	// Readiness signal: the socket is bound and the queue is running.
	fmt.Fprintf(os.Stderr, "wipnote: writer-only daemon ready socket=%s pid=%d\n", ln.Addr(), os.Getpid())

	// The writer is auto-spawned detached by hooks, the CLI and the launchers,
	// so it idle-exits after writerIdleTimeout rather than lingering.
	idle := writerIdleTimeout()
	return ln.ServeWithIdleTimeout(ctx, idle)
}

// retentionSweepInterval is how often the daemon runs the retention pass
// (archive completed sessions, sweep stale NDJSON). It matches the cadence
// retention.StartLoop used before this function drove the ticker itself.
const retentionSweepInterval = 24 * time.Hour

// startWriterMaintenance launches the per-project background maintenance that
// does not fit the socket/writequeue op API. It runs INSIDE the headless writer
// daemon (feat-075c110d increment 2), the one long-lived per-project process
// and therefore the only place a periodic pass can live.
//
// # The test every loop had to pass
//
// These loops were written when the project database was a file on disk shared
// by every wipnote process. The daemon wrote, the dashboard read, and a row the
// daemon inserted was a row the dashboard could serve. That is no longer true:
// each process builds its OWN in-memory projection (feat-fc3cc9e0), hydrated
// from canonical files by hydrateCompatibilityDB.
//
// That breaks loops in two different ways, and the second is the dangerous one:
//
//  1. A loop whose entire OUTPUT was a DB row now produces nothing.
//  2. A loop whose GUARD reads a table the projection does not hydrate does not
//     become cautious — it becomes reckless. An empty table reads as "nothing
//     to protect", never as "I cannot tell". Every such guard fails OPEN.
//
// So the question for each loop is not "does it still run" but "is its output
// durable, and is every table its guards read actually hydrated". The hydration
// set is exactly what hydrateCompatibilityDB writes: tracks, features/bugs/
// spikes, archived work items, claim_episodes, sessions (from the ledger, nine
// columns), graph_edges, plans, gate_records, recaps. Notably NOT hydrated:
// claims, agent_events, messages, tool_calls.
//
// # Running (2)
//
//   - retention (24h) — reads sessions.status/completed_at, which the ledger
//     hydration does populate, and writes NOTHING to the database. Destructive
//     by intent: it tar.gz's completed sessions into .wipnote/archive/ and then
//     os.RemoveAll's the live directory. Its own interlock is the .index-offset
//     progress marker (retention.go:267), which is missing or behind whenever
//     the dashboard indexer has not caught up — and a missing marker reads as
//     "not caught up", so it defers rather than deleting un-indexed telemetry.
//   - reconcile drain (5m) — git-commits done-but-uncommitted work-item
//     artifacts (feat-c08d1ba1 slice-6 moved this off the Stop hook's ~5.45s
//     hot path). It needs no projection at all: hooks.Reconcile-
//     DoneButUncommittedForProject ignores its *sql.DB (session_end.go:733,
//     `_ = database`) and works entirely from canonical files plus git.
//
// # Off, with cause (5)
//
// None of these had a production caller before this function was restored, so
// leaving them off returns to the status quo rather than losing anything.
//
//   - EMPTY-SPIKE WORKTREE GC — OFF. The feature is opt-in and DEFAULT OFF
//     (worktree.LoadCleanupConfig sets EmptySpikeWorktreeCleanup: false), so
//     wiring it would change nothing for anyone today. It is unwired anyway
//     because its LIVENESS GUARD IS INERT, and the previous comment here
//     claimed the opposite — it called the sweep "liveness-aware", which is the
//     sentence a future reader would trust when deciding whether turning the
//     feature ON is safe. It is not.
//     workItemHasLiveHeartbeat (config.go:198) joins the `claims` table.
//     `claims` has no hydration path, and an audit found no production writer
//     for it at all, so the query returns ErrNoRows and the guard returns false
//     for EVERY work item — it cannot protect anything, in any configuration.
//     With it inert, all that stands between a live agent's worktree and
//     destruction is an mtime TTL, a git-lock check and a clean-tree check —
//     which an agent idling on a clean tree satisfies — after which the sweep
//     calls `git worktree remove` and shells out to `wipnote <type> complete`.
//     Tracked as bug-0b322d67; rewire once liveness is answerable from
//     canonical data. claim_episodes is hydrated but carries no
//     last_heartbeat_at, so it is not a substitute: "episode still open" is not
//     "heartbeat within N minutes".
//   - ORPHAN DRAIN — OFF. hooks.SweepOrphanedEventsForProject selects from
//     agent_events (event_repo.go:392), which the projection never hydrates, so
//     it finds zero orphans on every pass. This one fails CLOSED — an empty
//     table means it sweeps nothing — so it is harmless, but it would pay a
//     full project hydrate every five minutes to guarantee finding nothing.
//   - REAPER — OFF. Its heartbeat guard is dead the same way the sweep's is:
//     db.SessionLivenessByHeartbeat reads MAX(last_heartbeat_at) FROM claims
//     (session_repo.go:586), so heartbeatStale is permanently true. Unlike the
//     sweep it is NOT dangerous, because SessionReapEligible ANDs that with
//     !IsSessionProcessAlive (session_liveness.go:224), a pid-file + kill(0)
//     check that degrades to LIVE on any uncertainty. It stays off for a
//     different reason: its only durable output is closing claim episodes in
//     the canonical ledger, and it cannot record that it did so — the session
//     stays open in sessions-ledger.html because the remediation is an UPDATE
//     against the projection — so every daemon restart re-reaps the same dead
//     sessions. Restore it once that close is durable; the orphaned-collector
//     reaping it also does is real work being deferred with it.
//   - AUTO-INGEST — OFF. Both of its skip guards read unhydrated state:
//     CountMessages reads `messages`, so it always returns 0, and the
//     transcript_synced mtime guard reads a column the ledger hydration never
//     populates. needsIngest is therefore true for every session on every
//     pass — a full re-ingest of every transcript, forever. In a repo with
//     bug-1f338b5b and bug-4e5816f4 in its history that is a severe regression.
//   - AI-TITLE BACKFILL — OFF. It selects transcript_path, which is not among
//     the columns the ledger hydration inserts, so every row short-circuits and
//     zero titles are updated — and it then writes the persistent sentinel
//     .wipnote/migrations/ai-title-backfill.done, durably guaranteeing a pass
//     that accomplished nothing never runs again.
//   - RECAPS REINDEX — OFF. It refilled the recaps table so the dashboard's
//     Recap tab was not blank after a restart (bug-95d2d493). The dashboard now
//     populates recaps in its own projection on every start, so the loop
//     refilled a table nobody queries.
//
// The NDJSON→SQLite indexer does not run here: nothing outside `wipnote
// reindex` reads indexed signals, and SessionEnd marks each session's telemetry
// consumed once its collector exits (markTelemetryConsumed).
//
// ctx cancellation (from SIGTERM/SIGINT or idle-exit) stops every loop.
func startWriterMaintenance(ctx context.Context, wipnoteDir string) {
	projectRoot := filepath.Dir(wipnoteDir)

	// Retention archival: startup, then every 24h. A fresh projection per pass
	// rather than one hydrated at daemon boot — a daemon lives for hours, and a
	// boot-time snapshot would never show a session completed since.
	startDrainLoop(ctx, retentionSweepInterval, func() {
		withFreshProjection(wipnoteDir, "retention", func(database *sql.DB) {
			if _, err := retention.Sweep(database, wipnoteDir, "", os.Getenv("WIPNOTE_RETENTION_DRYRUN") == "1"); err != nil {
				log.Printf("retention: sweep: %v", err)
			}
		})
	})

	// Out-of-band reconcile drain (feat-c08d1ba1 slice-6). No projection: the
	// callee ignores the handle and reads canonical artifacts directly, so
	// building one would be pure cost.
	startDrainLoop(ctx, reconcileDrainInterval, func() {
		hooks.ReconcileDoneButUncommittedForProject(nil, projectRoot)
	})
}

// withFreshProjection builds a short-lived compatibility projection from
// canonical state, hands it to fn, and closes it. label names the caller in
// failure logs so a broken pass is attributable.
func withFreshProjection(wipnoteDir, label string, fn func(*sql.DB)) {
	database, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		log.Printf("%s: open projection: %v", label, err)
		return
	}
	defer database.Close()
	if err := hydrateCompatibilityDB(database, wipnoteDir); err != nil {
		log.Printf("%s: hydrate projection: %v", label, err)
		return
	}
	fn(database)
}

// writerIdleTimeout resolves the headless writer's idle-exit window. It honours
// WIPNOTE_WRITER_IDLE_TIMEOUT (e.g. "200ms", "10m") for tests/operators and
// otherwise falls back to daemon.DefaultIdleTimeout.
func writerIdleTimeout() time.Duration {
	if v := os.Getenv("WIPNOTE_WRITER_IDLE_TIMEOUT"); v != "" {
		if d, err := time.ParseDuration(v); err == nil && d > 0 {
			return d
		}
	}
	return daemon.DefaultIdleTimeout
}

// drainGrace bounds how long the headless writer waits for in-flight ops to
// commit on shutdown.
const drainGrace = 2 * time.Second
