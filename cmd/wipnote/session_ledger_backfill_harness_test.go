package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// writeAgentSessionHTML writes a root session record whose data-agent is agent.
func writeAgentSessionHTML(t *testing.T, wipnoteDir, id, agent string) {
	t.Helper()
	html := fmt.Sprintf(`<!DOCTYPE html><html><body>
<article id="%s" data-type="session" data-agent="%s" data-project-dir="."
         data-started-at="2026-10-26T05:00:00Z" data-ended-at="2026-10-26T06:00:00Z"
         data-is-subagent="false"></article></body></html>`, id, agent)
	if err := os.WriteFile(filepath.Join(wipnoteDir, "sessions", id+".html"), []byte(html), 0o644); err != nil {
		t.Fatalf("write session html: %v", err)
	}
}

// TestBackfillLateRowNeverTakesTheHumanAgentFallback: the session HTML's
// data-agent is an AGENT identity. A backfilled row maps it to a harness and
// records the agent fallback "human" as an unknown harness (bug-5333ea49).
func TestBackfillLateRowNeverTakesTheHumanAgentFallback(t *testing.T) {
	projectDir := backfillTestProject(t)
	wipnoteDir := filepath.Join(projectDir, ".wipnote")
	writeAgentSessionHTML(t, wipnoteDir, backfillHTMLSession, "human")
	writeAgentSessionHTML(t, wipnoteDir, backfillBothSession, "claude-code")

	runBackfill(t, wipnoteDir, false)

	store := sessionledger.NewStore(wipnoteDir)
	want := map[string]string{backfillHTMLSession: "", backfillBothSession: "claude"}
	for id, harness := range want {
		rec, ok, err := store.Get(id)
		if err != nil || !ok {
			t.Fatalf("%s: not backfilled (ok %v err %v)", id, ok, err)
		}
		if rec.Harness != harness {
			t.Errorf("%s: harness = %q, want %q", id, rec.Harness, harness)
		}
	}
}
