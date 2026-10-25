package db

import (
	"database/sql"
	"fmt"
)

// currentSchemaVersion is the highest migration step version. Bump by 1 when
// adding a new migration step to the migrations slice below. PRAGMA
// user_version on a fully-migrated database is equal to currentSchemaVersion.
//
// Versioning replaces the unconditional DDL re-execution that previously ran on
// every Open. The fast warm-open path (when user_version == currentSchemaVersion)
// executes ZERO CREATE / ALTER / DROP / trigger / normalisation statements —
// avoiding the write-lock acquisition that caused SQLITE_BUSY in short-lived
// hook processes.
const currentSchemaVersion = 24

// copySwapStepName is the name of the agent_events copy-and-swap migration
// step. Exposed via CopySwapStepName() so tests can assert it runs at most
// once per database.
const copySwapStepName = "004_agent_events_copy_swap"

// migrationStep represents one ordered, idempotent schema migration. apply
// MUST be safe to call on a database whose live schema is already at or beyond
// the step's intended state — every step is guarded by user_version, but
// idempotent apply functions are belt-and-suspenders insurance against partial
// rollbacks.
type migrationStep struct {
	version int    // user_version after this step applies
	name    string // stable identifier (recorded via the migration observer)
	apply   func(*sql.DB) error
}

// migrations is the ordered registry of every schema migration. Steps are
// applied in slice order. The runner skips steps whose version is <= the
// database's current user_version.
//
// Adding a new step:
//  1. Append a migrationStep with version = currentSchemaVersion + 1.
//  2. Bump currentSchemaVersion to match.
//  3. Make the apply function idempotent — it may run on legacy DBs whose
//     live schema already reflects part of the change.
var migrations = []migrationStep{
	{
		version: 1,
		name:    "001_initial_schema",
		apply:   stepCreateBaseTables,
	},
	{
		version: 2,
		name:    "002_create_indexes",
		apply:   stepCreateIndexes,
	},
	{
		version: 3,
		name:    "003_post_initial_columns_and_tables",
		apply:   stepPostInitialColumnsAndTables,
	},
	{
		version: 4,
		name:    copySwapStepName,
		apply:   stepAgentEventsCopySwap,
	},
	{
		version: 5,
		name:    "005_normalize_plan_feedback",
		apply:   stepNormalizePlanFeedback,
	},
	{
		version: 6,
		name:    "006_gate_records",
		apply:   stepGateRecords,
	},
	{
		version: 7,
		name:    "007_session_family_id",
		apply:   stepSessionFamilyID,
	},
	{
		version: 8,
		name:    "008_session_files",
		apply:   stepSessionFiles,
	},
	{
		version: 9,
		name:    "009_feature_files_path_seen_index",
		apply:   stepFeatureFilesPathSeenIndex,
	},
	{
		version: 10,
		name:    "010_repair_trigger_increment_total_events",
		apply:   stepRepairTriggerIncrementTotalEvents,
	},
	{
		version: 11,
		name:    "011_backfill_total_events",
		apply:   stepBackfillTotalEvents,
	},
	{
		version: 12,
		name:    "012_gate_records_profile_signature",
		apply:   stepGateRecordsProfileSignature,
	},
	{
		version: 13,
		name:    "013_arch_cards",
		apply:   stepArchCards,
	},
	{
		version: 14,
		name:    "014_session_exec_context",
		apply:   stepSessionExecContext,
	},
	{
		version: 15,
		name:    "015_session_handoff_fields",
		apply:   stepSessionHandoffFields,
	},
	{
		version: 16,
		name:    "016_plan_feedback_annotation_state",
		apply:   stepPlanFeedbackAnnotationState,
	},
	{
		version: 17,
		name:    "017_recaps_table",
		apply:   stepRecapsTable,
	},
	{
		version: 18,
		name:    "018_git_commits_composite_key",
		apply:   stepGitCommitsCompositeKey,
	},
	{
		version: 19,
		name:    "019_otel_signals_attribution_columns",
		apply:   stepOtelSignalsAttributionColumns,
	},
	{
		version: 20,
		name:    "020_gate_records_record_id",
		apply:   stepGateRecordsRecordID,
	},
	{
		version: 21,
		name:    "021_claim_episodes_table",
		apply:   stepClaimEpisodesTable,
	},
	{
		version: 22,
		name:    "022_otel_span_id_index_not_unique",
		apply:   stepOtelSpanIDIndexNotUnique,
	},
	{
		version: 23,
		name:    "023_otel_session_kind_index",
		apply:   stepOtelSessionKindIndex,
	},
	{
		version: 24,
		name:    "024_delegation_runs_table",
		apply:   stepDelegationRunsTable,
	},
}

// readUserVersion returns the database's current PRAGMA user_version. A fresh
// database reports 0. Errors propagate to the caller (e.g. for fail-fast Open).
func readUserVersion(db *sql.DB) (int, error) {
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		return 0, fmt.Errorf("read user_version: %w", err)
	}
	return v, nil
}

// writeUserVersion sets PRAGMA user_version. SQLite does not support parameter
// binding for PRAGMA values, so the literal is rendered into the statement.
func writeUserVersion(db *sql.DB, v int) error {
	if _, err := db.Exec(fmt.Sprintf("PRAGMA user_version = %d", v)); err != nil {
		return fmt.Errorf("write user_version=%d: %w", v, err)
	}
	return nil
}

// RunMigrations applies any pending schema migrations to an ALREADY-OPEN
// *sql.DB, without going through Open's own connection/pragma setup. It
// exists for callers that must own their own connection-opening logic (their
// own DSN pragmas, pool sizing, or pinned-connection semantics) but still
// need the same version-gated migration guarantee every db.Open caller gets
// for free — see receiver.NewWriter (bug-286ce8f7), which used to open its
// DB handle via a raw sql.Open and never ran migrations at all, silently
// depending on some OTHER writer having already migrated the same file
// first. Safe to call on every open: version-gated and idempotent, exactly
// like the internal runMigrations Open wraps.
func RunMigrations(db *sql.DB) error {
	return RetryOnBusy(DefaultBusyBackoff, func() error {
		return runMigrations(db)
	})
}

// runMigrations applies every migration step whose version is greater than the
// database's current user_version. Steps are applied in slice order and the
// user_version PRAGMA is bumped after each successful step so a mid-chain
// failure leaves the database at the last completed step (forward progress is
// preserved across process restarts).
//
// Each step is responsible for its own transactional discipline. Schema-altering
// steps that combine multiple DDL operations wrap them in BEGIN/COMMIT
// internally; trivial single-statement steps may run outside a transaction
// because SQLite's autocommit already provides atomicity for one DDL.
func runMigrations(db *sql.DB) error {
	current, err := readUserVersion(db)
	if err != nil {
		return err
	}
	if current >= currentSchemaVersion {
		return nil
	}

	for _, step := range migrations {
		if step.version <= current {
			continue
		}
		notifyMigration(step.name)
		if err := step.apply(db); err != nil {
			return fmt.Errorf("migration %s (v%d): %w", step.name, step.version, err)
		}
		if err := writeUserVersion(db, step.version); err != nil {
			return fmt.Errorf("after %s: %w", step.name, err)
		}
	}
	return nil
}

// stepCreateBaseTables creates every wipnote table (CreateAllTables) and the
// OTel ingestion tables (CreateOtelTables). All statements are idempotent
// (CREATE TABLE IF NOT EXISTS), so the step is safe to run against a legacy DB
// whose tables already exist.
func stepCreateBaseTables(db *sql.DB) error {
	if err := CreateAllTables(db); err != nil {
		return fmt.Errorf("create base tables: %w", err)
	}
	if err := CreateOtelTables(db); err != nil {
		return fmt.Errorf("create otel tables: %w", err)
	}
	return nil
}

// stepCreateIndexes installs all performance indexes. Both index sets use
// CREATE INDEX IF NOT EXISTS, so the step is idempotent.
func stepCreateIndexes(db *sql.DB) error {
	if err := CreateAllIndexes(db); err != nil {
		return fmt.Errorf("create base indexes: %w", err)
	}
	if err := CreateOtelIndexes(db); err != nil {
		return fmt.Errorf("create otel indexes: %w", err)
	}
	return nil
}
