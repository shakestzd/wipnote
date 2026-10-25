package db

import (
	"database/sql"
	"fmt"
)

// stepDelegationRunsTable creates the delegation_runs read index for the
// canonical delegations ledger (feat-dd8b0cde). Both this step and
// CreateAllTables declare the same DDL, as for claim_episodes and recaps, so a
// database already past version 1 still gets the table. Idempotent.
func stepDelegationRunsTable(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS delegation_runs (
		run_id          TEXT PRIMARY KEY,
		mode            TEXT NOT NULL,
		harness         TEXT NOT NULL,
		name            TEXT NOT NULL DEFAULT '',
		label           TEXT NOT NULL DEFAULT '',
		work_item_id    TEXT NOT NULL DEFAULT '',
		link            TEXT NOT NULL DEFAULT '',
		caller_session  TEXT NOT NULL DEFAULT '',
		worker_sessions TEXT NOT NULL DEFAULT '',
		host            TEXT NOT NULL DEFAULT '',
		started_at      TEXT NOT NULL,
		ended_at        TEXT NOT NULL DEFAULT '',
		exit_code       INTEGER,
		end_reason      TEXT NOT NULL DEFAULT '',
		usage_input     INTEGER NOT NULL DEFAULT 0,
		usage_cached    INTEGER NOT NULL DEFAULT 0,
		usage_output    INTEGER NOT NULL DEFAULT 0,
		resumed_from    TEXT NOT NULL DEFAULT ''
	)`); err != nil {
		return fmt.Errorf("create delegation_runs: %w", err)
	}
	if _, err := db.Exec(`CREATE INDEX IF NOT EXISTS idx_delegation_runs_work_item ON delegation_runs(work_item_id, started_at)`); err != nil {
		return fmt.Errorf("create delegation_runs index: %w", err)
	}
	return nil
}
