package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/sessionledger"
)

// repairRealTagSession is a fourth root session whose row already carries a
// real harness that disagrees with its session HTML.
const repairRealTagSession = "ddddeeee-ffff-0000-1111-222233335555"

// seedHarnessRow writes a closed (live-close) row tagged with harness. Enrich
// refuses the agent fallback "human", which is exactly the value old ledgers
// still carry, so a "human" tag is written under a placeholder and swapped in
// on disk — in a temp project only.
func seedHarnessRow(t *testing.T, store *sessionledger.Store, id, harness string) {
	t.Helper()
	tag := harness
	if harness == "human" {
		tag = "zzseedhuman"
	}
	start := time.Date(2026, 10, 26, 5, 0, 0, 0, time.UTC)
	if _, err := store.Enrich(id, sessionledger.Enrichment{
		Harness: tag, ProjectDir: ".", StartedAt: start,
		EndedAt: start.Add(time.Hour), EndSource: sessionledger.EndSourceLiveClose,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
	if tag == harness {
		return
	}
	data, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}
	data = bytes.ReplaceAll(data, []byte(tag), []byte(harness))
	if err := os.WriteFile(store.Path(), data, 0o644); err != nil {
		t.Fatalf("write ledger: %v", err)
	}
}

// harnessRepairFixture seeds the four cases repair must tell apart and returns
// the store and the harness each row must end up with.
func harnessRepairFixture(t *testing.T) (*sessionledger.Store, map[string]string) {
	t.Helper()
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	os.Unsetenv("WIPNOTE_PROJECT_DIR")
	os.Unsetenv("CLAUDE_PROJECT_DIR")
	wipnoteDir := filepath.Join(backfillTestProject(t), ".wipnote")
	store := sessionledger.NewStore(wipnoteDir)

	seedHarnessRow(t, store, backfillBothSession, "claude-code")
	seedHarnessRow(t, store, backfillHTMLSession, "human")
	writeAgentSessionHTML(t, wipnoteDir, backfillHTMLSession, "codex")
	seedHarnessRow(t, store, backfillArchivedSession, "human") // no HTML at all
	seedHarnessRow(t, store, repairRealTagSession, "gemini")
	writeAgentSessionHTML(t, wipnoteDir, repairRealTagSession, "claude-code")

	return store, map[string]string{
		backfillBothSession:     "claude", // old spelling renamed
		backfillHTMLSession:     "codex",  // inferred from data-agent
		backfillArchivedSession: "",       // nothing to infer: unknown
		repairRealTagSession:    "gemini", // a real tag is never changed
	}
}

func TestRepairDryRunReportsHarnessFixesAndWritesNothing(t *testing.T) {
	store, _ := harnessRepairFixture(t)
	before, err := os.ReadFile(store.Path())
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}

	out := runRepair(t, filepath.Dir(store.Path()), false)

	after, _ := os.ReadFile(store.Path())
	if !bytes.Equal(before, after) {
		t.Fatalf("the dry-run wrote to the ledger")
	}
	for _, want := range []string{
		backfillBothSession + "\n  harness claude-code -> claude",
		backfillHTMLSession + "\n  harness human -> codex",
		backfillArchivedSession + "\n  harness human -> (unknown)",
		"3 session harness tag(s) would be corrected",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("dry-run output missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, repairRealTagSession) {
		t.Errorf("dry-run proposed changing a real harness tag:\n%s", out)
	}
}

func TestRepairApplyFixesHarnessTags(t *testing.T) {
	store, want := harnessRepairFixture(t)

	runRepair(t, filepath.Dir(store.Path()), true)

	recs, err := store.ReadAll()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(recs) != len(want) {
		t.Fatalf("repair changed the row count: got %d, want %d", len(recs), len(want))
	}
	for _, r := range recs {
		if r.Harness != want[r.SessionID] {
			t.Errorf("%s: harness = %q, want %q", r.SessionID, r.Harness, want[r.SessionID])
		}
	}

	out := runRepair(t, filepath.Dir(store.Path()), true)
	if strings.Contains(out, "harness ") {
		t.Errorf("a second repair still proposed harness changes:\n%s", out)
	}
}
