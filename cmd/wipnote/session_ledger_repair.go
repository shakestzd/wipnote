package main

import (
	"fmt"
	"sort"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// sessionLedgerRepairCmd returns `wipnote session ledger repair`.
//
// A canonical artifact needs a way to correct a row whose recorded value is
// known wrong; without one, the only remedies are hand-editing the managed
// store (which the .wipnote guard refuses, correctly) or living with the bad
// data. This is that way.
//
// It is DRY-RUN BY DEFAULT, inverting `backfill`'s shape on purpose. Backfill
// only adds rows and fills gaps, so doing it is safe and --dry-run is the
// opt-in. Repair OVERWRITES recorded values, so seeing it is the default and
// --apply is the opt-in.
func sessionLedgerRepairCmd() *cobra.Command {
	var apply bool
	cmd := &cobra.Command{
		Use:   "repair",
		Short: "Re-derive ledger ends and harness tags from the best available evidence",
		Long: `Correct session rows whose end time came from a weaker source than the
evidence now available.

Every recorded end carries its provenance, and repair replaces a value only
when the incoming source outranks the recorded one, least to most trusted:

  archive-mtime    when retention created the tarball — not an end at all,
                   only a loose upper bound; used when events are unreadable
  last-activity    the final timestamp in the session's events.ndjson, or that
                   file's mtime at archive time
  session-record   data-ended-at from the canonical session HTML
  live-close       stamped by the SessionEnd hook as the session ended

A row written before provenance existed is unattributed and ranks below all of
these, so it is re-derived from the best source available. A live-close end is
never moved.

There is deliberately NO value-based rule. "An end later than the last recorded
event is wrong" looks general and is false: SessionEnd legitimately fires after
the last tool event. Provenance is the only sound signal, so it is the only one
used.

Repair also fixes harness tags written before the tag was a closed set:
"claude-code" (the old spelling) becomes "claude", and "human" or any other
value that names no harness becomes the harness inferred from the session's
canonical record (its data-agent), or unknown when none can be inferred. A row
that already names a real harness is never retagged, and no row is dropped.

Prints what would change and exits without writing unless --apply is given.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runSessionLedgerRepair(cmd, wipnoteDir, apply)
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "Write the corrections (default: report only)")
	return cmd
}

// runSessionLedgerRepair re-derives ends and harness tags from the best
// available evidence and applies only the corrections that evidence justifies.
func runSessionLedgerRepair(cmd *cobra.Command, wipnoteDir string, apply bool) error {
	store := sessionledger.NewStore(wipnoteDir)
	records, err := store.ReadAll()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		fmt.Fprintln(cmd.OutOrStdout(), "sessions ledger is empty — nothing to repair")
		return nil
	}

	// The candidates come from the SAME collectors backfill uses, so repair and
	// backfill can never disagree about what a source says.
	candidates := collectBackfillCandidates(wipnoteDir)

	ids := make([]string, 0, len(records))
	for _, r := range records {
		ids = append(ids, r.SessionID)
	}
	sort.Strings(ids)

	ends := repairLedgerEnds(cmd, store, ids, candidates, apply)
	retagged := repairLedgerHarnesses(cmd, store, ids, candidates, apply)
	reportLedgerRepair(cmd, ends, retagged, apply)
	return nil
}

// endRepairCounts tallies the end corrections one repair pass made.
type endRepairCounts struct {
	corrected, attributed, refused int
}

// repairLedgerEnds corrects each row's end where provenance justifies it,
// printing every value change.
func repairLedgerEnds(cmd *cobra.Command, store *sessionledger.Store, ids []string,
	candidates map[string]backfillCandidate, apply bool) endRepairCounts {
	var n endRepairCounts
	for _, id := range ids {
		cand, found := candidates[id]
		if !found {
			continue
		}
		c, corrErr := store.Correct(id, cand.enrich.EndedAt, cand.enrich.EndSource, !apply)
		if corrErr != nil {
			if corrErr != sessionledger.ErrNoRow {
				fmt.Fprintf(cmd.ErrOrStderr(), "warn: %s: %v\n", id, corrErr)
			}
			continue
		}
		// Trust the decision the store made rather than re-deriving it here: a
		// second copy of the rule is a copy that can disagree with the one that
		// actually governs writes.
		switch {
		case !c.Applied && !c.WouldApply:
			if c.Changed() {
				n.refused++
			}
		case c.AttributionOnly():
			n.attributed++
		default:
			n.corrected++
			fmt.Fprintf(cmd.OutOrStdout(), "%s\n  end %s -> %s\n  %s\n",
				id, formatLedgerTime(c.OldEnd), formatLedgerTime(c.NewEnd), c.Reason)
		}
	}
	return n
}

// reportLedgerRepair prints the summary of one repair pass.
func reportLedgerRepair(cmd *cobra.Command, n endRepairCounts, retagged int, apply bool) {
	out := cmd.OutOrStdout()
	verb := "would be"
	if apply {
		verb = "were"
	}
	if n.corrected == 0 && n.attributed == 0 {
		fmt.Fprintln(out, "every recorded end is already backed by the best available source")
	}
	if n.corrected > 0 {
		fmt.Fprintf(out, "\n%d session end(s) %s corrected\n", n.corrected, verb)
	}
	// Attribution-only rows are reported as a count, not a list: nothing about
	// the session's history changes, only the row's ability to say where its end
	// came from, which is what makes the NEXT repair sound.
	if n.attributed > 0 {
		fmt.Fprintf(out, "%d row(s) %s left unchanged but gained provenance\n", n.attributed, verb)
	}
	if n.refused > 0 {
		fmt.Fprintf(out, "%d row(s) left alone — their recorded source is already at least as trustworthy\n", n.refused)
	}
	if retagged > 0 {
		fmt.Fprintf(out, "%d session harness tag(s) %s corrected\n", retagged, verb)
	}
	if !apply && (n.corrected > 0 || n.attributed > 0 || retagged > 0) {
		fmt.Fprintln(out, "\nRe-run with --apply to write.")
	}
}

func formatLedgerTime(t time.Time) string {
	if t.IsZero() {
		return "(none)"
	}
	return t.UTC().Format(time.RFC3339)
}
