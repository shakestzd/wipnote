package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// setupReusePlan writes a 2-slice approved plan (slice 2 depends on slice 1)
// into a fresh wipnote dir and returns (dir, planID).
func setupReusePlan(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	for _, sub := range []string{"plans", "features", "tracks"} {
		if err := os.MkdirAll(filepath.Join(dir, sub), 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", sub, err)
		}
	}
	pID := workitem.GenerateID("plan", "reuse feature ids")
	plan := planyaml.NewPlan(pID, "Reuse Feature IDs", "")
	plan.Meta.Status = "active"
	for i := 1; i <= 2; i++ {
		s := planyaml.PlanSlice{
			ID:       workitem.GenerateID("slice", fmt.Sprintf("s%d", i)),
			Num:      i,
			Title:    fmt.Sprintf("Slice %d", i),
			What:     fmt.Sprintf("do thing %d", i),
			Approved: true,
		}
		if i == 2 {
			s.Deps = []int{1}
		}
		plan.Slices = append(plan.Slices, s)
	}
	planPath := filepath.Join(dir, "plans", pID+".yaml")
	if err := planyaml.Save(planPath, plan); err != nil {
		t.Fatalf("save plan yaml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "plans", pID+".html"), []byte("<html></html>"), 0o644); err != nil {
		t.Fatalf("write html stub: %v", err)
	}
	return dir, pID
}

// reopenAndAddApprovedSlice reopens the plan, appends slice 3 (depends on 1)
// and approves it.
func reopenAndAddApprovedSlice(t *testing.T, dir, pID string) {
	t.Helper()
	if err := executePlanReopen(dir, pID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := runPlanAddSliceYAML(dir, pID, "Slice 3", "do thing 3", "", "", "", "", "S", "Low", "1"); err != nil {
		t.Fatalf("add-slice-yaml: %v", err)
	}
	if err := runApproveSlice(dir, pID, "3"); err != nil {
		t.Fatalf("approve-slice: %v", err)
	}
}

func countFeatures(t *testing.T, dir string) int {
	t.Helper()
	p, err := workitem.Open(dir, "test-agent")
	if err != nil {
		t.Fatalf("workitem.Open: %v", err)
	}
	defer p.Close()
	feats, err := p.Features.List()
	if err != nil {
		t.Fatalf("features list: %v", err)
	}
	return len(feats)
}

// TestFinalizeYAML_ReopenAddSliceReusesFeatureIDs is the regression test for
// GH-#116: finalize → reopen → add-slice → approve → finalize must keep the
// original feature IDs and create exactly one new feature for the new slice.
func TestFinalizeYAML_ReopenAddSliceReusesFeatureIDs(t *testing.T) {
	dir, pID := setupReusePlan(t)

	ids1, failures, err := finalizeYAMLCanonical(dir, pID)
	if err != nil {
		t.Fatalf("first finalize: %v", err)
	}
	if len(failures) > 0 || len(ids1) != 2 {
		t.Fatalf("first finalize: ids=%v failures=%v, want 2 ids and no failures", ids1, failures)
	}

	reopenAndAddApprovedSlice(t, dir, pID)

	ids2, failures, err := finalizeYAMLCanonical(dir, pID)
	if err != nil {
		t.Fatalf("second finalize: %v", err)
	}
	if len(failures) > 0 {
		t.Fatalf("second finalize failures: %v", failures)
	}
	if len(ids2) != 3 {
		t.Fatalf("second finalize returned %d ids, want 3: %v", len(ids2), ids2)
	}
	for i := range ids1 {
		if ids2[i] != ids1[i] {
			t.Errorf("slice %d: feature id regenerated: got %q, want %q", i+1, ids2[i], ids1[i])
		}
	}
	if ids2[2] == ids1[0] || ids2[2] == ids1[1] {
		t.Errorf("new slice must get a new feature, got %q", ids2[2])
	}
	if got := countFeatures(t, dir); got != 3 {
		t.Errorf("features on disk = %d, want 3 (2 reused + 1 new)", got)
	}

	// YAML still binds the original IDs and the new one.
	reloaded, err := planyaml.Load(filepath.Join(dir, "plans", pID+".yaml"))
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if reloaded.Meta.Status != "finalized" {
		t.Errorf("status = %q, want finalized", reloaded.Meta.Status)
	}
	for i, s := range reloaded.Slices {
		if s.FeatureID != ids2[i] {
			t.Errorf("slice[%d] yaml feature_id = %q, want %q", i, s.FeatureID, ids2[i])
		}
	}

	// Edges: reused slice 2 must still have exactly one blocked_by edge on
	// slice 1 (not duplicated), and the new slice 3 must be blocked_by slice 1.
	p, err := workitem.Open(dir, "test-agent")
	if err != nil {
		t.Fatalf("workitem.Open: %v", err)
	}
	defer p.Close()
	f2, err := p.Features.Get(ids2[1])
	if err != nil {
		t.Fatalf("get feature 2: %v", err)
	}
	if n := len(f2.Edges["blocked_by"]); n != 1 {
		t.Errorf("feature 2 blocked_by edges = %d, want 1 (idempotent re-link)", n)
	}
	f3, err := p.Features.Get(ids2[2])
	if err != nil {
		t.Fatalf("get feature 3: %v", err)
	}
	if !nodeHasEdge(f3, ids1[0], "blocked_by") {
		t.Errorf("feature 3 should be blocked_by %s, edges=%v", ids1[0], f3.Edges["blocked_by"])
	}
}

// TestFinalizeYAML_RegenerateMintsNewFeatures covers the opt-in destructive
// path: --regenerate replaces every approved slice's feature.
func TestFinalizeYAML_RegenerateMintsNewFeatures(t *testing.T) {
	dir, pID := setupReusePlan(t)

	ids1, _, err := finalizeYAMLCanonical(dir, pID)
	if err != nil || len(ids1) != 2 {
		t.Fatalf("first finalize: ids=%v err=%v", ids1, err)
	}
	reopenAndAddApprovedSlice(t, dir, pID)

	ids2, _, err := finalizeYAMLCanonicalOpts(dir, pID, finalizeYAMLOptions{Regenerate: true})
	if err != nil {
		t.Fatalf("regenerate finalize: %v", err)
	}
	if len(ids2) != 3 {
		t.Fatalf("regenerate returned %d ids, want 3", len(ids2))
	}
	for i := range ids1 {
		if ids2[i] == ids1[i] {
			t.Errorf("slice %d: --regenerate should mint a new feature, still %q", i+1, ids1[i])
		}
	}
	if got := countFeatures(t, dir); got != 5 {
		t.Errorf("features on disk = %d, want 5 (2 orphaned + 3 new)", got)
	}
}

// TestFinalizeYAML_ReuseSkipsDeletedFeature: a stale feature_id whose feature
// no longer exists must not be reused — a fresh feature is created instead.
func TestFinalizeYAML_ReuseSkipsDeletedFeature(t *testing.T) {
	dir, pID := setupReusePlan(t)
	ids1, _, err := finalizeYAMLCanonical(dir, pID)
	if err != nil || len(ids1) != 2 {
		t.Fatalf("first finalize: ids=%v err=%v", ids1, err)
	}
	if err := os.Remove(filepath.Join(dir, "features", ids1[0]+".html")); err != nil {
		t.Fatalf("delete feature: %v", err)
	}
	if err := executePlanReopen(dir, pID); err != nil {
		t.Fatalf("reopen: %v", err)
	}
	ids2, _, err := finalizeYAMLCanonical(dir, pID)
	if err != nil {
		t.Fatalf("second finalize: %v", err)
	}
	if len(ids2) != 2 || ids2[0] == ids1[0] || ids2[1] != ids1[1] {
		t.Errorf("ids after re-finalize = %v; want slice 1 recreated and slice 2 = %q kept", ids2, ids1[1])
	}
}
