package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

// These tests dispatch through the paths a real `wipnote lineage` runs, against
// a delegations ledger and claim ledger written by their own stores in a temp
// project (plan-28b4a7f6 slice 13).

const (
	dlgTagged  = "dlg-aaaaaaaaaaaa"
	dlgDerived = "dlg-bbbbbbbbbbbb"
	dlgForeign = "dlg-cccccccccccc"
	dlgOther   = "dlg-dddddddddddd"

	dlgCaller   = "0c0c0c0c-1111-4222-8333-944445555666"
	dlgWorker   = "0d0d0d0d-1111-4222-8333-944445555666"
	dlgItemSess = "0b7c2f1e-1111-4222-8333-944445555666" // an implemented_in session of the item
)

var dlgAt = time.Date(2026, 10, 27, 16, 0, 0, 0, time.UTC)

type dlgFixture struct {
	wipnoteDir string
	feature    string
}

// newDlgFixture creates a feature implemented in dlgItemSess, with the commit
// seams off so no store write reaches git.
func newDlgFixture(t *testing.T) dlgFixture {
	t.Helper()
	for _, k := range []string{"WIPNOTE_PROJECT_DIR", "CLAUDE_PROJECT_DIR"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	origD, origC := delegationledger.OnCommit, claimledger.OnCommit
	delegationledger.OnCommit, claimledger.OnCommit = nil, nil
	t.Cleanup(func() { delegationledger.OnCommit, claimledger.OnCommit = origD, origC })
	f := dlgFixture{wipnoteDir: newLineageProjectionWipnoteDir(t)}
	p, err := workitem.Open(f.wipnoteDir, "test-agent")
	if err != nil {
		t.Fatal(err)
	}
	feat, err := p.Features.Create("Served by delegations", workitem.FeatWithStatus("in-progress"))
	if err != nil {
		t.Fatal(err)
	}
	f.feature = feat.ID
	if _, err := p.Features.AddEdge(feat.ID, models.Edge{TargetID: dlgItemSess, Relationship: models.RelImplementedIn}); err != nil {
		t.Fatal(err)
	}
	return f
}

func (f dlgFixture) openRun(t *testing.T, r delegationledger.Record) {
	t.Helper()
	if r.Started.IsZero() {
		r.Started = dlgAt
	}
	if _, err := delegationledger.NewStore(f.wipnoteDir).Open(r); err != nil {
		t.Fatal(err)
	}
}

// seedRuns writes one tagged, one derived, one foreign-tagged (whose worker is
// also an item session) and one unrelated run.
func (f dlgFixture) seedRuns(t *testing.T) {
	t.Helper()
	exit0 := 0
	f.openRun(t, delegationledger.Record{ID: dlgTagged, Mode: "oneshot", Harness: "codex", WorkItem: f.feature,
		CallerSession: dlgCaller, WorkerSessions: []string{"thread-1"}})
	f.openRun(t, delegationledger.Record{ID: dlgDerived, Mode: "interactive", Harness: "claude", Name: "w1",
		Link: "sid", WorkerSessions: []string{"sid-other", dlgItemSess},
		Ended: dlgAt.Add(time.Hour), Exit: &exit0, EndReason: delegationledger.EndExit})
	f.openRun(t, delegationledger.Record{ID: dlgForeign, Mode: "interactive", Harness: "claude", Name: "w2",
		WorkItem: "feat-ffffffff", Link: "tag", WorkerSessions: []string{dlgItemSess}})
	f.openRun(t, delegationledger.Record{ID: dlgOther, Mode: "oneshot", Harness: "claude-print",
		WorkerSessions: []string{"sid-unrelated"}})
}

func dlgJSON(t *testing.T, f dlgFixture, root string) (lineageJSON, map[string]json.RawMessage) {
	t.Helper()
	var buf bytes.Buffer
	if err := runLineageFor(&buf, f.wipnoteDir, root, lineageOpts{depth: 5, jsonOut: true}); err != nil {
		t.Fatalf("lineage %s: %v", root, err)
	}
	var out lineageJSON
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json: %v\n%s", err, buf.String())
	}
	_ = json.Unmarshal(buf.Bytes(), &raw)
	return out, raw
}

func dlgText(t *testing.T, f dlgFixture, root string) string {
	t.Helper()
	var buf bytes.Buffer
	if err := runLineageFor(&buf, f.wipnoteDir, root, lineageOpts{depth: 5}); err != nil {
		t.Fatalf("lineage %s: %v", root, err)
	}
	return buf.String()
}

func delegationLinks(out lineageJSON) map[string]string {
	links := map[string]string{}
	for _, d := range out.Delegations {
		links[d.ID] = d.Link
	}
	return links
}

func TestLineageDelegations_TagAndSid(t *testing.T) {
	f := newDlgFixture(t)
	f.seedRuns(t)
	out, _ := dlgJSON(t, f, f.feature)
	links := delegationLinks(out)
	if len(links) != 2 || links[dlgTagged] != "tag" || links[dlgDerived] != "sid" {
		t.Fatalf("want %s (tag) and %s (sid) only, got %v", dlgTagged, dlgDerived, links)
	}
	for _, d := range out.Delegations {
		switch d.ID {
		case dlgTagged:
			if d.Mode != "oneshot" || d.Harness != "codex" || d.State != "running" || d.Name != "" ||
				strings.Join(d.WorkerSessions, ",") != "thread-1" || d.CallerSession != dlgCaller {
				t.Errorf("tagged row wrong: %+v", d)
			}
		case dlgDerived:
			if d.Mode != "interactive" || d.State != "ended" || d.Name != "w1" || len(d.WorkerSessions) != 2 {
				t.Errorf("derived row wrong: %+v", d)
			}
		}
	}
	if len(out.Forward) == 0 {
		t.Error("the graph walk must still be present next to the delegations section")
	}
	text := dlgText(t, f, f.feature)
	for _, want := range []string{"delegations (2)", dlgTagged, dlgDerived, "derived: worker session", "tagged"} {
		if !strings.Contains(text, want) {
			t.Errorf("text missing %q:\n%s", want, text)
		}
	}
	for _, unwanted := range []string{dlgOther, dlgForeign, f.wipnoteDir} {
		if strings.Contains(text, unwanted) {
			t.Errorf("text must not contain %q:\n%s", unwanted, text)
		}
	}
}

func TestLineageDelegations_NeverDeriveForeignTag(t *testing.T) {
	f := newDlgFixture(t)
	// Only a run tagged with another item, whose worker IS one of this item's
	// sessions: the sid join must not pull it in.
	f.openRun(t, delegationledger.Record{ID: dlgForeign, Mode: "interactive", Harness: "claude", Name: "w2",
		WorkItem: "feat-ffffffff", Link: "tag", WorkerSessions: []string{dlgItemSess}})
	_, raw := dlgJSON(t, f, f.feature)
	if _, ok := raw["delegations"]; ok {
		t.Errorf("a row tagged with another item was derived: %s", raw["delegations"])
	}
	if text := dlgText(t, f, f.feature); strings.Contains(text, "delegations (") {
		t.Errorf("text must not show a delegations section:\n%s", text)
	}
	// The other side: the same row is shown, tagged, under its own item.
	got := delegationsForWorkItem([]delegationledger.Record{{ID: dlgForeign, WorkItem: "feat-ffffffff",
		Mode: "interactive", Harness: "claude", WorkerSessions: []string{dlgItemSess}, Started: dlgAt}},
		"feat-ffffffff", nil)
	if len(got) != 1 || got[0].Link != "tag" {
		t.Errorf("own item must show the tagged row, got %+v", got)
	}
}

func TestLineageDelegations_AbsentWithoutLedger(t *testing.T) {
	f := newDlgFixture(t)
	_, raw := dlgJSON(t, f, f.feature)
	for _, k := range []string{"delegations", "delegations_error", "orch_sessions", "orch_error"} {
		if _, ok := raw[k]; ok {
			t.Errorf("%s must be absent with no ledger", k)
		}
	}
}

func TestLineageDelegations_UnreadableLedgerIsReported(t *testing.T) {
	f := newDlgFixture(t)
	path := filepath.Join(f.wipnoteDir, delegationledger.FileName)
	if err := os.Mkdir(path, 0o755); err != nil { // a directory cannot be read as a ledger
		t.Fatal(err)
	}
	out, _ := dlgJSON(t, f, f.feature)
	if out.DelegationsError == "" || len(out.Delegations) != 0 {
		t.Errorf("want delegations_error and no rows, got %+v / %q", out.Delegations, out.DelegationsError)
	}
	if len(out.Forward) == 0 {
		t.Error("a broken ledger must not break the rest of lineage")
	}
	if text := dlgText(t, f, f.feature); !strings.Contains(text, "delegations: unreadable") {
		t.Errorf("text must say the ledger is unreadable:\n%s", text)
	}
}

func TestLineageDlgRoot(t *testing.T) {
	f := newDlgFixture(t)
	f.seedRuns(t)
	out, raw := dlgJSON(t, f, dlgTagged)
	if out.Root != dlgTagged || out.Kind != "delegation" || len(out.Delegations) != 1 {
		t.Fatalf("dlg root json wrong: %+v", out)
	}
	d := out.Delegations[0]
	if d.CallerSession != dlgCaller || d.WorkItem != f.feature || strings.Join(d.WorkerSessions, ",") != "thread-1" {
		t.Errorf("dlg root row wrong: %+v", d)
	}
	if string(raw["forward"]) != "[]" || string(raw["backward"]) != "[]" {
		t.Errorf("forward/backward must be empty lists, got %s / %s", raw["forward"], raw["backward"])
	}
	text := dlgText(t, f, dlgTagged)
	for _, want := range []string{"Lineage: " + dlgTagged, "[delegation]", "caller session: " + dlgCaller,
		"work item: " + f.feature, "worker sessions: thread-1"} {
		if !strings.Contains(text, want) {
			t.Errorf("dlg text missing %q:\n%s", want, text)
		}
	}
	var buf bytes.Buffer
	if err := runLineageFor(&buf, f.wipnoteDir, "dlg-eeeeeeeeeeee", lineageOpts{depth: 5}); err == nil ||
		!strings.Contains(err.Error(), "dlg-eeeeeeeeeeee") {
		t.Errorf("an unknown dlg- id must fail naming it, got %v", err)
	}
	if detectLineageKind(dlgTagged) != kindDelegation {
		t.Errorf("dlg- must route to the delegation kind")
	}
}

// claim opens (and, when end is set, closes) one claim episode.
func (f dlgFixture) claim(t *testing.T, session, item string, start, end time.Time, outcome claimledger.Outcome) {
	t.Helper()
	s := claimledger.NewStore(f.wipnoteDir)
	if _, _, err := s.Open(session, claimledger.Episode{WorkItemID: item, SessionID: session,
		AgentID: "__root__", StartedAt: start}); err != nil {
		t.Fatal(err)
	}
	if !end.IsZero() {
		if _, err := s.Close(session, session, "__root__", item, outcome, end); err != nil {
			t.Fatal(err)
		}
	}
}

func interactiveTaggedRun(f dlgFixture) delegationledger.Record {
	return delegationledger.Record{ID: dlgTagged, Mode: "interactive", Harness: "claude", Name: "w1",
		WorkItem: f.feature, Link: "tag", CallerSession: dlgCaller, WorkerSessions: []string{dlgWorker}}
}

func TestLineageDelegations_WorkerClaimEpisodes(t *testing.T) {
	f := newDlgFixture(t)
	f.openRun(t, interactiveTaggedRun(f))
	f.claim(t, dlgWorker, f.feature, dlgAt.Add(time.Minute), dlgAt.Add(30*time.Minute), claimledger.OutcomeReleased)
	f.claim(t, "0e0e0e0e-1111-4222-8333-944445555666", f.feature, dlgAt, time.Time{}, "") // not a worker
	out, _ := dlgJSON(t, f, f.feature)
	if len(out.Delegations) != 1 {
		t.Fatalf("want one delegation, got %+v", out.Delegations)
	}
	claims := out.Delegations[0].Claims
	if len(claims) != 1 || claims[0].Session != dlgWorker || claims[0].Outcome != "released" ||
		claims[0].StartedAt == "" || claims[0].EndedAt == "" {
		t.Fatalf("want the worker's one closed episode, got %+v", claims)
	}
	text := dlgText(t, f, f.feature)
	if !strings.Contains(text, "claim "+claims[0].Episode) || !strings.Contains(text, "released") {
		t.Errorf("text must show the worker's claim episode:\n%s", text)
	}
	if text := dlgText(t, f, dlgTagged); !strings.Contains(text, "claim "+claims[0].Episode) {
		t.Errorf("dlg root must show the worker's claim episode:\n%s", text)
	}
}

func TestLineageDelegations_CoClaimNotCollision(t *testing.T) {
	f := newDlgFixture(t)
	f.openRun(t, interactiveTaggedRun(f))
	f.claim(t, dlgCaller, f.feature, dlgAt, time.Time{}, "")                                                                 // orchestrator, open
	f.claim(t, dlgWorker, f.feature, dlgAt.Add(time.Minute), time.Time{}, "")                                                // worker, overlapping
	f.claim(t, dlgWorker, "feat-ffffffff", dlgAt.Add(2*time.Minute), dlgAt.Add(3*time.Minute), claimledger.OutcomeCompleted) // other item
	out, _ := dlgJSON(t, f, f.feature)
	if len(out.Delegations) != 1 {
		t.Fatalf("want one delegation, got %+v", out.Delegations)
	}
	claims := out.Delegations[0].Claims
	byItem := map[string]lineageClaim{}
	for _, c := range claims {
		byItem[c.WorkItem] = c
	}
	if c := byItem[f.feature]; c.CoClaim != dlgTagged || c.Outcome != "open" {
		t.Errorf("overlapping worker episode must be a co-claim of %s, got %+v", dlgTagged, c)
	}
	if c := byItem["feat-ffffffff"]; c.CoClaim != "" {
		t.Errorf("an episode with no overlapping caller episode is no co-claim, got %+v", c)
	}
	text := dlgText(t, f, f.feature)
	if !strings.Contains(text, "co-claim with caller ("+dlgTagged+")") {
		t.Errorf("text must name the co-claim with the dlg- id:\n%s", text)
	}
	if strings.Contains(strings.ToLower(text), "collision") {
		t.Errorf("a co-claim must not read as a collision:\n%s", text)
	}
}

func TestLineageDelegations_CoClaimNeedsOverlap(t *testing.T) {
	run := delegationledger.Record{ID: dlgTagged, CallerSession: dlgCaller, WorkerSessions: []string{dlgWorker}}
	ep := func(sess string, start, end time.Duration) claimledger.Episode {
		e := claimledger.Episode{ID: "ep-" + sess[:4], WorkItemID: "feat-11111111", SessionID: sess,
			RootSessionID: sess, StartedAt: dlgAt.Add(start)}
		if end > 0 {
			e.EndedAt, e.Outcome = dlgAt.Add(end), claimledger.OutcomeReleased
		}
		return e
	}
	before := []claimledger.Episode{ep(dlgCaller, 0, time.Minute), ep(dlgWorker, 2*time.Minute, 0)}
	if got := claimsFor(run, before); len(got) != 1 || got[0].CoClaim != "" {
		t.Errorf("a caller episode that ended before the worker's began is no co-claim: %+v", got)
	}
	overlap := []claimledger.Episode{ep(dlgCaller, 0, 3*time.Minute), ep(dlgWorker, 2*time.Minute, 0)}
	if got := claimsFor(run, overlap); len(got) != 1 || got[0].CoClaim != dlgTagged {
		t.Errorf("overlapping episodes are a co-claim: %+v", got)
	}
}

// Reading lineage must leave the .wipnote store byte-for-byte as it was.
func TestLineageDelegations_NeverWritesStore(t *testing.T) {
	f := newDlgFixture(t)
	f.seedRuns(t)
	f.claim(t, dlgWorker, f.feature, dlgAt, time.Time{}, "")
	before := snapshotTree(t, f.wipnoteDir)
	_ = dlgText(t, f, f.feature)
	_, _ = dlgJSON(t, f, f.feature)
	_ = dlgText(t, f, dlgTagged)
	if after := snapshotTree(t, f.wipnoteDir); !equalTrees(before, after) {
		t.Error("lineage wrote into .wipnote while reading delegations")
	}
}

func TestFeatureShow_ListsDelegations(t *testing.T) {
	f := newDlgFixture(t)
	f.seedRuns(t)
	projectDirFlag = filepath.Dir(f.wipnoteDir)
	t.Cleanup(func() { projectDirFlag = "" })
	out := captureStdout(t, func() {
		if err := runWiShowWithFormat(f.feature, "text"); err != nil {
			t.Fatalf("show: %v", err)
		}
	})
	if !strings.Contains(out, "delegations (2)") || !strings.Contains(out, dlgTagged) || !strings.Contains(out, dlgDerived) {
		t.Errorf("feature show must list the item's delegations:\n%s", out)
	}
	if strings.Contains(out, dlgForeign) || strings.Contains(out, dlgOther) {
		t.Errorf("feature show listed another item's delegation:\n%s", out)
	}
}

// Moved from the retired lineage_orch_test.go with directSessions.
func snapshotTree(t *testing.T, root string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		data, _ := os.ReadFile(path)
		out[path] = info.ModTime().String() + "|" + string(data)
		return nil
	})
	return out
}

func equalTrees(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

func TestDirectSessionsTakesOnlyTheRootsOwnImplementedInEdges(t *testing.T) {
	nodes := []lineageNode{
		{ID: "s-own", EdgeType: "implemented_in", Parent: "feat-aaaaaaaa"},
		{ID: "s-related", EdgeType: "implemented_in", Parent: "feat-bbbbbbbb"},
		{ID: "trk-cccccccc", EdgeType: "part_of", Parent: "feat-aaaaaaaa"},
	}
	got := directSessions("feat-aaaaaaaa", nodes)
	if len(got) != 1 || !got["s-own"] {
		t.Errorf("want only s-own, got %v", got)
	}
}
