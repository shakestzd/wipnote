package db

import (
	"database/sql"
	"fmt"
	"strings"
)

// stepArchCards creates the arch_cards read-index table and its indexes.
// This is a derived read index — the canonical store is .wipnote/arch/*.md.
// Idempotent: CREATE TABLE/INDEX IF NOT EXISTS are no-ops on a DB that already
// has the table (e.g. a fresh DB where stepCreateBaseTables ran first).
func stepArchCards(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS arch_cards (
		slug           TEXT PRIMARY KEY,
		kind           TEXT NOT NULL,
		paths_json     TEXT NOT NULL DEFAULT '[]',
		verified_at    TEXT NOT NULL DEFAULT '',
		links_json     TEXT NOT NULL DEFAULT '[]',
		created_by     TEXT NOT NULL DEFAULT '',
		superseded_by  TEXT NOT NULL DEFAULT '',
		retired        INTEGER NOT NULL DEFAULT 0,
		body           TEXT NOT NULL DEFAULT '',
		created_at     DATETIME,
		updated_at     DATETIME,
		indexed_at     DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create arch_cards: %w", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_arch_cards_kind ON arch_cards(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_arch_cards_retired ON arch_cards(retired)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create arch_cards index: %w", err)
		}
	}
	return nil
}

// stepRecapsTable creates the recaps read-index table and its indexes. Recaps
// are committed HTML artifacts under .wipnote/recaps/; this table is a derived
// read index (the HTML stays canonical). Recaps carry a distinct shape from
// work items — grounding scope plus a source range/session — so they get a
// dedicated table rather than extending the features index.
// Idempotent: CREATE TABLE/INDEX IF NOT EXISTS are no-ops on a DB that already
// has the table (e.g. a fresh DB where stepCreateBaseTables ran first).
func stepRecapsTable(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS recaps (
		id            TEXT PRIMARY KEY,
		kind          TEXT NOT NULL DEFAULT '',
		input         TEXT NOT NULL DEFAULT '',
		git_range     TEXT NOT NULL DEFAULT '',
		grounded      INTEGER NOT NULL DEFAULT 0,
		title         TEXT NOT NULL DEFAULT '',
		outcome       TEXT NOT NULL DEFAULT '',
		work_item_id  TEXT NOT NULL DEFAULT '',
		created_at    DATETIME,
		updated_at    DATETIME,
		indexed_at    DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create recaps: %w", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_recaps_kind ON recaps(kind)`,
		`CREATE INDEX IF NOT EXISTS idx_recaps_work_item ON recaps(work_item_id)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create recaps index: %w", err)
		}
	}
	return nil
}

func stepSessionExecContext(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN exec_worktree_path TEXT`,
		`ALTER TABLE sessions ADD COLUMN branch TEXT`,
		`ALTER TABLE sessions ADD COLUMN harness TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add session exec-context column (%s): %w", stmt, err)
			}
		}
	}
	return nil
}

func stepSessionHandoffFields(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE sessions ADD COLUMN last_user_query_at DATETIME`,
		`ALTER TABLE sessions ADD COLUMN last_user_query TEXT`,
		`ALTER TABLE sessions ADD COLUMN handoff_notes TEXT`,
		`ALTER TABLE sessions ADD COLUMN recommended_next TEXT`,
		`ALTER TABLE sessions ADD COLUMN blockers JSON`,
		`ALTER TABLE sessions ADD COLUMN recommended_context JSON`,
		`ALTER TABLE sessions ADD COLUMN continued_from TEXT`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add session handoff column (%s): %w", stmt, err)
			}
		}
	}
	return nil
}

// stepPlanFeedbackAnnotationState adds the block-anchor + two-axis annotation
// state columns to plan_feedback (slice-8). Annotations reuse the existing
// plan_feedback table (action='annotation') rather than a new table: the FK to
// plan_id and the per-plan SSE/read loop are already wired here. The two axes —
// consumed (an agent has ingested the note) and resolved (the note has been
// addressed) — are independent, and resolution_target routes the note to
// 'agent' or 'human'. All columns are NULL-able / default-zero so existing rows
// and existing writers (approve/comment/answer) are unaffected.
func stepPlanFeedbackAnnotationState(db *sql.DB) error {
	// Legacy/partial DBs may not yet have plan_feedback (e.g. a DB seeded before
	// the table existed). The base schema creates it WITH these columns, so when
	// the table is absent here there is nothing to ALTER — skip rather than fail.
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='plan_feedback'`,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check plan_feedback existence: %w", err)
	}
	for _, stmt := range []string{
		`ALTER TABLE plan_feedback ADD COLUMN anchor TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE plan_feedback ADD COLUMN consumed INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE plan_feedback ADD COLUMN resolved INTEGER NOT NULL DEFAULT 0`,
		`ALTER TABLE plan_feedback ADD COLUMN resolution_target TEXT NOT NULL DEFAULT ''`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add plan_feedback annotation column (%s): %w", stmt, err)
			}
		}
	}
	return nil
}

// stepBackfillTotalEvents recomputes sessions.total_events for every session
// from the actual agent_events rows. This repairs DBs that recorded agent_events
// while trg_increment_total_events was absent (e.g. after step 10 recreated the
// trigger but before this step ran) — their total_events counts remained stale.
//
// The backfill exactly mirrors what the trigger does: for each inserted
// agent_events row the trigger increments total_events by 1 where
// agent_events.session_id = sessions.session_id (no additional filter).
// The COUNT(*) correlated subquery reproduces that accumulation in bulk.
//
// Idempotent: recomputing an already-correct count yields the same value.
// Safe on a fresh DB (no sessions → zero rows updated).
func stepBackfillTotalEvents(db *sql.DB) error {
	_, err := db.Exec(`UPDATE sessions
		SET total_events = (
			SELECT COUNT(*)
			FROM agent_events
			WHERE agent_events.session_id = sessions.session_id
		)`)
	if err != nil {
		return fmt.Errorf("backfill total_events: %w", err)
	}
	return nil
}

// stepGitCommitsCompositeKey widens git_commits' primary key from
// (commit_hash, session_id) to (commit_hash, session_id, feature_id).
//
// Why: a single commit can legitimately name multiple work items (e.g. a
// trailer block "Refs: feat-a, feat-b", or two IDs in one subject line).
// Every ingestion path (trailer scan, hook capture, bulk backfill) uses
// INSERT OR IGNORE keyed on the primary key. Under the old two-column key,
// the trailer-ingest path writes every row under a single constant
// session_id ("trailer-ingest"), so the second and later feature_id for the
// same commit collided with the first insert and was silently dropped
// (bug-3bf05d49).
//
// feature_id is normalized to the empty string (never NULL) so it can safely sit in a
// composite key: SQLite's PRIMARY KEY does not imply NOT NULL, and two NULLs
// are never equal under a UNIQUE index — a nullable PK column would silently
// defeat INSERT OR IGNORE's de-duplication for unattributed commits.
func stepGitCommitsCompositeKey(db *sql.DB) error {
	var currentSQL string
	err := db.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name='git_commits'`,
	).Scan(&currentSQL)
	if err != nil {
		// Table doesn't exist yet -- CreateAllTables will create it correctly.
		return nil
	}
	if strings.Contains(currentSQL, "PRIMARY KEY (commit_hash, session_id, feature_id)") {
		return nil // already migrated
	}

	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck

	if _, err := tx.Exec(`
		CREATE TABLE git_commits_new (
			commit_hash TEXT NOT NULL,
			session_id TEXT NOT NULL,
			feature_id TEXT NOT NULL DEFAULT '',
			tool_event_id TEXT,
			message TEXT,
			timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (commit_hash, session_id, feature_id)
		)`); err != nil {
		return fmt.Errorf("create git_commits_new: %w", err)
	}

	// The old PK (commit_hash, session_id) guarantees at most one row per pair
	// in the source table, so this copy cannot collide against the new,
	// wider PK -- it only unlocks capacity for future inserts. It does NOT
	// recover feature_ids already dropped by the old collision; re-run
	// `wipnote reindex --full` afterwards to re-derive those from source.
	if _, err := tx.Exec(`
		INSERT INTO git_commits_new
			(commit_hash, session_id, feature_id, tool_event_id, message, timestamp)
		SELECT commit_hash, session_id, COALESCE(feature_id, ''), tool_event_id, message, timestamp
		FROM git_commits`); err != nil {
		return fmt.Errorf("copy git_commits data: %w", err)
	}

	if _, err := tx.Exec(`DROP TABLE git_commits`); err != nil {
		return fmt.Errorf("drop old git_commits: %w", err)
	}
	if _, err := tx.Exec(`ALTER TABLE git_commits_new RENAME TO git_commits`); err != nil {
		return fmt.Errorf("rename git_commits_new: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit git_commits migration: %w", err)
	}

	// Reinstall idx_git_commits_feature, dropped by DROP TABLE above.
	if err := CreateAllIndexes(db); err != nil {
		return fmt.Errorf("reinstall indexes after git_commits migration: %w", err)
	}
	return nil
}

// stepOtelSignalsAttributionColumns adds the otel_signals.feature_id and
// otel_signals.agent_id columns (and their partial indexes) as a properly
// versioned step, correcting bug-286ce8f7 (filed against team-lead's
// bug-6f0f0b3a): both columns used to be added via an idempotent ALTER
// embedded directly in CreateOtelTables, which core/db is only reachable
// from step version 1 (stepCreateBaseTables). A step at version 1 runs
// exactly once -- the first time a database goes from user_version 0 to
// current -- and never again once a database has reached
// currentSchemaVersion. Any column added inside CreateOtelTables AFTER a
// given database was already fully migrated therefore never applies to
// that database, no matter which binary or which entry point (serve,
// hooks, reindex) opens it afterward. This bit feature_id for roughly four
// months before an incidental full-DB rebuild happened to apply it, and bit
// agent_id (feat-be696acc) immediately, because no such rebuild had
// happened yet -- confirmed live against this repo's own dev database
// (user_version already at 18, feature_id present, agent_id absent).
//
// Both ALTERs are safe to re-run here even on a database that already has
// one or both columns (from an incidental full rebuild, as above): duplicate
// column errors are swallowed the same way every other column-migration step
// in this file does it.
//
// Legacy/partial DBs may not have otel_signals at all yet (a hand-seeded test
// fixture that jumps straight to a mid-chain user_version, skipping step 1).
// The base schema (CreateOtelTables, step 1) creates the table WITH these
// columns for any database that runs the full chain, so when the table is
// absent here there is nothing to ALTER — skip rather than fail, matching
// stepPlanFeedbackAnnotationState and stepGitCommitsCompositeKey.
func stepOtelSignalsAttributionColumns(db *sql.DB) error {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='otel_signals'`,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check otel_signals existence: %w", err)
	}

	addCols := []string{
		`ALTER TABLE otel_signals ADD COLUMN feature_id TEXT`,
		`ALTER TABLE otel_signals ADD COLUMN agent_id TEXT`,
	}
	for _, stmt := range addCols {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add column (%s): %w", stmt, err)
			}
		}
	}
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_otel_feature_ts ON otel_signals(feature_id, ts_micros) WHERE feature_id IS NOT NULL`,
		`CREATE INDEX IF NOT EXISTS idx_otel_agent_ts ON otel_signals(agent_id, ts_micros) WHERE agent_id IS NOT NULL`,
	}
	for _, stmt := range indexes {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create index (%s): %w", stmt, err)
		}
	}
	return nil
}
