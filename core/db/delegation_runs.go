package db

import (
	"database/sql"
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/core/delegationledger"
)

// delegation_runs is the derived read index of .wipnote/delegations-ledger.html
// (feat-dd8b0cde). The HTML is canonical; reindex replaces this table from it
// on every pass, because Close and Enrich change rows in place.

const delegationRunColumns = `run_id, mode, harness, name, label, work_item_id, link,
	caller_session, worker_sessions, host, started_at, ended_at, exit_code,
	end_reason, usage_input, usage_cached, usage_output, resumed_from`

// ReplaceDelegationRuns empties the index and inserts recs, in one transaction
// so a reader never sees a half-built table.
func ReplaceDelegationRuns(db *sql.DB, recs []delegationledger.Record) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("replace delegation runs: begin: %w", err)
	}
	defer tx.Rollback() //nolint:errcheck // no-op after Commit

	if _, err := tx.Exec(`DELETE FROM delegation_runs`); err != nil {
		return fmt.Errorf("purge delegation runs: %w", err)
	}
	stmt, err := tx.Prepare(`INSERT INTO delegation_runs (` + delegationRunColumns + `)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return fmt.Errorf("prepare delegation run insert: %w", err)
	}
	defer stmt.Close()
	for _, r := range recs {
		if _, err := stmt.Exec(delegationRunArgs(r)...); err != nil {
			return fmt.Errorf("insert delegation run %s: %w", r.ID, err)
		}
	}
	return tx.Commit()
}

func delegationRunArgs(r delegationledger.Record) []any {
	ended := ""
	if !r.Ended.IsZero() {
		ended = delegationledger.FormatTime(r.Ended)
	}
	var exit any
	if r.Exit != nil {
		exit = *r.Exit
	}
	return []any{
		r.ID, r.Mode, r.Harness, r.Name, r.Label, r.WorkItem, r.Link,
		r.CallerSession, strings.Join(r.WorkerSessions, " "), r.Host,
		delegationledger.FormatTime(r.Started), ended, exit, r.EndReason,
		r.UsageInput, r.UsageCached, r.UsageOutput, r.ResumedFrom,
	}
}

// ListDelegationRuns returns every indexed run ordered by start, then id —
// the ledger's own order.
func ListDelegationRuns(db *sql.DB) ([]delegationledger.Record, error) {
	rows, err := db.Query(`SELECT ` + delegationRunColumns + ` FROM delegation_runs ORDER BY started_at, run_id`)
	if err != nil {
		return nil, fmt.Errorf("list delegation runs: %w", err)
	}
	defer rows.Close()
	var out []delegationledger.Record
	for rows.Next() {
		r, err := scanDelegationRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func scanDelegationRun(rows *sql.Rows) (delegationledger.Record, error) {
	var r delegationledger.Record
	var workers, started, ended string
	var exit sql.NullInt64
	if err := rows.Scan(&r.ID, &r.Mode, &r.Harness, &r.Name, &r.Label, &r.WorkItem, &r.Link,
		&r.CallerSession, &workers, &r.Host, &started, &ended, &exit, &r.EndReason,
		&r.UsageInput, &r.UsageCached, &r.UsageOutput, &r.ResumedFrom); err != nil {
		return r, fmt.Errorf("scan delegation run: %w", err)
	}
	var err error
	if r.Started, err = delegationledger.ParseTime(started); err != nil {
		return r, fmt.Errorf("delegation run %s started: %w", r.ID, err)
	}
	if r.Ended, err = delegationledger.ParseTime(ended); err != nil {
		return r, fmt.Errorf("delegation run %s ended: %w", r.ID, err)
	}
	if exit.Valid {
		n := int(exit.Int64)
		r.Exit = &n
	}
	if f := strings.Fields(workers); len(f) > 0 {
		r.WorkerSessions = f
	}
	return r, nil
}
