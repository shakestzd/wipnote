package main

import (
	"database/sql"
	"fmt"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/delegationledger"
)

// reindexDelegationRuns projects the canonical delegations ledger
// (.wipnote/delegations-ledger.html) into the derived delegation_runs table.
// Returns (rows written, errors).
//
// A FULL replace every pass, like reindexClaimEpisodes: Close and Enrich
// rewrite rows in place, so insert-if-absent would keep a stale open row, and
// ledger writes are committed through the commit queue, which a git-diff
// driven pass would miss. The ledger is small (one row per run).
func reindexDelegationRuns(database *sql.DB, wipnoteDir string, verbose bool) (int, int) {
	if database == nil {
		return 0, 0
	}
	records, err := delegationledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		if verbose {
			fmt.Printf("reindex delegation ledger: %v\n", err)
		}
		return 0, 1
	}
	if err := dbpkg.ReplaceDelegationRuns(database, records); err != nil {
		if verbose {
			fmt.Printf("reindex delegation ledger: %v\n", err)
		}
		return 0, 1
	}
	return len(records), 0
}
