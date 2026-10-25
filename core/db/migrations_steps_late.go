package db

import (
	"database/sql"
	"fmt"
)

// stepGateRecordsProfileSignature adds the guard-profile provenance columns to
// gate_records: profile_signature (the canonical guardprofile.Signature of the
// approved profile that supplied the guards, empty when autodetection was used)
// and guards_run (a JSON array of the guard names that ran). Both are NEW
// columns distinct from the existing record-integrity `signature` MAC column.
// Idempotent: ALTER TABLE ADD COLUMN is swallowed by isDuplicateColumnError on
// re-run, and a fresh DB created by CreateAllTables already has both columns.
func stepGateRecordsProfileSignature(db *sql.DB) error {
	for _, stmt := range []string{
		`ALTER TABLE gate_records ADD COLUMN profile_signature TEXT NOT NULL DEFAULT ''`,
		`ALTER TABLE gate_records ADD COLUMN guards_run TEXT NOT NULL DEFAULT '[]'`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			if !isDuplicateColumnError(err) {
				return fmt.Errorf("add gate_records column (%s): %w", stmt, err)
			}
		}
	}
	return nil
}

// stepGateRecordsRecordID adds gate_records.record_id — the id of the canonical
// .wipnote/gate-ledger.html row this index row projects (feat-0e5ca43e).
//
// The index is now derived: reindex replays the ledger through INSERT OR IGNORE
// keyed on record_id, so a purged cache rebuilds and a warm cache is a no-op.
// The unique index is PARTIAL because existing rows predate the ledger and all
// carry an empty record_id — constraining them would fail the migration on any
// DB with more than one historical gate run. They keep their rows and are given
// canonical ids by the backfill pass, which stamps record_id as it writes each.
//
// Idempotent: ALTER TABLE ADD COLUMN is swallowed by isDuplicateColumnError on
// re-run, and a fresh DB created by CreateAllTables already has the column.
//
// A MISSING gate_records table is not an error. stepCreateBaseTables is version
// 1, so it never re-runs on a database seeded at a later user_version, and such a
// database can legitimately reach this step with the table absent. There is
// nothing to alter and nothing to lose: CreateAllTables already declares
// record_id, so whenever the table is finally created it arrives with the column.
func stepGateRecordsRecordID(db *sql.DB) error {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name='gate_records'`,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return fmt.Errorf("check gate_records existence: %w", err)
	}

	if _, err := db.Exec(`ALTER TABLE gate_records ADD COLUMN record_id TEXT NOT NULL DEFAULT ''`); err != nil {
		if !isDuplicateColumnError(err) {
			return fmt.Errorf("add gate_records.record_id: %w", err)
		}
	}
	if _, err := db.Exec(
		`CREATE UNIQUE INDEX IF NOT EXISTS idx_gate_records_record_id ON gate_records(record_id) WHERE record_id != ''`,
	); err != nil {
		return fmt.Errorf("create gate_records record_id index: %w", err)
	}
	return nil
}

// stepClaimEpisodesTable creates the claim_episodes read-index table and its
// three lookup indexes (bug-8af46da3).
//
// claim_episodes was declared in CreateAllTables (schema.go) — the function
// stepCreateBaseTables (version 1) calls — but never given a companion
// versioned step. stepCreateBaseTables only runs for a database going from
// user_version 0 to current; any database that had already migrated past
// version 1 BEFORE claim_episodes was added to CreateAllTables never runs
// that function again and therefore never gets the table, no matter which
// binary or entry point (serve, hooks, reindex) opens it afterward — the
// same defect shape as bug-286ce8f7 (see stepOtelSignalsAttributionColumns)
// and the gate_records fix in stepGateRecordsRecordID. Confirmed live: the
// table did not exist on an index already at the (then-)current schema
// version, so every claim-episode reindex pass failed with a missing-table
// error and the claim ledger was never once projected into the index it
// feeds.
//
// Idempotent: CREATE TABLE/INDEX IF NOT EXISTS are no-ops on a DB that
// already has the table (e.g. a fresh DB where stepCreateBaseTables ran
// first — both this step and CreateAllTables declare the same DDL, matching
// the existing gate_records/session_files/arch_cards/recaps pattern where a
// table lives in both the create-all path and its own dedicated step).
func stepClaimEpisodesTable(db *sql.DB) error {
	if _, err := db.Exec(`CREATE TABLE IF NOT EXISTS claim_episodes (
		episode_id      TEXT PRIMARY KEY,
		work_item_id    TEXT NOT NULL,
		session_id      TEXT NOT NULL,
		root_session_id TEXT NOT NULL DEFAULT '',
		agent_id        TEXT NOT NULL,
		started_at      TEXT NOT NULL,
		ended_at        TEXT NOT NULL DEFAULT '',
		outcome         TEXT NOT NULL DEFAULT '',
		source_file     TEXT NOT NULL DEFAULT '',
		indexed_at      DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
	)`); err != nil {
		return fmt.Errorf("create claim_episodes: %w", err)
	}
	for _, stmt := range []string{
		`CREATE INDEX IF NOT EXISTS idx_claim_episodes_agent_start ON claim_episodes(agent_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_claim_episodes_session_agent ON claim_episodes(session_id, agent_id, started_at)`,
		`CREATE INDEX IF NOT EXISTS idx_claim_episodes_work_item ON claim_episodes(work_item_id, started_at)`,
	} {
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create claim_episodes index: %w", err)
		}
	}
	return nil
}

// stepOtelSpanIDIndexNotUnique replaces the UNIQUE index on
// otel_signals(span_id) with a plain one, and flags the derived OTel index for
// a full re-ingest (bug-0fc17d53).
//
// WHY THE UNIQUE INDEX WAS WRONG: a span_id identifies a span, not a signal.
// Every OTel log record and metric correlated to a span carries that span's
// id, and Claude Code emits many of them against a single interaction span.
// The unique index therefore permitted at most one row per span, and because
// the writer inserts with INSERT OR IGNORE, every later signal sharing a
// span_id was discarded with no error, no log line, and a checkpoint that
// advanced as if the data had been stored. On the corpus this was measured
// against that was 86,758 of 155,855 signals.
//
// WHY THE INDEX DROP ALONE ONCE FIXED NOTHING: the NDJSON indexer recorded a
// byte offset per shard, and on every existing install those offsets already
// sat at end-of-file. Dropping the index stopped future loss but left the
// historical hole untouched — a fresh database looked perfect while every
// upgraded one stayed 56% empty. This step therefore used to arm a re-ingest
// marker (SetOtelReingestRequired) that indexer.EnsureReingest consumed by
// clearing the per-shard checkpoints exactly once.
//
// THAT MARKER IS GONE, and so is the problem it solved. The indexer's read
// position is now in memory and starts at zero in every process, because its
// destination is a process-local projection that starts empty (feat-fc3cc9e0).
// A full replay from byte zero is no longer a special recovery mode that has to
// be requested — it is what every process already does, unconditionally. Arming
// a marker to ask for it would leave a flag nothing reads, which is the husk
// this migration would have become.
//
// Replay remains safe and non-destructive: inserts are keyed on signal_id, so
// rows already present are skipped, and rows whose NDJSON has since been swept
// by retention are left alone rather than deleted and not re-derived.
func stepOtelSpanIDIndexNotUnique(db *sql.DB) error {
	// DROP INDEX IF EXISTS is safe on a database without the table.
	if _, err := db.Exec(`DROP INDEX IF EXISTS idx_otel_span_id_unique`); err != nil {
		return fmt.Errorf("drop idx_otel_span_id_unique: %w", err)
	}

	// CREATE INDEX is not safe on a missing table, and a partially-built
	// legacy database can reach this step without one — same guard the
	// gate_records and plan_feedback steps use.
	indexes := map[string]string{
		"otel_signals": `CREATE INDEX IF NOT EXISTS idx_otel_span_id ON otel_signals(span_id) WHERE span_id IS NOT NULL`,
		// Also declared in CreateOtelIndexes. It previously lived in the
		// create-all path, which never reaches a database past version 1, so
		// create it here too rather than assume every install has it.
		"pending_subagent_starts": `CREATE INDEX IF NOT EXISTS idx_pending_subagent_session ON pending_subagent_starts(session_id)`,
	}
	for table, stmt := range indexes {
		exists, err := tableExists(db, table)
		if err != nil {
			return err
		}
		if !exists {
			continue
		}
		if _, err := db.Exec(stmt); err != nil {
			return fmt.Errorf("create index on %s: %w", table, err)
		}
	}

	// No re-ingest marker is armed here any more — see the doc comment. The
	// index change above is the whole of this step now.
	return nil
}

// stepOtelSessionKindIndex adds the index that makes pruneMetricSignals
// selective instead of a per-session table walk (bug-129bf18d).
//
// pruneMetricSignals runs once per metric signal. Its DELETE and its
// keep-the-newest-N subquery both filter on (session_id, kind), but only
// session_id was indexed, so each call walked every row belonging to the
// session — including the non-metric rows, which are the large majority.
// Cost per metric signal therefore grew with session size, making ingest
// quadratic. That is what made the bug-0fc17d53 recovery replay expensive.
//
// The DESC columns are not decoration: they match the subquery's ORDER BY, so
// the plan becomes a covering-index search with no temporary B-tree. Measured
// on a real shard, per-call cost fell 33.8ms -> 1.7ms, and replaying 28,000
// real signals fell from 63.6s to 11.6s. Without the DESC tail the same index
// still helps but only reaches 3.4ms per call and 15.2s overall.
//
// Deliberately NOT added here: an index on (session_id, canonical) for
// tryReattributeParent's Strategy B scan. Its query plan improves exactly as
// you would predict, and end-to-end it made replay 19-22% SLOWER, because the
// scan fires for only about 1% of spans on real data (a span's parent is
// rarely an interaction span) while the index is maintained on every insert.
// Do not re-add it on the strength of an EXPLAIN alone.
func stepOtelSessionKindIndex(db *sql.DB) error {
	exists, err := tableExists(db, "otel_signals")
	if err != nil {
		return err
	}
	if !exists {
		return nil
	}
	if _, err := db.Exec(
		`CREATE INDEX IF NOT EXISTS idx_otel_session_kind_ts
		 ON otel_signals(session_id, kind, ts_micros DESC, created_at DESC, signal_id DESC)`,
	); err != nil {
		return fmt.Errorf("create idx_otel_session_kind_ts: %w", err)
	}
	return nil
}

// tableExists reports whether a table of that name is present.
func tableExists(db *sql.DB, table string) (bool, error) {
	var name string
	err := db.QueryRow(
		`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&name)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check %s existence: %w", table, err)
	}
	return true, nil
}
