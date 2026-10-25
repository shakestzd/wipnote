package main

import (
	"context"
	"database/sql"
	"io"
	"os"

	"github.com/shakestzd/wipnote/internal/gate"
)

type gateRunResult = gate.RunResult

func runSessionGate(projectRoot, sessionID, workItemID, source, phase string, stdout, stderr io.Writer) (*gateRunResult, error) {
	return gate.RunSession(gate.RunOptions{
		ProjectRoot: projectRoot,
		SessionID:   sessionID,
		WorkItemID:  workItemID,
		Source:      source,
		Phase:       phase,
		Harness:     currentHarness(),
		Stdout:      stdout,
		Stderr:      stderr,
	})
}

func resolveGateWorkItem(projectRoot, sessionID, agentID, flagValue string, w io.Writer) string {
	return gate.ResolveWorkItem(projectRoot, sessionID, agentID, flagValue, w)
}

func reportGuardProfileDrift(database *sql.DB, projectRoot, sessionID string, w io.Writer) {
	gate.ReportGuardProfileDrift(database, projectRoot, sessionID, w)
}

// validateCompletionGateRecord takes no *sql.DB: the completion gate resolves its
// evidence from the canonical ledger, so its verdict is independent of index
// state (feat-0e5ca43e).
func validateCompletionGateRecord(projectRoot, sessionID, workItemID string) error {
	return gate.ValidateCompletionRecord(projectRoot, sessionID, workItemID, currentHarness(), os.Stdout, os.Stderr)
}

func gateSignalContext() (context.Context, context.CancelFunc) {
	return gate.SignalContext()
}
