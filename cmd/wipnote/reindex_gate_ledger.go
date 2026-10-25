package main

import (
	"database/sql"
	"fmt"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/gateledger"
)

// reindexGateRecords projects the canonical gate ledger
// (.wipnote/gate-ledger.html) into the derived gate_records table. Returns
// (rows inserted, errors).
//
// This is a FULL pass every time, never incremental, for the same two reasons as
// reindexClaimEpisodes: the ledger is small (one row per gate run, seventy-five
// in this repo's entire history), and — decisively — the incremental path keys
// off git-changed files while ledger writes are committed asynchronously through
// the commit queue. A gate run written but not yet flushed would be invisible to
// a git-diff-driven pass.
//
// # Insert-if-absent, not purge-and-rebuild
//
// The projection is idempotent on record_id via a partial unique index, so a
// record already in the table (written by the gate run itself) replays as a
// no-op. Purging first — the shape reindexClaimEpisodes uses — would be wrong
// here: it would also delete legacy rows written before the ledger existed, which
// have no canonical source to be rebuilt from. (backfillGateLedgerFromIndex
// once moved those into the ledger; it was deleted once reindex began building
// a fresh projection, which holds no such rows.)
//
// A purged cache therefore rebuilds every canonical record, and a warm cache
// costs one parse and a run of ignored inserts.
func reindexGateRecords(database *sql.DB, wipnoteDir string, verbose bool) (int, int) {
	if database == nil {
		return 0, 0
	}
	records, err := gateledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		if verbose {
			fmt.Printf("reindex gate ledger: %v\n", err)
		}
		return 0, 1
	}

	inserted, errCount := 0, 0
	for _, r := range records {
		wrote, insertErr := dbpkg.InsertGateRecordIfAbsent(database, dbpkg.GateRecordFromLedger(r))
		if insertErr != nil {
			errCount++
			if verbose {
				fmt.Printf("reindex gate ledger: %s: %v\n", r.ID, insertErr)
			}
			continue
		}
		if wrote {
			inserted++
		}
	}
	return inserted, errCount
}
