package main

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/models"
)

// runLineageFor dispatches the lineage routes that read the canonical store
// rather than the SQLite index: dlg- run ids and work-item/session roots.
func runLineageFor(w io.Writer, wipnoteDir, arg string, opts lineageOpts) error {
	if detectLineageKind(arg) == kindDelegation {
		return runLineageDelegation(w, wipnoteDir, arg, opts)
	}
	return runLineageProjection(w, wipnoteDir, arg, opts)
}

// runLineageDelegation renders `wipnote lineage dlg-<id>`: the run's caller
// session, work item and worker sessions, with the workers' claim episodes.
// A dlg- id is no graph node, so forward and backward are empty.
func runLineageDelegation(w io.Writer, wipnoteDir, id string, opts lineageOpts) error {
	rec, ok, err := delegationledger.NewStore(wipnoteDir).Get(id)
	if err != nil {
		return fmt.Errorf("read delegations ledger: %w", err)
	}
	if !ok {
		return fmt.Errorf("no delegation run %s in the delegations ledger", id)
	}
	link := delegationLinkTag
	if rec.WorkItem == "" {
		link = rec.Link
	}
	row := toLineageDelegation(rec, link)
	if eps, readErr := claimledger.NewStore(wipnoteDir).ReadAll(); readErr == nil {
		row.Claims = claimsFor(rec, eps)
	}
	if opts.jsonOut {
		return writeJSON(w, lineageJSON{Root: id, Kind: kindDelegation.String(),
			Forward: []lineageNode{}, Backward: []lineageNode{}, Delegations: []lineageDelegation{row}})
	}
	sep := strings.Repeat("─", 60)
	fmt.Fprintf(w, "%s\n  Lineage: %s  [%s]\n%s\n", sep, id, kindDelegation, sep)
	printDelegationHead(w, "  ", row)
	fmt.Fprintf(w, "  caller session: %s\n", orNone(row.CallerSession))
	fmt.Fprintf(w, "  work item: %s\n", orNone(row.WorkItem))
	fmt.Fprintf(w, "  worker sessions: %s\n", orNone(strings.Join(row.WorkerSessions, ", ")))
	printDelegationClaims(w, "  ", row.Claims)
	return nil
}

// implementedInNodes gives a work item's own implemented_in edges in the
// shape directSessions reads, so `feature show` derives sid links without a
// graph walk.
func implementedInNodes(n *models.Node) []lineageNode {
	var out []lineageNode
	for _, e := range n.Edges[string(models.RelImplementedIn)] {
		out = append(out, lineageNode{ID: e.TargetID, EdgeType: string(models.RelImplementedIn), Parent: n.ID})
	}
	return out
}

func orNone(s string) string {
	if s == "" {
		return "(none)"
	}
	return s
}

// printLineageDelegations renders the delegations section below the tree.
func printLineageDelegations(w io.Writer, v *lineageDelegationsView) {
	if v == nil {
		return
	}
	if v.Err != "" {
		fmt.Fprintf(w, "\n  delegations: unreadable (%s)\n", v.Err)
		return
	}
	fmt.Fprintf(w, "\n  delegations (%d):\n", len(v.Rows))
	for _, d := range v.Rows {
		printDelegationHead(w, "    ", d)
		if d.CallerSession != "" {
			fmt.Fprintf(w, "      caller: %s\n", d.CallerSession)
		}
		if len(d.WorkerSessions) > 0 {
			fmt.Fprintf(w, "      workers: %s\n", strings.Join(d.WorkerSessions, ", "))
		}
		printDelegationClaims(w, "      ", d.Claims)
	}
}

// printDelegationHead prints e.g. "dlg-…  w1 [interactive, claude, ended]  (tagged)".
func printDelegationHead(w io.Writer, indent string, d lineageDelegation) {
	name := ""
	if d.Name != "" {
		name = " " + d.Name
	}
	link := "tagged"
	switch d.Link {
	case delegationLinkSID:
		link = "derived: worker session"
	case "":
		link = "untagged"
	}
	fmt.Fprintf(w, "%s%s%s  [%s, %s, %s]  (%s)\n", indent, d.ID, name, d.Mode, d.Harness, d.State, link)
}

// printDelegationClaims prints each worker claim episode: start, end, outcome.
func printDelegationClaims(w io.Writer, indent string, claims []lineageClaim) {
	for _, c := range claims {
		end := "open"
		if c.EndedAt != "" {
			end = shortClaimTime(c.EndedAt) + " " + c.Outcome
		}
		line := fmt.Sprintf("%sclaim %s  %s  %s → %s  (%s)", indent, c.Episode, c.WorkItem,
			shortClaimTime(c.StartedAt), end, c.Session)
		if c.CoClaim != "" {
			line += "  co-claim with caller (" + c.CoClaim + ")"
		}
		fmt.Fprintln(w, line)
	}
}

// shortClaimTime renders an RFC3339 time in lineageRunsLoc (local by default).
func shortClaimTime(v string) string {
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return v
	}
	loc := lineageRunsLoc
	if loc == nil {
		loc = time.Local
	}
	return t.In(loc).Format("2006-01-02 15:04")
}
