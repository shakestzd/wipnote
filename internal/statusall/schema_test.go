package statusall

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/procsweep"
)

var update = flag.Bool("update", false, "rewrite the golden files")

// fullReport fills every field of the schema once.
func fullReport() Report {
	exit := 1
	return Report{Schema: Schema, GeneratedAt: "2026-10-28T12:00:00Z", Projects: []Project{{
		Name: "alpha", ID: "0a1b2c3d", LastActivity: "2026-10-28T11:55:00Z", Branch: "work",
		Plan: &Plan{ID: "plan-0000aaaa", Title: "Ship the widget", Done: 1, Total: 3,
			Slice: &Slice{Num: 2, Title: "Second", FeatureID: "feat-11110002", Status: "promoted"}},
		InProgress: []Item{{ID: "feat-11110002", Title: "Second", Type: "feature"}},
		LastCommit: &Commit{SHA: "0123456789abcdef0123456789abcdef01234567", Subject: "Add the thing", At: "2026-10-28T11:00:00Z"},
		PR: &PR{Number: 42, Title: "Add the thing", State: "OPEN", URL: "https://example.invalid/o/r/pull/42",
			Checks: gitwatch.Counts{Pass: 1, Fail: 1, Total: 2}, Failing: []string{"lint"}, Review: "APPROVED", Mergeable: "MERGEABLE"},
		Agents: []Agent{{Source: "herdr+ledger", Harness: "claude", State: "blocked", WorkItem: "feat-11110002",
			Title: "Second", LastLine: "Allow this command?", Pane: "w1:p2", Session: sessA, Since: "2026-10-28T11:40:00Z"}},
		Runs: []Run{{ID: "run-0123456789ab", Kind: "shell", Description: "gate", Command: "scripts/gate.sh",
			Folder: ".", Status: "failed", Exit: &exit, StartedAt: "2026-10-28T11:50:00Z", EndedAt: "2026-10-28T11:55:00Z",
			Commit: "0123456", LogPath: "…/tasks/b1.output", LastLine: "FAIL", FirstError: "FAIL TestSomething",
			WorkItem: "feat-11110002", Session: sessA, Harness: "claude", Line: "gate exit 1 at 11:55 on commit 0123456"}},
		NeedsYou: []Need{{Kind: NeedBlockedAgent, What: "claude agent in pane w1:p2 is blocked", Since: "", WorkItem: "feat-11110002"},
			{Kind: NeedFailedRun, What: "gate exit 1 at 11:55: FAIL TestSomething", Since: "2026-10-28T11:55:00Z", WorkItem: "feat-11110002"},
			{Kind: NeedOrphanProcess, What: "orphaned process 4242 — node ./server.js --port 3000; stop with: kill 4242", Since: "2026-10-28T09:30:00Z"}},
		Orphans: []procsweep.Proc{{PID: 4242, Command: "node ./server.js --port 3000", SessionID: sessA, Project: "alpha",
			StartedAt: time.Date(2026, 10, 28, 9, 30, 0, 0, time.UTC), Orphan: true, StopHint: "kill 4242"}},
		Sources: map[string]string{"git": SourceOK, "gh": SourceOK, "herdr": SourceOK, "procs": SourceOK},
		Errors:  []string{},
	}}}
}

func TestReportJSON_MatchesGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, fullReport()); err != nil {
		t.Fatal(err)
	}
	golden := filepath.Join("testdata", "report.golden.json")
	if *update {
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Fatalf("JSON drifted from the golden file (run with -update after updating the doc):\n%s", buf.String())
	}
}

// Every key in the golden output is documented in docs/design/status-all.md.
func TestReportJSON_EveryKeyIsDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "design", "status-all.md"))
	if err != nil {
		t.Fatal(err)
	}
	var v any
	data, _ := os.ReadFile(filepath.Join("testdata", "report.golden.json"))
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatal(err)
	}
	keys := map[string]bool{}
	collectKeys(v, keys)
	var missing []string
	for k := range keys {
		if !regexp.MustCompile("`" + regexp.QuoteMeta(k) + "`").Match(doc) {
			missing = append(missing, k)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("keys not documented in status-all.md: %s", strings.Join(missing, ", "))
	}
}

// An empty project still prints every list as [] and every object as null,
// so a reader never has to tell "missing" from "empty".
func TestReportJSON_EmptyListsAreArrays(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSON(&buf, Report{Schema: Schema, Projects: []Project{newProject(entry("/x", "x", testNow))}}); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, k := range []string{`"in_progress": []`, `"agents": []`, `"runs": []`, `"needs_you": []`, `"orphans": []`, `"errors": []`, `"plan": null`, `"pr": null`} {
		if !strings.Contains(out, k) {
			t.Fatalf("missing %s in\n%s", k, out)
		}
	}
}

func collectKeys(v any, keys map[string]bool) {
	switch x := v.(type) {
	case map[string]any:
		for k, child := range x {
			keys[k] = true
			collectKeys(child, keys)
		}
	case []any:
		for _, child := range x {
			collectKeys(child, keys)
		}
	}
}
