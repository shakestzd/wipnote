package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// ---- migration step implementations -----------------------------------------

// stepPostInitialColumnsAndTables collects every column / table / trigger that
// was added after the initial schema landed. Each operation is independently
// idempotent (ALTER TABLE ADD COLUMN swallows "duplicate column"; CREATE TABLE
// IF NOT EXISTS, CREATE INDEX IF NOT EXISTS, CREATE TRIGGER IF NOT EXISTS, and
// DROP TABLE IF EXISTS are all no-ops on second run).
//
// This step does NOT include the agent_events copy-and-swap (step 4) or the
// post-swap columns (teammate_name / team_name / prompt_id, which must run
// AFTER the swap to avoid being lost during the column copy). Those live in
// step 4.
func stepPostInitialColumnsAndTables(db *sql.DB) error {
	// Idempotent column additions on existing tables.
	addCols := []string{
		`ALTER TABLE sessions ADD COLUMN title TEXT`,
		`ALTER TABLE sessions ADD COLUMN active_feature_id TEXT`,
		`ALTER TABLE sessions ADD COLUMN updated_at DATETIME`,
		`ALTER TABLE agent_events ADD COLUMN subagent_type TEXT`,
		`ALTER TABLE agent_events ADD COLUMN reason TEXT`,
		`ALTER TABLE sessions ADD COLUMN git_remote_url TEXT`,
		`ALTER TABLE sessions ADD COLUMN project_dir TEXT`,
		`ALTER TABLE tool_calls ADD COLUMN feature_id TEXT`,
		`ALTER TABLE messages ADD COLUMN agent_id TEXT`,
		`ALTER TABLE claims ADD COLUMN claimed_by_agent_id TEXT DEFAULT ""`,
	}
	for _, stmt := range addCols {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add column (%s): %w", stmt, err)
			}
		}
	}

	// active_work_items: per-agent claim attribution.
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS active_work_items (
		session_id    TEXT NOT NULL,
		agent_id      TEXT NOT NULL,
		work_item_id  TEXT NOT NULL,
		claimed_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		PRIMARY KEY (session_id, agent_id)
	)`); err != nil {
		return fmt.Errorf("create active_work_items: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_active_work_items_work_item
		ON active_work_items(work_item_id)`); err != nil {
		return fmt.Errorf("create idx_active_work_items_work_item: %w", err)
	}

	// Drop deprecated tables replaced by the claims system.
	if _, err := db.Exec(`DROP TABLE IF EXISTS agent_collaboration`); err != nil {
		return fmt.Errorf("drop agent_collaboration: %w", err)
	}
	if _, err := db.Exec(`DROP TABLE IF EXISTS agent_presence`); err != nil {
		return fmt.Errorf("drop agent_presence: %w", err)
	}

	// Trigger: auto-increment sessions.total_events on each agent_event insert.
	if err := createTriggerIncrementTotalEvents(db); err != nil {
		return err
	}

	// Backfill total_events for sessions that pre-date the trigger.
	if _, err := db.Exec(`UPDATE sessions SET total_events = (
		SELECT COUNT(*) FROM agent_events WHERE agent_events.session_id = sessions.session_id
	) WHERE total_events = 0 AND EXISTS (
		SELECT 1 FROM agent_events WHERE agent_events.session_id = sessions.session_id
	)`); err != nil {
		return fmt.Errorf("backfill total_events: %w", err)
	}
	return nil
}

func stepGateRecords(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS gate_records (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		session_id TEXT NOT NULL,
		work_item_id TEXT,
		harness TEXT,
		project_type TEXT NOT NULL,
		gate_command TEXT NOT NULL,
		status TEXT NOT NULL CHECK(status IN ('pass','fail')),
		checked_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
		signature TEXT NOT NULL,
		allowlist_hits_json TEXT NOT NULL DEFAULT '[]',
		allowlist_hit_count INTEGER NOT NULL DEFAULT 0,
		source TEXT NOT NULL DEFAULT 'check',
		output_summary TEXT
	)`); err != nil {
		return fmt.Errorf("create gate_records: %w", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_gate_records_session_checked ON gate_records(session_id, checked_at DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_gate_records_work_item_checked ON gate_records(work_item_id, checked_at DESC)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create gate_records index: %w", err)
		}
	}
	return nil
}

// stepAgentEventsCopySwap runs the agent_events CHECK-constraint + self-FK-drop
// migration and the three post-swap columns (teammate_name, team_name,
// prompt_id) that must be added AFTER the swap so they aren't dropped during
// the column copy. The copy-swap helper checks the live DDL and short-circuits
// when no changes are required, so this step is idempotent on already-migrated
// DBs.
//
// agent_events indexes are reinstalled after the swap because the DROP TABLE
// inside migrateAgentEventsAddCheckConstraint discards the table's indexes.
// CreateAllIndexes uses CREATE INDEX IF NOT EXISTS for all index sets, so it
// is safe to re-run for every table (the only ones that actually missed indexes
// are the agent_events set).
func stepAgentEventsCopySwap(db *sql.DB) error {
	if err := migrateAgentEventsAddCheckConstraint(db); err != nil {
		return fmt.Errorf("agent_events copy-swap: %w", err)
	}

	postSwapCols := []string{
		`ALTER TABLE agent_events ADD COLUMN teammate_name TEXT`,
		`ALTER TABLE agent_events ADD COLUMN team_name TEXT`,
		// OTel correlation: stable per-turn identifier bridged from OTel signals.
		`ALTER TABLE agent_events ADD COLUMN prompt_id TEXT`,
	}
	for _, stmt := range postSwapCols {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add column after copy-swap (%s): %w", stmt, err)
			}
		}
	}

	// Reinstall indexes lost during the DROP TABLE inside the swap.
	// CreateAllIndexes is fully idempotent (CREATE INDEX IF NOT EXISTS).
	if err := CreateAllIndexes(db); err != nil {
		return fmt.Errorf("reinstall indexes after copy-swap: %w", err)
	}

	// Recreate trg_increment_total_events: the DROP TABLE inside the swap
	// silently destroyed the trigger that was attached to agent_events.
	// Without recreation sessions.total_events stops incrementing after migration.
	if err := createTriggerIncrementTotalEvents(db); err != nil {
		return fmt.Errorf("recreate trigger after copy-swap: %w", err)
	}
	return nil
}

// stepNormalizePlanFeedback rewrites legacy plan_feedback value strings
// ('approved' / 'rejected' / 'changes_requested') to the canonical boolean
// strings ('true' / 'false'). Idempotent: once migrated no rows match the
// WHERE clauses.
func stepNormalizePlanFeedback(db *sql.DB) error {
	return NormalizePlanFeedbackValues(db)
}

// isDuplicateColumnError reports whether err is the "duplicate column" error
// that SQLite returns when ALTER TABLE ADD COLUMN names an already-present
// column. The migration runner uses this to keep the apply function quiet on
// idempotent re-runs.
func isDuplicateColumnError(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "duplicate column name")
}

// triggerIncrementTotalEventsSQL is the single source of truth for the
// trg_increment_total_events trigger DDL. Both the initial creation path
// (stepPostInitialColumnsAndTables) and the post-copy-swap recreation path
// (stepAgentEventsCopySwap) use this constant — never copy-paste the SQL.
const triggerIncrementTotalEventsSQL = `CREATE TRIGGER IF NOT EXISTS trg_increment_total_events
	AFTER INSERT ON agent_events
	FOR EACH ROW
	BEGIN
		UPDATE sessions
		SET total_events = total_events + 1
		WHERE session_id = NEW.session_id;
	END`

// createTriggerIncrementTotalEvents (re)creates the trg_increment_total_events
// trigger. Safe to call after a copy-swap that dropped the trigger — the
// IF NOT EXISTS clause makes it idempotent on already-migrated DBs.
func createTriggerIncrementTotalEvents(db *sql.DB) error {
	if _, err := db.Exec(triggerIncrementTotalEventsSQL); err != nil {
		return fmt.Errorf("create trg_increment_total_events: %w", err)
	}
	return nil
}

// stepSessionFamilyID adds the session_family_id column to sessions and creates
// an index for efficient family-based lookups. Also backfills existing rows so
// that any session without a family_id uses its own session_id as the family
// (each pre-existing session is its own family of one). Idempotent: the ALTER
// TABLE ADD COLUMN is swallowed by isDuplicateColumnError on re-run.
func stepSessionFamilyID(db *sql.DB) error {
	if _, err := db.Exec("ALTER TABLE sessions ADD COLUMN session_family_id TEXT"); err != nil {
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("add session_family_id: %w", err)
		}
	}
	// Backfill: existing sessions without a family get their own session_id as
	// the family_id so they remain queryable by family without NULL handling.
	if _, err := db.Exec("UPDATE sessions SET session_family_id = session_id WHERE session_family_id IS NULL OR session_family_id = ''"); err != nil {
		return fmt.Errorf("backfill session_family_id: %w", err)
	}
	// Index for efficient family-based grouping queries.
	if _, err := db.Exec("CREATE INDEX IF NOT EXISTS idx_sessions_family ON sessions(session_family_id)"); err != nil {
		return fmt.Errorf("create idx_sessions_family: %w", err)
	}
	return nil
}

// stepSessionFiles creates the session_files table (claimless file-touch
// visibility, feat-793844bd slice-4) and its lookup indexes. Idempotent:
// CREATE TABLE/INDEX IF NOT EXISTS are no-ops on a DB that already has them
// (e.g. a fresh DB where stepCreateBaseTables already created the table from
// CreateAllTables). Legacy DBs at user_version<8 that pre-date the table get
// it created here.
func stepSessionFiles(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS session_files (
		id TEXT PRIMARY KEY,
		session_id TEXT NOT NULL,
		file_path TEXT NOT NULL,
		operation TEXT NOT NULL DEFAULT 'unknown',
		first_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		last_seen DATETIME DEFAULT CURRENT_TIMESTAMP,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
		UNIQUE(session_id, file_path)
	)`); err != nil {
		return fmt.Errorf("create session_files: %w", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_session_files_session ON session_files(session_id)`,
		`CREATE INDEX IF NOT EXISTS idx_session_files_path ON session_files(file_path)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create session_files index: %w", err)
		}
	}
	return nil
}

// stepFeatureFilesPathSeenIndex adds the composite (file_path, last_seen)
// index on feature_files (live file-overlap detection, Tier 1 / feat-b5fa9392).
// Only idx_feature_files_path on (file_path) existed before; the overlap query
// filters by file_path AND a recent last_seen window, so without the trailing
// last_seen column the planner range-scans every row for the path. Idempotent:
// CREATE INDEX IF NOT EXISTS is a no-op on a DB that already has it (e.g. a
// fresh DB where stepCreateIndexes created it from CreateAllIndexes).
func stepFeatureFilesPathSeenIndex(db *sql.DB) error {
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_feature_files_path_seen ON feature_files(file_path, last_seen)`,
	); err != nil {
		return fmt.Errorf("create idx_feature_files_path_seen: %w", err)
	}
	return nil
}

// stepRepairTriggerIncrementTotalEvents ensures trg_increment_total_events
// exists on every database, regardless of starting version.
//
// The bug-045124a6 fix recreated this trigger inside stepAgentEventsCopySwap
// (step 4), which only ran on DBs that had not yet completed step 4. Any DB
// already at user_version >= 4 that lost the trigger (e.g. due to manual DROP,
// or a buggy old migration) remained broken. This new step runs unconditionally
// on all DBs still below version 10 — including those already at version 9 —
// and (re)creates the trigger using CREATE TRIGGER IF NOT EXISTS, which is a
// no-op on DBs that still have it and a repair on DBs that lost it.
func stepRepairTriggerIncrementTotalEvents(db *sql.DB) error {
	return createTriggerIncrementTotalEvents(db)
}
