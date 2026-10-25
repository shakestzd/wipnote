package receiver

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sync"

	dbpkg "github.com/shakestzd/wipnote/core/db"
)

// dbExecer is the minimal interface shared by *sql.Conn and *sql.Tx, used
// by helpers that need to issue queries within a live transaction without
// caring whether they hold a *sql.Tx or a raw *sql.Conn.
type dbExecer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
}

// metricRowsPerSessionLimit bounds high-cardinality metric streams in the
// derived SQLite index. Canonical NDJSON remains the durable replay source;
// SQLite keeps recent metric rows for dashboard queries and drops the tail.
const metricRowsPerSessionLimit = 5000

// Writer persists UnifiedSignals into the otel_signals table. It owns
// its own *sql.DB with MaxOpenConns=1 so every write serializes through
// one connection — this eliminates SQLITE_BUSY errors under concurrent
// load from the OTLP receiver and hook binaries that share the DB file.
//
// All inserts go through BEGIN IMMEDIATE transactions (one per batch);
// IMMEDIATE acquires the writer lock up front so we don't burn retry
// budget on deferred upgrades. Prepared statements are held for the
// Writer's lifetime.
//
// conn is a pinned *sql.Conn obtained at construction from the single
// underlying connection. Using a pinned conn lets us issue raw
// "BEGIN IMMEDIATE" / "COMMIT" / "ROLLBACK" statements that the
// database/sql api cannot express through sql.TxOptions.
type Writer struct {
	db   *sql.DB
	conn *sql.Conn // pinned to the single connection (own-pool mode only; nil in shared mode)
	// shared is true when the Writer borrows an externally-owned *sql.DB
	// (feat-075c110d: the daemon's single writable handle) instead of
	// opening + pinning its own. In shared mode the Writer does NOT pin a
	// lifetime connection and does NOT hold lifetime prepared statements;
	// each WriteBatch acquires the pool's connection for the duration of
	// the batch and releases it on completion. The shared handle is
	// opened with MaxOpenConns=1 by the owner, so the database/sql pool
	// itself serializes every caller (the writer batches, the socket-op
	// applier, and the maintenance loops) onto ONE physical SQLite
	// connection — making concurrent BEGIN IMMEDIATE structurally
	// impossible and eliminating the cross-handle SQLITE_BUSY thrash.
	shared          bool
	ownDB           bool      // true when the Writer opened db itself and must Close it
	insertStmt      *sql.Stmt // own-pool mode only: lifetime statements pinned to conn
	sessStmt        *sql.Stmt
	resStmt         *sql.Stmt
	placeholderStmt *sql.Stmt  // INSERT placeholder subagent_invocation row
	upgradeStmt     *sql.Stmt  // UPDATE placeholder → real Agent span
	mu              sync.Mutex // serializes WriteBatch calls — SQLite serializes writes anyway via IMMEDIATE lock, this just makes it explicit at the Go layer
}

// batchStmts is the per-batch set of prepared statements used in shared
// mode. They are prepared on the acquired connection and closed when the
// batch completes, mirroring the lifetime statements held in own-pool mode.
type batchStmts struct {
	insert      *sql.Stmt
	sess        *sql.Stmt
	res         *sql.Stmt
	placeholder *sql.Stmt
	upgrade     *sql.Stmt
}

// NewWriter opens a writer-mode DB handle on dbPath. The handle is
// separate from whatever read pool the caller may already have open:
//
//	readers := dbpkg.Open(path)          // existing read pool
//	writer  := receiver.NewWriter(path)  // dedicated single-conn writer
//
// Both are fine because SQLite WAL mode allows concurrent readers with
// a single writer. The caller must Close the writer on shutdown so the
// prepared statements release.
func NewWriter(dbPath string) (*Writer, error) {
	db, err := openWriterDB(dbPath)
	if err != nil {
		return nil, err
	}

	// Acquire the pinned connection before preparing statements so that
	// all prepared statements and BEGIN IMMEDIATE calls share the exact
	// same underlying SQLite connection. Since MaxOpenConns=1 this is
	// the one and only connection the pool will ever create.
	conn, err := db.Conn(context.Background())
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("pin conn: %w", err)
	}

	// bug-74a7bda7 cold-start race: the DSN deliberately omits journal_mode
	// so the read-pool Open's BuildPragmas stays the single source of truth.
	// But if the Writer is the very first connection to a brand-new DB file
	// (it opens before the read pool in runServeChild), the file starts in
	// the SQLite default (DELETE) and stays there until some WAL-safe
	// connection flips it. On a WAL-safe filesystem that means the writer
	// runs in DELETE for the cold-start window, re-introducing the
	// shared/reserved contention this bug is about. So: assert journal_mode
	// once, here, and ONLY promote DELETE→WAL when the backing filesystem is
	// WAL-safe. On WAL-unsafe filesystems (overlayfs/FUSE/9p/NFS — the
	// devcontainer case) we accept DELETE silently: no error, no log spam.
	// This never forces WAL on overlayfs and never touches isUnsafeForMmap.
	assertWriterJournalMode(conn, dbPath)

	w := &Writer{db: db, conn: conn, ownDB: true}
	if err := w.prepare(); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}
	return w, nil
}

// openWriterDB opens the writer's own pool on dbPath, runs migrations and
// verifies the otel_signals schema.
func openWriterDB(dbPath string) (*sql.DB, error) {
	// Per-connection pragmas only — journal_mode is intentionally absent.
	// BuildPragmas (via ApplyPragmas on the read-pool Open) is the sole
	// source of truth for journal_mode; on unsafe filesystems it resolves
	// to DELETE. Setting WAL here would permanently override that decision
	// for the lifetime of the DB file, breaking all subsequent connections.
	dsn := dbPath + "?_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)&_pragma=foreign_keys(1)&_txlock=immediate"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open writer: %w", err)
	}
	// The single-writer constraint is the core of the concurrency
	// design. We use MaxOpenConns=3: the writer pins one connection,
	// and the remaining 2 allow concurrent readers and test assertions
	// (e.g., QueryRow in ConcurrentBatches test). SQLite WAL mode
	// supports multiple concurrent readers; only writes serialize.
	db.SetMaxOpenConns(3)
	db.SetMaxIdleConns(1)
	db.SetConnMaxIdleTime(0)

	// bug-286ce8f7: this constructor used to open its own DB handle via the
	// raw sql.Open above and never run schema migrations at all, silently
	// depending on some OTHER writer (serve_child's dbpkg.Open, a hook) having
	// already migrated the same file first. cmd/wipnote/reindex_otel_events.go
	// calls NewWriter directly, with no such guarantee — a fresh install or a
	// DB that fell behind gets a writer with a stale schema and every insert
	// referencing a missing column fails from then on. Run migrations here so
	// this constructor gives the same guarantee every dbpkg.Open caller already
	// gets, regardless of which entry point opens the writer first.
	if err := dbpkg.RunMigrations(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("open writer: run migrations: %w", err)
	}
	if err := verifyOtelSignalsSchema(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("open writer: %w", err)
	}
	return db, nil
}

// NewWriterFromDB constructs a Writer that BORROWS an existing writable
// *sql.DB instead of opening + pinning its own pool. This is the
// daemon's single-writer path (feat-075c110d): serve_child opens ONE
// writable handle with MaxOpenConns=1 (which also runs migrations) and
// shares it across every write path — the socket-op applier, the OTel
// sink (this Writer), the indexer, and the maintenance loops. Because
// the underlying pool caps at a single physical connection, the
// database/sql pool serializes every caller onto that one connection, so
// two concurrent BEGIN IMMEDIATE transactions can never exist and the
// cross-handle SQLITE_BUSY contention is eliminated at the root.
//
// Unlike NewWriter, this constructor does NOT pin a lifetime connection
// (that would starve the applier + maintenance of the single pooled
// connection) and does NOT prepare lifetime statements. Each WriteBatch
// acquires the pool connection for its duration, prepares the batch's
// statements on it, runs the BEGIN IMMEDIATE transaction, and releases
// the connection back to the pool so the next caller can proceed.
//
// The caller OWNS the *sql.DB lifecycle: Writer.Close does NOT close it.
func NewWriterFromDB(database *sql.DB) (*Writer, error) {
	if database == nil {
		return nil, fmt.Errorf("NewWriterFromDB: nil *sql.DB")
	}
	// The doc comment above promises migrations already ran (the caller's
	// dbpkg.Open did it) — verify that promise rather than trust it blindly.
	// Cheap (one PRAGMA, once, not per batch) and it is exactly this
	// assumption bug-286ce8f7 showed can go quietly wrong.
	if err := verifyOtelSignalsSchema(database); err != nil {
		return nil, fmt.Errorf("NewWriterFromDB: %w", err)
	}
	return &Writer{db: database, shared: true, ownDB: false}, nil
}

// SQL for the five statements the Writer holds. Shared by both the
// own-pool lifetime-prepare path (prepare) and the shared-handle
// per-batch prepare path (prepareBatchStmts) so the two modes execute
// byte-identical SQL.
const (
	sqlInsertSignal = `
		INSERT OR IGNORE INTO otel_signals (
			signal_id, harness, session_id, prompt_id,
			trace_id, span_id, parent_span,
			kind, canonical, native, ts_micros,
			tool_name, tool_use_id, model, decision, decision_source,
			tokens_in, tokens_out, tokens_cache_read, tokens_cache_creation,
			tokens_thought, tokens_tool, tokens_reasoning,
			cost_usd, cost_source,
			duration_ms, success, error_msg, attempt, status_code,
			attrs_json, feature_id, agent_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	sqlSessionUpsert = `
		INSERT OR IGNORE INTO sessions (session_id, agent_assigned, status)
		VALUES (?, ?, 'active')`

	sqlResourceUpsert = `
		INSERT INTO otel_resource_attrs (session_id, harness, key, value, observed_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(session_id, key) DO UPDATE SET
			value = excluded.value,
			observed_at = excluded.observed_at`

	sqlPlaceholderInsert = `
		INSERT OR IGNORE INTO otel_signals (
			signal_id, harness, session_id,
			trace_id, span_id,
			kind, canonical, native, ts_micros,
			tool_name,
			attrs_json
		) VALUES (?, ?, ?, ?, ?, 'span', 'subagent_invocation', 'agent_invocation', ?, 'Agent', ?)`

	sqlUpgradePlaceholder = `
		UPDATE otel_signals SET
			signal_id = ?,
			harness = ?,
			prompt_id = ?,
			trace_id = ?,
			parent_span = ?,
			native = ?,
			ts_micros = ?,
			tool_name = ?,
			tool_use_id = ?,
			model = ?,
			decision = ?,
			decision_source = ?,
			tokens_in = ?,
			tokens_out = ?,
			tokens_cache_read = ?,
			tokens_cache_creation = ?,
			tokens_thought = ?,
			tokens_tool = ?,
			tokens_reasoning = ?,
			cost_usd = ?,
			cost_source = ?,
			duration_ms = ?,
			success = ?,
			error_msg = ?,
			attempt = ?,
			status_code = ?,
			attrs_json = ?,
			feature_id = ?,
			agent_id = ?
		WHERE span_id = ? AND attrs_json LIKE '%"_pending":true%'`
)

// stmtPreparer is satisfied by both *sql.Conn and *sql.DB, letting the
// statement-preparation helper serve the pinned-conn (own-pool) path and
// the per-batch acquired-conn (shared) path identically.
type stmtPreparer interface {
	PrepareContext(ctx context.Context, query string) (*sql.Stmt, error)
}

// prepareBatchStmts prepares the five statements on p (a pinned *sql.Conn
// in own-pool mode, or a per-batch acquired *sql.Conn in shared mode).
func prepareBatchStmts(ctx context.Context, p stmtPreparer) (*batchStmts, error) {
	bs := &batchStmts{}
	var err error
	if bs.insert, err = p.PrepareContext(ctx, sqlInsertSignal); err != nil {
		return nil, fmt.Errorf("prepare insert: %w", err)
	}
	if bs.sess, err = p.PrepareContext(ctx, sqlSessionUpsert); err != nil {
		bs.close()
		return nil, fmt.Errorf("prepare session upsert: %w", err)
	}
	if bs.res, err = p.PrepareContext(ctx, sqlResourceUpsert); err != nil {
		bs.close()
		return nil, fmt.Errorf("prepare resource upsert: %w", err)
	}
	if bs.placeholder, err = p.PrepareContext(ctx, sqlPlaceholderInsert); err != nil {
		bs.close()
		return nil, fmt.Errorf("prepare placeholder insert: %w", err)
	}
	if bs.upgrade, err = p.PrepareContext(ctx, sqlUpgradePlaceholder); err != nil {
		bs.close()
		return nil, fmt.Errorf("prepare upgrade stmt: %w", err)
	}
	return bs, nil
}

func (bs *batchStmts) close() {
	if bs == nil {
		return
	}
	for _, s := range []*sql.Stmt{bs.insert, bs.sess, bs.res, bs.placeholder, bs.upgrade} {
		if s != nil {
			s.Close()
		}
	}
}

// prepare prepares the lifetime statements on the pinned connection
// (own-pool mode only). Shared-handle writers prepare per-batch instead.
func (w *Writer) prepare() error {
	bs, err := prepareBatchStmts(context.Background(), w.conn)
	if err != nil {
		return err
	}
	w.insertStmt = bs.insert
	w.sessStmt = bs.sess
	w.resStmt = bs.res
	w.placeholderStmt = bs.placeholder
	w.upgradeStmt = bs.upgrade
	return nil
}

// Close releases prepared statements and the underlying connection.
//
// In shared mode the Writer holds no lifetime statements or pinned
// connection and does NOT own the *sql.DB (the daemon owns it), so Close
// is effectively a no-op — the owner closes the handle.
func (w *Writer) Close() error {
	if w.insertStmt != nil {
		w.insertStmt.Close()
	}
	if w.sessStmt != nil {
		w.sessStmt.Close()
	}
	if w.resStmt != nil {
		w.resStmt.Close()
	}
	if w.placeholderStmt != nil {
		w.placeholderStmt.Close()
	}
	if w.upgradeStmt != nil {
		w.upgradeStmt.Close()
	}
	if w.conn != nil {
		w.conn.Close()
	}
	if w.ownDB {
		return w.db.Close()
	}
	return nil
}

// PurgeStaleSubagentStarts removes pending_subagent_starts rows older than 24 h.
// Called on Writer startup and periodically to bound table growth.
func (w *Writer) PurgeStaleSubagentStarts() {
	dbpkg.PurgeStalePendingSubagentStarts(w.db)
}

// DB returns the underlying handle. Tests use this to assert row counts
// without opening a second connection (which would contend for the
// MaxOpenConns=1 writer lock).
func (w *Writer) DB() *sql.DB { return w.db }

func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func nullInt64(v int64) any {
	if v == 0 {
		return nil
	}
	return v
}
func nullInt(v int) any {
	if v == 0 {
		return nil
	}
	return v
}
func nullFloat(v float64) any {
	if v == 0 {
		return nil
	}
	return v
}

// valueString converts a resource-attribute AnyValue (already flattened
// to map[string]any by the decoder) into a string suitable for the
// otel_resource_attrs.value column. Non-scalar values are JSON-encoded.
func valueString(v any) (string, bool) {
	if v == nil {
		return "", false
	}
	switch x := v.(type) {
	case string:
		return x, true
	case bool:
		if x {
			return "true", true
		}
		return "false", true
	case int64:
		return fmt.Sprintf("%d", x), true
	case int:
		return fmt.Sprintf("%d", x), true
	case float64:
		return fmt.Sprintf("%g", x), true
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return "", false
		}
		return string(b), true
	}
}
