package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

// linkRunner is the gh runner behind the work-item PR lookups in
// `check --gate` and `<type> show`. Neither command runs from a hook (hooks
// are `wipnote hook <handler>`), so gh is never called on a hook path.
// TestMain swaps in a runner with no gh.
var linkRunner gitwatch.Runner = gitwatch.ExecRunner{}

// writeGatePRWarning prints the one WARN line when workItemID's linked PR
// has failed or cancelled checks, a conflict or unresolved threads. No
// work item, no PR, no gh or any gh failure prints nothing; the lookup is
// capped at gitwatch.LinkTimeout. It never fails the gate.
func writeGatePRWarning(w io.Writer, r gitwatch.Runner, workItemID string) {
	if !gitwatch.WorkItemPattern.MatchString(workItemID) {
		return
	}
	if line := gitwatch.GateWarning(context.Background(), r, workItemID, gitwatch.LinkTimeout); line != "" {
		fmt.Fprintln(w, line)
	}
}

// printLinkedPRLine prints "  PR #N <state> · CI <counts>" under a work
// item's show header when a linked PR is found; any failure prints nothing.
func printLinkedPRLine(w io.Writer, r gitwatch.Runner, id string) {
	if !gitwatch.WorkItemPattern.MatchString(id) {
		return
	}
	if line := gitwatch.ShowLine(context.Background(), r, id, gitwatch.LinkTimeout); line != "" {
		fmt.Fprintf(w, "  %s\n", line)
	}
}

// runCIStatusWorkItem resolves id to its linked PR and writes the usual
// snapshot for it.
func runCIStatusWorkItem(ctx context.Context, w io.Writer, r gitwatch.Runner, id string, jsonOut bool) error {
	if ctx == nil {
		ctx = context.Background()
	}
	snap, err := gitwatch.BuildSnapshotForWorkItem(ctx, r, id, time.Now)
	if err != nil {
		return err
	}
	if jsonOut {
		return writeJSON(w, snap)
	}
	writeSnapshotText(w, snap)
	return nil
}
