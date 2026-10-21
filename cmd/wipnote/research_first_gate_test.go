package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/research"
	"github.com/shakestzd/wipnote/core/workitem"
)

// Research-first gate (docs/design/research-gate.md): CLI wiring tests. The
// pure rules are covered in core/research.

func writeResearchConfig(t *testing.T, projectRoot, mode string) {
	t.Helper()
	body := `{"research":{"mode":"` + mode + `"}}`
	if err := os.WriteFile(filepath.Join(projectRoot, ".wipnote", "config.json"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func openTestProject(t *testing.T, hgDir string) *workitem.Project {
	t.Helper()
	p, err := workitem.Open(hgDir, "claude-code")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { p.Close() })
	return p
}

func parseItem(t *testing.T, hgDir, id string) *models.Node {
	t.Helper()
	n, err := htmlparse.ParseFile(resolveNodePath(hgDir, id))
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func fullRecordFlags() *researchAddOpts {
	return &researchAddOpts{
		question:  "How should header toolbar actions overflow?",
		primary:   []string{"https://primer.style/components/action-bar"},
		exists:    research.ExistsNoneFound,
		queries:   []string{"wipnote relevant overflow"},
		options:   []string{"hamburger menu", "priority overflow"},
		chosen:    "priority overflow",
		rejected:  []string{"hamburger menu: NN/g data is about site navigation"},
		retrieved: "2026-10-21",
	}
}

func TestCreate_InfersAndStoresChangeKind(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Header toolbar overflow menu", trackID)
	if got := research.KindOf(parseItem(t, hgDir, id).Properties); got != research.KindUI {
		t.Fatalf("change kind = %q, want ui", got)
	}
	bad := runWiCreate("feature", "x", &wiCreateOpts{trackID: trackID, description: "d", changeKind: "nope"})
	if bad == nil {
		t.Fatal("invalid --change-kind must be rejected")
	}
}

func TestResearchGate_Modes(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Header toolbar overflow menu", trackID)

	for _, tc := range []struct {
		mode    string
		wantErr bool
		wantOut bool
	}{{"off", false, false}, {"warn", false, true}, {"block", true, true}} {
		writeResearchConfig(t, tmpDir, tc.mode)
		var out bytes.Buffer
		err := checkResearchGateForWorkItem(&out, tmpDir, id, "")
		if (err != nil) != tc.wantErr {
			t.Errorf("mode %s: err=%v wantErr=%v", tc.mode, err, tc.wantErr)
		}
		if tc.wantErr && !strings.Contains(err.Error(), "wipnote research start") {
			t.Errorf("mode %s: error must carry the fix command: %v", tc.mode, err)
		}
		if (out.Len() > 0) != (tc.wantOut && !tc.wantErr) {
			t.Errorf("mode %s: output=%q", tc.mode, out.String())
		}
	}

	// A complete record satisfies block mode.
	rec, err := fullRecordFlags().record()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := saveResearchRecord(openTestProject(t, hgDir).Features.Collection, id, rec); err != nil {
		t.Fatal(err)
	}
	if err := checkResearchGateForWorkItem(&bytes.Buffer{}, tmpDir, id, ""); err != nil {
		t.Fatalf("complete record must pass block mode: %v", err)
	}
}

func TestResearchGate_WaiverRecordedAndShown(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Redesign the plan API", trackID)
	writeResearchConfig(t, tmpDir, "block")

	const reason = "prototype behind a flag; research follows in spk-x"
	if err := checkResearchGateForWorkItem(&bytes.Buffer{}, tmpDir, id, reason); err != nil {
		t.Fatalf("waiver must satisfy block mode: %v", err)
	}
	n := parseItem(t, hgDir, id)
	if rec := research.FromNode(n); rec == nil || rec.Waiver != reason {
		t.Fatalf("waiver not recorded: %+v", rec)
	}
	var shown bytes.Buffer
	printResearchRecord(&shown, n, "")
	if !strings.Contains(shown.String(), reason) {
		t.Errorf("show output missing waiver: %s", shown.String())
	}
	var lin bytes.Buffer
	if err := runLineageProjection(&lin, hgDir, id, lineageOpts{}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(lin.String(), "waiver") || !strings.Contains(lin.String(), reason) {
		t.Errorf("lineage must list the waiver: %s", lin.String())
	}
}

func TestResearchGate_StartWarnsNeverFails(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Header toolbar overflow menu", trackID)
	writeResearchConfig(t, tmpDir, "block")
	var out bytes.Buffer
	emitResearchStartAdvisory(&out, tmpDir, parseItem(t, hgDir, id))
	if !strings.Contains(out.String(), "wipnote research start") {
		t.Fatalf("start advisory missing fix: %q", out.String())
	}
	if err := runWiSetStatus("feature", id, "in-progress"); err != nil {
		t.Fatalf("start must not fail on missing research: %v", err)
	}
}

func TestResearchStart_SpikeAttachesRecordToParent(t *testing.T) {
	_, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	parentID := createItem(t, hgDir, "feature", "Header toolbar overflow menu", trackID)

	spike, err := startResearchSpike("How should toolbar actions overflow?", parentID)
	if err != nil {
		t.Fatal(err)
	}
	if spike.Properties[research.PropFor] != parentID {
		t.Fatalf("spike not linked: %+v", spike.Properties)
	}
	if !spikeHasEdge(spike, models.RelSpawnedFrom, parentID) {
		t.Error("spike must be spawned_from the parent")
	}
	p := openTestProject(t, hgDir)
	rec, _ := fullRecordFlags().record()
	if _, err := saveResearchRecord(p.Spikes.Collection, spike.ID, rec); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	attachSpikeResearch(&out, p, spike.ID)

	got := research.FromNode(parseItem(t, hgDir, parentID))
	if got == nil || got.Chosen != "priority overflow" || got.SpikeID != spike.ID || got.Question == "" {
		t.Fatalf("parent record not attached: %+v (%s)", got, out.String())
	}
	if missing, _ := research.Assess(got); len(missing) != 0 {
		t.Errorf("attached record should satisfy the gate, missing %v", missing)
	}
}

func spikeHasEdge(n *models.Node, rel models.RelationshipType, target string) bool {
	for _, e := range n.Edges[string(rel)] {
		if e.TargetID == target {
			return true
		}
	}
	return false
}

func TestResearchGate_LegacyArtifactLoadsUngated(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	writeResearchConfig(t, tmpDir, "block")
	p := openTestProject(t, hgDir)
	// Created through the library, as pre-gate artifacts were: no change kind,
	// no research property.
	n, err := p.Features.Create("Legacy feature")
	if err != nil {
		t.Fatal(err)
	}
	parsed := parseItem(t, hgDir, n.ID)
	if research.FromNode(parsed) != nil || research.KindOf(parsed.Properties) != "" {
		t.Fatal("legacy artifact must load with no record and no kind")
	}
	if err := checkResearchGateForWorkItem(&bytes.Buffer{}, tmpDir, n.ID, ""); err != nil {
		t.Fatalf("legacy artifact must not be gated: %v", err)
	}
}

// TestResearchGate_DependencyURLStoredAsSource proves --research-url still
// satisfies the dependency gate and now also lands on the record as a primary
// source, while the legacy note is still written.
func TestResearchGate_DependencyURLStoredAsSource(t *testing.T) {
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Bump dep", trackID)
	seedProvCommit(t, tmpDir, id, "go.mod")
	p := openTestProject(t, hgDir)

	const cited = "https://pkg.go.dev/github.com/some/dep"
	if err := checkDependencyResearchCompleteGate(p, p.Features.Collection, "feature", id, []string{cited}, ""); err != nil {
		t.Fatal(err)
	}
	n := parseItem(t, hgDir, id)
	rec := research.FromNode(n)
	if rec == nil || len(rec.Sources) != 1 || rec.Sources[0].URL != cited || rec.Sources[0].Type != research.SourcePrimary {
		t.Fatalf("url not stored as primary source: %+v", rec)
	}
	if !strings.Contains(n.Content, cited) {
		t.Error("legacy research-evidence note must still be written")
	}
	// A record source alone now satisfies the dependency gate on a later run.
	if err := checkDependencyResearchCompleteGate(p, p.Features.Collection, "feature", id, nil, ""); err != nil {
		t.Fatalf("record source should satisfy the dependency gate: %v", err)
	}
}

// TestResearchGate_BlockModeCompletion drives `feature complete` end to end:
// block mode refuses a gated item without research, and --research-waiver lets
// it through with the rationale recorded.
func TestResearchGate_BlockModeCompletion(t *testing.T) {
	if testing.Short() {
		t.Skip("drives real completion gate (seedPassingGateRecord)")
	}
	tmpDir, hgDir := prepProject(t)
	trackID := testSetupTrack(t, hgDir)
	id := createItem(t, hgDir, "feature", "Header toolbar overflow menu", trackID)
	seedPassingGateRecord(t, tmpDir, "test-session-prov", id)
	seedProvCommit(t, tmpDir, id, "internal/foo/bar.go")
	writeResearchConfig(t, tmpDir, "block")
	wiResearchURL, wiResearchWaiver = nil, ""
	t.Cleanup(func() { wiResearchURL = nil; wiResearchWaiver = "" })

	err := runWiSetStatus("feature", id, "done")
	if err == nil || !strings.Contains(err.Error(), "research gate") {
		t.Fatalf("block mode must refuse completion, got %v", err)
	}
	if parseItem(t, hgDir, id).Status == models.StatusDone {
		t.Fatal("item must not be done when the research gate blocks")
	}

	wiResearchWaiver = "copy change only; pattern already in use on the dashboard"
	if err := runWiSetStatus("feature", id, "done"); err != nil {
		t.Fatalf("waiver must allow completion: %v", err)
	}
	n := parseItem(t, hgDir, id)
	if n.Status != models.StatusDone || research.FromNode(n).Waiver != wiResearchWaiver {
		t.Fatalf("expected done with waiver recorded, status=%s rec=%+v", n.Status, research.FromNode(n))
	}
}
