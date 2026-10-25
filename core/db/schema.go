package db

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"
)

// Open opens (or creates) an wipnote SQLite database at the given path,
// applies performance PRAGMAs, and runs any pending schema migrations.
//
// Migrations are version-gated by PRAGMA user_version: a database whose
// user_version already equals currentSchemaVersion takes the warm-open fast
// path and executes ZERO CREATE / ALTER / DROP / trigger / normalisation
// statements. This eliminates the write-lock contention that DDL re-execution
// caused in short-lived hook and CLI processes.
//
// Brand-new databases (user_version = 0) run every registered migration in
// order, then land at currentSchemaVersion. Legacy databases at any
// intermediate version run only the missing migrations and advance to current.
// The full migration registry lives in migrations.go.
func Open(dbPath string) (*sql.DB, error) {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("creating db directory: %w", err)
	}

	// Embed busy_timeout in the DSN so it is applied on the first connection
	// open, before any schema queries. This prevents SQLITE_BUSY errors during
	// startup races when the OTel receiver writer or a hook binary holds the
	// write lock. ApplyPragmas below re-applies the full set for completeness
	// (idempotent).
	// Skip DSN-level pragma for in-memory DBs; the driver will reject it.
	dsn := dbPath
	isInMemory := strings.Contains(dbPath, ":memory:")
	if !isInMemory {
		dsn = dsn + "?_pragma=busy_timeout(5000)&_txlock=immediate"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("opening database: %w", err)
	}

	if err := ApplyPragmas(db, BuildPragmas(dbPath)); err != nil {
		db.Close()
		return nil, fmt.Errorf("applying pragmas: %w", err)
	}

	// bug-7dbaf552: runMigrations performs the user_version write (and, for a
	// brand-new / legacy DB, DDL). On a contended DELETE-journal database the
	// SHARED→RESERVED upgrade can transiently SQLITE_BUSY even with
	// busy_timeout set. Wrapping the whole migration step in RetryOnBusy makes
	// EVERY dbpkg.Open caller (lineage/query/session/workitem/...) resilient
	// without each having to hand-roll its own loop. runMigrations is
	// version-gated and idempotent, so re-running it is safe. Same-package
	// call: RetryOnBusy + DefaultBusyBackoff live in busy_retry.go.
	if err := RetryOnBusy(DefaultBusyBackoff, func() error {
		return runMigrations(db)
	}); err != nil {
		db.Close()
		return nil, fmt.Errorf("running migrations: %w", err)
	}

	return db, nil
}

// CreateAllTables creates every wipnote table if it does not already exist.
// Mirrors create_all_tables() from Python ddl.py.
func CreateAllTables(db *sql.DB) error {
	for _, stmt := range allTablesDDL {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("exec DDL: %w\nSQL: %.120s", err, stmt)
		}
	}
	return nil
}

// CreateAllIndexes creates performance indexes matching Python ddl.py.
func CreateAllIndexes(db *sql.DB) error {
	indexes := []string{
		// agent_events
		"CREATE INDEX IF NOT EXISTS idx_agent_events_session_ts_desc ON agent_events(session_id, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_agent_ts_desc ON agent_events(agent_id, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_agent ON agent_events(agent_id)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_type ON agent_events(event_type)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_parent_event ON agent_events(parent_event_id)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_task_delegation ON agent_events(event_type, subagent_type, timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_session_tool ON agent_events(session_id, tool_name)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_timestamp ON agent_events(timestamp DESC)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_claude_task_id ON agent_events(claude_task_id)",
		"CREATE INDEX IF NOT EXISTS idx_agent_events_step_id ON agent_events(step_id)",
		// features
		"CREATE INDEX IF NOT EXISTS idx_features_status_priority ON features(status, priority DESC, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_features_track_priority ON features(track_id, priority DESC, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_features_assigned ON features(assigned_to)",
		"CREATE INDEX IF NOT EXISTS idx_features_parent ON features(parent_feature_id)",
		"CREATE INDEX IF NOT EXISTS idx_features_type ON features(type)",
		"CREATE INDEX IF NOT EXISTS idx_features_created ON features(created_at DESC)",
		// sessions
		"CREATE INDEX IF NOT EXISTS idx_sessions_agent_created ON sessions(agent_assigned, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_family ON sessions(session_family_id)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_status_created ON sessions(status, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_parent ON sessions(parent_session_id, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at DESC)",
		// tracks
		"CREATE INDEX IF NOT EXISTS idx_tracks_status_created ON tracks(status, created_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_tracks_priority ON tracks(priority DESC)",
		// claims
		"CREATE INDEX IF NOT EXISTS idx_claims_work_item ON claims(work_item_id)",
		"CREATE INDEX IF NOT EXISTS idx_claims_session ON claims(owner_session_id)",
		"CREATE INDEX IF NOT EXISTS idx_claims_status ON claims(status)",
		"CREATE INDEX IF NOT EXISTS idx_claims_lease_expiry ON claims(lease_expires_at)",
		// graph_edges
		"CREATE INDEX IF NOT EXISTS idx_edges_from ON graph_edges(from_node_id)",
		"CREATE INDEX IF NOT EXISTS idx_edges_to ON graph_edges(to_node_id)",
		"CREATE INDEX IF NOT EXISTS idx_edges_type ON graph_edges(relationship_type)",
		// git_commits
		"CREATE INDEX IF NOT EXISTS idx_git_commits_feature ON git_commits(feature_id)",
		// live_events
		"CREATE INDEX IF NOT EXISTS idx_live_events_pending ON live_events(broadcast_at) WHERE broadcast_at IS NULL",
		"CREATE INDEX IF NOT EXISTS idx_live_events_created ON live_events(created_at DESC)",
		// agent_lineage_trace
		"CREATE INDEX IF NOT EXISTS idx_lineage_root ON agent_lineage_trace(root_session_id)",
		"CREATE INDEX IF NOT EXISTS idx_lineage_session ON agent_lineage_trace(session_id)",
		// messages
		"CREATE INDEX IF NOT EXISTS idx_messages_session_ord ON messages(session_id, ordinal)",
		"CREATE INDEX IF NOT EXISTS idx_messages_session_role ON messages(session_id, role)",
		"CREATE INDEX IF NOT EXISTS idx_messages_timestamp ON messages(timestamp DESC)",
		// tool_calls
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_message ON tool_calls(message_id)",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_name ON tool_calls(tool_name)",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_category ON tool_calls(category)",
		"CREATE INDEX IF NOT EXISTS idx_tool_calls_feature ON tool_calls(feature_id)",
		// feature_files
		"CREATE INDEX IF NOT EXISTS idx_feature_files_feature ON feature_files(feature_id)",
		"CREATE INDEX IF NOT EXISTS idx_feature_files_path ON feature_files(file_path)",
		// Composite (file_path, last_seen) — supports the live file-overlap
		// query (Tier 1): an equality+range probe on (file_path, recent
		// last_seen) without range-scanning every row for the path.
		"CREATE INDEX IF NOT EXISTS idx_feature_files_path_seen ON feature_files(file_path, last_seen)",
		// session_files
		"CREATE INDEX IF NOT EXISTS idx_session_files_session ON session_files(session_id)",
		"CREATE INDEX IF NOT EXISTS idx_session_files_path ON session_files(file_path)",
		// arch_cards
		"CREATE INDEX IF NOT EXISTS idx_arch_cards_kind ON arch_cards(kind)",
		"CREATE INDEX IF NOT EXISTS idx_arch_cards_retired ON arch_cards(retired)",
		// claim_episodes — the point query is (agent, timestamp) → work item, so
		// the covering index leads with agent_id and orders by started_at.
		"CREATE INDEX IF NOT EXISTS idx_claim_episodes_agent_start ON claim_episodes(agent_id, started_at)",
		"CREATE INDEX IF NOT EXISTS idx_claim_episodes_session_agent ON claim_episodes(session_id, agent_id, started_at)",
		"CREATE INDEX IF NOT EXISTS idx_claim_episodes_work_item ON claim_episodes(work_item_id, started_at)",
		// plan_feedback
		"CREATE INDEX IF NOT EXISTS idx_plan_feedback_plan_id ON plan_feedback(plan_id)",
		"CREATE INDEX IF NOT EXISTS idx_plan_feedback_section ON plan_feedback(plan_id, section)",
		// gate_records
		"CREATE INDEX IF NOT EXISTS idx_gate_records_session_checked ON gate_records(session_id, checked_at DESC)",
		"CREATE INDEX IF NOT EXISTS idx_gate_records_work_item_checked ON gate_records(work_item_id, checked_at DESC)",
		// PARTIAL unique index: it makes the ledger projection idempotent (INSERT
		// OR IGNORE on a record already projected) while leaving legacy rows, which
		// all share record_id '', outside the constraint entirely.
		"CREATE UNIQUE INDEX IF NOT EXISTS idx_gate_records_record_id ON gate_records(record_id) WHERE record_id != ''",
		// recaps
		"CREATE INDEX IF NOT EXISTS idx_recaps_kind ON recaps(kind)",
		"CREATE INDEX IF NOT EXISTS idx_recaps_work_item ON recaps(work_item_id)",
	}

	for _, idx := range indexes {
		if _, err := db.Exec(idx); err != nil {
			// Non-fatal: silently continue (matches Python behaviour).
			// Do NOT log to stderr — Claude Code treats any hook stderr as an error.
			_ = err
		}
	}
	return nil
}

// migrateAgentEventsAddCheckConstraint adds the attribution CHECK constraint to
// the existing agent_events table via copy-and-swap, and also drops the
// self-referential FK on parent_event_id (which caused silent insert failures
// when the parent row didn't exist yet — see bug-89990f33). It is idempotent:
// it only runs when the live DDL requires changes.
//
// The migration uses dynamic column introspection (PRAGMA table_info) to detect
// and preserve columns added by later migrations (reason, teammate_name, team_name,
// prompt_id, etc.), ensuring no data loss during the copy-and-swap.
func migrateAgentEventsAddCheckConstraint(db *sql.DB) error {
	// Check if the live table DDL requires changes.
	var currentSQL string
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='agent_events'`,
	).Scan(&currentSQL)
	if err != nil {
		// Table doesn't exist yet — CreateAllTables will create it correctly.
		return nil
	}

	needsCheck := !strings.Contains(currentSQL, "tool_name != 'UserQuery'")
	// The parent_event_id self-referential FK causes silent insert drops when the
	// parent row hasn't been written yet (timing race). Drop it.
	hasParentEventFK := strings.Contains(currentSQL, "REFERENCES agent_events(event_id)")
	if !needsCheck && !hasParentEventFK {
		// Both the check constraint is present and the FK is already gone — nothing to do.
		return nil
	}

	// Disable foreign keys for the duration of the swap.
	if _, err := db.Exec(`PRAGMA foreign_keys = OFF`); err != nil {
		return fmt.Errorf("disable foreign_keys: %w", err)
	}
	defer db.Exec(`PRAGMA foreign_keys = ON`) //nolint:errcheck

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	// 1. Introspect the live agent_events table to preserve ALL columns.
	rows, err := tx.Query(`PRAGMA table_info(agent_events)`)
	if err != nil {
		return fmt.Errorf("pragma table_info: %w", err)
	}
	defer rows.Close()

	type columnInfo struct {
		name     string
		typeName string
		notnull  int
		dfltVal  *string
		pk       int
	}
	var columns []columnInfo
	var colNames []string // for SELECT clause
	for rows.Next() {
		var cid int
		var name, typeName string
		var notnull int
		var dfltVal *string
		var pk int
		if err := rows.Scan(&cid, &name, &typeName, &notnull, &dfltVal, &pk); err != nil {
			return fmt.Errorf("scan column info: %w", err)
		}
		columns = append(columns, columnInfo{
			name:     name,
			typeName: typeName,
			notnull:  notnull,
			dfltVal:  dfltVal,
			pk:       pk,
		})
		colNames = append(colNames, name)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate table_info: %w", err)
	}

	// 2. Build the new DDL dynamically from the introspected columns.
	// Preserve the CHECK constraint, session_id FK, and feature_id FK.
	// OMIT the parent_event_id FK (self-referential) — this is the whole point.
	ddlParts := []string{"CREATE TABLE agent_events_new ("}
	for i, col := range columns {
		ddlParts = append(ddlParts, col.name+" "+col.typeName)
		if col.notnull == 1 {
			ddlParts = append(ddlParts, " NOT NULL")
		}
		if col.dfltVal != nil {
			ddlParts = append(ddlParts, " DEFAULT "+*col.dfltVal)
		}
		if col.pk == 1 {
			ddlParts = append(ddlParts, " PRIMARY KEY")
		}
		if i < len(columns)-1 {
			ddlParts = append(ddlParts, ",")
		}
	}
	// Add the CHECK constraint and foreign keys (except parent_event_id FK).
	ddlParts = append(ddlParts, `,
	CHECK (NOT (event_type = 'tool_call' AND agent_id = 'human' AND (tool_name IS NULL OR tool_name != 'UserQuery'))),
	FOREIGN KEY (session_id) REFERENCES sessions(session_id) ON DELETE CASCADE ON UPDATE CASCADE,
	FOREIGN KEY (feature_id) REFERENCES features(id) ON DELETE SET NULL ON UPDATE CASCADE`)
	ddlParts = append(ddlParts, ")")
	newTableDDL := strings.Join(ddlParts, "")

	if _, err := tx.Exec(newTableDDL); err != nil {
		return fmt.Errorf("create agent_events_new: %w", err)
	}

	// 3. Copy all rows. Build the INSERT...SELECT with the dynamically derived column list.
	// This preserves all columns (including those added by later migrations).
	selectClause := strings.Join(colNames, ",")
	copySQL := fmt.Sprintf(`
		INSERT OR IGNORE INTO agent_events_new
		SELECT %s FROM agent_events`, selectClause)
	if _, err := tx.Exec(copySQL); err != nil {
		return fmt.Errorf("copy rows to agent_events_new: %w", err)
	}

	// 4. Drop the old table and rename the new one.
	if _, err := tx.Exec(`DROP TABLE agent_events`); err != nil {
		return fmt.Errorf("drop agent_events: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE agent_events_new RENAME TO agent_events`); err != nil {
		return fmt.Errorf("rename agent_events_new: %w", err)
	}

	return tx.Commit()
}
