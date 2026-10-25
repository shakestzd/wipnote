package receiver

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
)

// requiredOtelSignalsColumns lists every otel_signals column sqlInsertSignal
// references, kept as an explicit list (rather than parsed out of the SQL
// string) so a future column addition is a deliberate two-place edit, not an
// implicit one. TestRequiredOtelSignalsColumnsMatchInsert asserts this list
// and sqlInsertSignal's own column list stay in sync.
var requiredOtelSignalsColumns = []string{
	"signal_id", "harness", "session_id", "prompt_id",
	"trace_id", "span_id", "parent_span",
	"kind", "canonical", "native", "ts_micros",
	"tool_name", "tool_use_id", "model", "decision", "decision_source",
	"tokens_in", "tokens_out", "tokens_cache_read", "tokens_cache_creation",
	"tokens_thought", "tokens_tool", "tokens_reasoning",
	"cost_usd", "cost_source",
	"duration_ms", "success", "error_msg", "attempt", "status_code",
	"attrs_json", "feature_id", "agent_id",
}

// verifyOtelSignalsSchema is the fail-loudly backstop for bug-286ce8f7: a
// schema-versioning gap silently left otel_signals.agent_id missing on an
// already-migrated database, and the resulting error surfaced only as one
// swallowed-and-logged failure per WriteBatch call — effectively per signal,
// since the reindex path batches one signal at a time — thousands of times,
// with the overall process completing "successfully" throughout. Whatever
// the cause (a versioning gap like this one, a downgrade, manual DB surgery),
// a writer that depends on a column it does not have should say so once,
// here, at construction, and refuse to start — not degrade silently row by
// row. This runs once per Writer, not per batch, so it does not sit in the
// hot path RunMigrations/prepare already dominate.
func verifyOtelSignalsSchema(db *sql.DB) error {
	rows, err := db.Query(`PRAGMA table_info(otel_signals)`)
	if err != nil {
		return fmt.Errorf("verify otel_signals schema: %w", err)
	}
	defer rows.Close()

	have := make(map[string]bool, len(requiredOtelSignalsColumns))
	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notNull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notNull, &dfltValue, &pk); err != nil {
			return fmt.Errorf("verify otel_signals schema: scan table_info: %w", err)
		}
		have[name] = true
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("verify otel_signals schema: %w", err)
	}

	var missing []string
	for _, col := range requiredOtelSignalsColumns {
		if !have[col] {
			missing = append(missing, col)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf(
			"otel_signals is missing column(s) %v that every INSERT depends on — "+
				"a schema migration did not apply (see core/db/migrations.go); "+
				"refusing to start rather than fail silently on every write",
			missing)
	}
	return nil
}

// assertWriterJournalMode promotes a fresh DB from the SQLite default
// (DELETE) to WAL when — and only when — the backing filesystem is WAL-safe.
// It is a no-op when the DB is already in WAL or when the filesystem is
// WAL-unsafe (the correct, filesystem-agnostic outcome). All failures are
// swallowed: journal-mode resolution is best-effort and the read-pool Open
// remains the authoritative pragma applier.
func assertWriterJournalMode(conn *sql.Conn, dbPath string) {
	ctx := context.Background()
	var mode string
	if err := conn.QueryRowContext(ctx, "PRAGMA journal_mode").Scan(&mode); err != nil {
		return
	}
	if strings.EqualFold(mode, "wal") {
		return // already WAL — nothing to do
	}
	if _, walSafe := dbpkg.ProbeFsType(dbPath); !walSafe {
		return // WAL-unsafe FS (overlayfs/FUSE/9p/NFS): DELETE is correct
	}
	// WAL-safe filesystem sitting in DELETE only because this writer is the
	// cold-start connection. Promote to WAL so readers and the writer stop
	// blocking each other for the rest of the DB file's life.
	_, _ = conn.ExecContext(ctx, "PRAGMA journal_mode = WAL")
}
