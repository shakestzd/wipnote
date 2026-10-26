package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// repairLedgerHarnesses retags rows whose harness is an old spelling or names
// no harness (bug-5333ea49), printing every change the way end repairs are
// printed. The inferred harness is the one backfill derives from the session
// HTML's data-agent; a session with no canonical record infers nothing and its
// tag becomes unknown. Returns how many rows were (or would be) retagged.
func repairLedgerHarnesses(cmd *cobra.Command, store *sessionledger.Store, ids []string,
	candidates map[string]backfillCandidate, apply bool) int {
	retagged := 0
	for _, id := range ids {
		r, err := store.RetagHarness(id, candidates[id].enrich.Harness, !apply)
		if err != nil {
			if err != sessionledger.ErrNoRow {
				fmt.Fprintf(cmd.ErrOrStderr(), "warn: %s: %v\n", id, err)
			}
			continue
		}
		if !r.Applied && !r.WouldApply {
			continue
		}
		retagged++
		fmt.Fprintf(cmd.OutOrStdout(), "%s\n  harness %s -> %s\n  %s\n",
			id, harnessLabel(r.Old), harnessLabel(r.New), r.Reason)
	}
	return retagged
}

// harnessLabel renders a harness tag for operator-facing output, naming the
// unknown case explicitly rather than printing an empty string.
func harnessLabel(h string) string {
	if h == "" {
		return "(unknown)"
	}
	return h
}
