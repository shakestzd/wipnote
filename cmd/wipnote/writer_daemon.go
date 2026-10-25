package main

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sync/atomic"
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

// runWriterOnly is the headless writer daemon (plan-bb91616a slice-2,
// feat-075c110d MVP-2, feat-f6759e37 reads).
//
// Lifecycle:
//  1. Acquire the O_EXCL single-owner lease. A racing loser gets
//     daemon.ErrLeaseHeld and exits 0 (the existing owner serves the socket).
//  2. Start the telemetry catch-up loop. It reads only events.ndjson, so it
//     does not need the projection (bug-98f32723).
//  3. Open the in-memory projection, start the single-writer queue, and
//     submit the startup hydrate as the queue's FIRST op.
//  4. Bind the per-project Unix socket and serve at once, while the hydrate
//     runs (bug-06d2c91d). See core/daemon/socket_ready.go for what callers
//     see until it ends.
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

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Telemetry catch-up must NOT wait for the startup hydrate: a loop
	// started after it left every unmarked shard unfinalized for as long as
	// the hydrate took (bug-98f32723). Catch-up reads only events.ndjson and
	// uses its own projection, so it is safe to run now.
	startTelemetryCatchUpLoop(ctx, telemetryCatchUpInterval, projectRoot)
	return serveWriter(ctx, cancel, wipnoteDir)
}

// serveWriter owns the daemon's single writable projection and serves the
// socket until ctx ends (signal, idle exit, or a failed hydrate).
func serveWriter(ctx context.Context, cancel context.CancelFunc, wipnoteDir string) error {
	writeDB, err := dbpkg.OpenEphemeralProjection()
	if err != nil {
		return fmt.Errorf("open ephemeral compatibility db: %w", err)
	}
	defer writeDB.Close()

	// The OTel signals Writer BORROWS the single writable handle above rather
	// than opening its own pool (feat-075c110d): with MaxOpenConns=1 it takes
	// the pool connection per batch, and that serialization is the
	// single-writer guarantee.
	writer, err := otelreceiver.NewWriterFromDB(writeDB)
	if err != nil {
		return fmt.Errorf("writer service init: %w", err)
	}
	defer writer.Close()

	q := writequeue.New(writequeue.Config{
		Capacity: writequeue.DefaultCapacity,
		OnError:  func(err error) { log.Printf("writequeue: op error: %v", err) },
	})
	if err := q.Start(ctx); err != nil {
		return fmt.Errorf("writer queue start: %w", err)
	}
	defer q.Stop(drainGrace)

	ready, err := submitStartupHydrate(ctx, cancel, q, writeDB, wipnoteDir)
	if err != nil {
		return err
	}
	defer stopOnSignal(ctx, cancel)()

	// Background maintenance (feat-075c110d increment 2) — see
	// startWriterMaintenance for which loops run and why.
	startWriterMaintenance(ctx, wipnoteDir)
	return bindAndServe(ctx, wipnoteDir, q, writer.DB(), ready.Load)
}

// submitStartupHydrate queues the projection build as the FIRST op on q, so
// every socket op, which can only arrive later, applies after it in FIFO
// order. The returned flag turns true when the build ends; the listener uses
// it as its readiness gate. A failed build cancels ctx and the daemon exits,
// as it did when the build ran before the socket was bound.
func submitStartupHydrate(ctx context.Context, cancel context.CancelFunc, q *writequeue.Queue, database *sql.DB, wipnoteDir string) (*atomic.Bool, error) {
	ready := new(atomic.Bool)
	build := func(context.Context) error {
		start := time.Now()
		if err := hydrateWriterDB(database, wipnoteDir); err != nil {
			log.Printf("writer: hydrate compatibility db: %v", err)
			cancel()
			return err
		}
		ready.Store(true)
		log.Printf("writer: startup projection built in %s", time.Since(start).Round(time.Millisecond))
		return nil
	}
	if err := q.Submit(ctx, build); err != nil {
		return nil, fmt.Errorf("queue startup hydrate: %w", err)
	}
	return ready, nil
}

// stopOnSignal cancels ctx on SIGINT/SIGTERM (feat-075c110d lifecycle
// hardening). Cancellation closes the listener, stops the maintenance loops,
// and lets the deferred cleanups unlink .wipnote/writer.sock and release
// .wipnote/writer.pid. It returns the function that stops the trap.
func stopOnSignal(ctx context.Context, cancel context.CancelFunc) func() {
	sigC := make(chan os.Signal, 1)
	signal.Notify(sigC, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		select {
		case <-sigC:
			cancel()
		case <-ctx.Done():
		}
	}()
	return func() { signal.Stop(sigC) }
}

// bindAndServe binds the per-project socket and serves until ctx ends or the
// daemon idles out.
//
// Writes go through the real derived-op Applier on writeDB, the same single
// writable handle the OTel sink and the hydrate use. Reads (feat-f6759e37) are
// answered from CANONICAL state — the .wipnote HTML files — by readsrv, so they
// share nothing with the write path and work before the hydrate ends.
func bindAndServe(ctx context.Context, wipnoteDir string, q *writequeue.Queue, writeDB *sql.DB, ready func() bool) error {
	projectRoot := filepath.Dir(wipnoteDir)
	ln, err := daemon.NewListener(daemon.ListenerConfig{
		SocketPath: daemon.SocketPath(projectRoot),
		Queue:      q,
		Applier:    apply.NewApplier(writeDB),
		Reader:     readsrv.Reader(readsrv.NewCache(wipnoteDir)),
		OwnerPID:   os.Getpid(),
		Ready:      ready,
	})
	if err != nil {
		return fmt.Errorf("bind writer socket: %w", err)
	}
	defer ln.Close()

	// Readiness signal: the socket is bound and the queue is running.
	log.Printf("wipnote: writer-only daemon ready socket=%s pid=%d", ln.Addr(), os.Getpid())

	// The writer is auto-spawned detached by hooks, the CLI and the launchers,
	// so it idle-exits after writerIdleTimeout rather than lingering.
	return ln.ServeWithIdleTimeout(ctx, writerIdleTimeout())
}

// retentionSweepInterval is how often the daemon runs the retention pass
// (archive completed sessions, sweep stale NDJSON). It matches the cadence
// retention.StartLoop used before this function drove the ticker itself.
const retentionSweepInterval = 24 * time.Hour

// hydrateWriterDB builds the daemon's startup projection. It is a variable so a
// test can make it block and prove the maintenance loops do not wait for it.
var hydrateWriterDB = hydrateCompatibilityDB

// telemetryCatchUpBudget bounds the telemetry catch-up that runs before each
// retention sweep (daemon and `wipnote prune`). Neither runs under a hook cap,
// so the budget is generous; a session it cannot reach waits for the next pass.
const telemetryCatchUpBudget = 2 * time.Minute

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
//     progress marker (retention.go:267), which is missing or behind until
//     observe/otel/finalize has rolled the session up — and a missing marker
//     reads as "not caught up", so it defers rather than deleting un-indexed
//     telemetry. finalize.Sweep runs that catch-up first, within
//     telemetryCatchUpBudget.
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
// The polling NDJSON→SQLite indexer does not run here. SessionEnd finalizes
// each session's telemetry once its collector exits, and the retention sweep
// below first runs the telemetry catch-up (finalize.Sweep) for sessions that
// never got a clean SessionEnd, so their logs can still be rolled up and
// archived.
//
// ctx cancellation (from SIGTERM/SIGINT or idle-exit) stops every loop.
func startWriterMaintenance(ctx context.Context, wipnoteDir string) {
	projectRoot := filepath.Dir(wipnoteDir)

	// Retention archival: startup, then every 24h. A fresh projection per pass
	// rather than one hydrated at daemon boot — a daemon lives for hours, and a
	// boot-time snapshot would never show a session completed since.
	startDrainLoop(ctx, retentionSweepInterval, func() {
		withFreshProjection(wipnoteDir, "retention", func(database *sql.DB) {
			dryRun := os.Getenv("WIPNOTE_RETENTION_DRYRUN") == "1"
			if !dryRun {
				runTelemetryCatchUp(ctx, projectRoot)
			}
			if _, err := retention.Sweep(database, wipnoteDir, "", dryRun); err != nil {
				log.Printf("retention: sweep: %v", err)
			}
		})
	})

	// Telemetry catch-up is started by runWriterOnly before the startup hydrate,
	// so it is not delayed by it.

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
