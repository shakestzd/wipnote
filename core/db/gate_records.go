package db

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/gateledger"
)

// GateRecord is a quality-gate run as it appears in the READ INDEX.
//
// The canonical record lives in core/gateledger (.wipnote/gate-ledger.html); this
// is the derived projection reindex rebuilds from it (feat-0e5ca43e, closing
// bug-550c1cd8). Rows here are queryable but never authoritative — the completion
// gate reads the ledger, not this table.
type GateRecord struct {
	ID int64
	// RecordID is the canonical ledger record id (gr-…) this row projects, or ""
	// for a legacy row written before the ledger existed. It is the key the
	// reindex projection is idempotent on.
	RecordID          string
	SessionID         string
	WorkItemID        string
	Harness           string
	ProjectType       string
	GateCommand       string
	Status            string
	CheckedAt         time.Time
	Signature         string
	AllowlistHitsJSON string
	AllowlistHitCount int
	Source            string
	OutputSummary     string
	// ProfileSignature is the canonical guardprofile.Signature of the APPROVED
	// guard profile that supplied this gate's commands, or "" when the gate ran
	// via manifest autodetection. It is provenance, NOT part of the
	// record-integrity signablePayload/Signature MAC.
	ProfileSignature string
	// GuardsRunJSON is a JSON array of the guard names that ran. "[]" when none
	// (autodetection or no-op).
	GuardsRunJSON string
}

// signatureInput builds the canonical signable field set.
//
// The algorithm deliberately lives in core/gateledger and is only reached from
// here: the SAME signature is computed over the canonical ledger record and
// verified against this projection, so two independent implementations would be
// a drift bug that only surfaces as a completion mysteriously refused.
func (gr *GateRecord) signatureInput() gateledger.SignatureInput {
	return gateledger.SignatureInput{
		SessionID:         gr.SessionID,
		WorkItemID:        gr.WorkItemID,
		Harness:           gr.Harness,
		ProjectType:       gr.ProjectType,
		GateCommand:       gr.GateCommand,
		Status:            gr.Status,
		CheckedAt:         gr.CheckedAt,
		AllowlistHitsJSON: gr.AllowlistHitsJSON,
		Source:            gr.Source,
		OutputSummary:     gr.OutputSummary,
	}
}

func (gr *GateRecord) ComputeSignature() string { return gr.signatureInput().Sum() }

func (gr *GateRecord) EnsureSignature() {
	gr.Signature = gr.ComputeSignature()
}

// InsertGateRecordIfAbsent projects one gate run into the read index and reports
// whether a row was actually written.
//
// The insert is OR IGNORE against the partial unique index on record_id, so
// projecting the same canonical record twice — a gate run that wrote this row
// directly, followed by the reindex pass replaying the same ledger row — is a
// no-op rather than a duplicate. Legacy rows (record_id "") sit outside that
// index and keep the old insert-always behaviour.
func InsertGateRecordIfAbsent(database *sql.DB, gr *GateRecord) (bool, error) {
	if database == nil {
		return false, nil
	}
	if gr == nil {
		return false, fmt.Errorf("gate record is nil")
	}
	if gr.CheckedAt.IsZero() {
		gr.CheckedAt = time.Now().UTC()
	}
	if gr.AllowlistHitsJSON == "" {
		gr.AllowlistHitsJSON = "[]"
	}
	if gr.GuardsRunJSON == "" {
		gr.GuardsRunJSON = "[]"
	}
	if gr.Signature == "" {
		gr.EnsureSignature()
	}
	res, err := database.Exec(`
		INSERT OR IGNORE INTO gate_records (
			record_id, session_id, work_item_id, harness, project_type, gate_command,
			status, checked_at, signature, allowlist_hits_json,
			allowlist_hit_count, source, output_summary,
			profile_signature, guards_run
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		gr.RecordID, gr.SessionID, nullStr(gr.WorkItemID), nullStr(gr.Harness), gr.ProjectType,
		gr.GateCommand, gr.Status, gr.CheckedAt.UTC().Format(time.RFC3339Nano),
		gr.Signature, gr.AllowlistHitsJSON, gr.AllowlistHitCount, gr.Source,
		nullStr(gr.OutputSummary), gr.ProfileSignature, gr.GuardsRunJSON,
	)
	if err != nil {
		return false, fmt.Errorf("insert gate record: %w", err)
	}
	// RowsAffected is the ONLY reliable inserted/ignored signal here: an ignored
	// INSERT OR IGNORE still reports the previous row's LastInsertId, so the id
	// alone cannot tell the two apart.
	affected, err := res.RowsAffected()
	if err != nil || affected == 0 {
		return false, nil
	}
	if id, err := res.LastInsertId(); err == nil {
		gr.ID = id
	}
	return true, nil
}

// GateRecordFromLedger projects a canonical ledger record into its index row.
func GateRecordFromLedger(r gateledger.Record) *GateRecord {
	return &GateRecord{
		RecordID:          r.ID,
		SessionID:         r.SessionID,
		WorkItemID:        r.WorkItemID,
		Harness:           r.Harness,
		ProjectType:       r.ProjectType,
		GateCommand:       r.GateCommand,
		Status:            r.Status,
		CheckedAt:         r.CheckedAt,
		Signature:         r.Signature,
		AllowlistHitsJSON: r.AllowlistHitsJSON,
		AllowlistHitCount: r.AllowlistHitCount,
		Source:            r.Source,
		OutputSummary:     r.OutputSummary,
		ProfileSignature:  r.ProfileSignature,
		GuardsRunJSON:     r.GuardsRunJSON,
	}
}

func LatestGateRecordForSession(database *sql.DB, sessionID string) (*GateRecord, error) {
	if database == nil || strings.TrimSpace(sessionID) == "" {
		return nil, nil
	}
	row := database.QueryRow(`
		SELECT id, COALESCE(record_id,''), session_id, COALESCE(work_item_id,''), COALESCE(harness,''),
		       COALESCE(project_type,''), COALESCE(gate_command,''), COALESCE(status,''),
		       checked_at, COALESCE(signature,''), COALESCE(allowlist_hits_json,'[]'),
		       COALESCE(allowlist_hit_count,0), COALESCE(source,''), COALESCE(output_summary,''),
		       COALESCE(profile_signature,''), COALESCE(guards_run,'[]')
		FROM gate_records
		WHERE session_id = ?
		ORDER BY checked_at DESC, id DESC
		LIMIT 1`, sessionID)
	return scanGateRecord(row)
}

// GateRecordCountsForWorkItem returns the number of pass and fail gate runs
// recorded for workItemID, across every session that ever ran one. It is the
// outcome-evidence source `compliance auto` (feat-f9118b9c) joins against the
// spec/diff comparison, so a compliance verdict can reflect whether the
// item's own quality gates ever failed, not only whether the diff matches
// the spec.
//
// The two counts are complete by construction — every gate_records row for
// this item is counted, not sampled — so (0, 0) is a genuine "no gate runs
// recorded" rather than a coverage gap. Callers must still not read (0, 0)
// as "passed": it means no evidence, and feat-f9118b9c requires that
// absence never be promoted to a passing signal.
func GateRecordCountsForWorkItem(database *sql.DB, workItemID string) (pass, fail int, err error) {
	if database == nil || strings.TrimSpace(workItemID) == "" {
		return 0, 0, nil
	}
	rows, err := database.Query(
		`SELECT status, COUNT(*) FROM gate_records WHERE work_item_id = ? GROUP BY status`,
		workItemID)
	if err != nil {
		return 0, 0, fmt.Errorf("count gate records for %s: %w", workItemID, err)
	}
	defer rows.Close()

	for rows.Next() {
		var status string
		var count int
		if err := rows.Scan(&status, &count); err != nil {
			return 0, 0, fmt.Errorf("scan gate record count: %w", err)
		}
		switch status {
		case "pass":
			pass = count
		case "fail":
			fail = count
		}
	}
	return pass, fail, rows.Err()
}

func scanGateRecord(scanner interface{ Scan(dest ...any) error }) (*GateRecord, error) {
	var gr GateRecord
	var checkedAt string
	err := scanner.Scan(
		&gr.ID, &gr.RecordID, &gr.SessionID, &gr.WorkItemID, &gr.Harness, &gr.ProjectType,
		&gr.GateCommand, &gr.Status, &checkedAt, &gr.Signature,
		&gr.AllowlistHitsJSON, &gr.AllowlistHitCount, &gr.Source, &gr.OutputSummary,
		&gr.ProfileSignature, &gr.GuardsRunJSON,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	gr.CheckedAt, _ = time.Parse(time.RFC3339Nano, checkedAt)
	if gr.CheckedAt.IsZero() {
		gr.CheckedAt, _ = time.Parse(time.RFC3339, checkedAt)
	}
	return &gr, nil
}
