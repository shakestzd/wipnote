package main

import (
	"fmt"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// finalizeYAMLOptions tunes finalizeYAMLCanonicalOpts.
type finalizeYAMLOptions struct {
	// Regenerate forces a fresh feature for every approved slice, even when
	// the slice already carries a feature_id that still exists (the
	// pre-GH-#116 destructive behaviour). The previous features are NOT
	// deleted — they are left on the track as orphans, so this is opt-in.
	Regenerate bool
}

// reusableSliceFeature returns the existing feature bound to an approved slice
// when finalize-yaml should reuse it instead of creating a new one — mirroring
// promote-slice Rule 3 (GH-#116). It returns nil when the slice has no
// feature_id, when that feature no longer exists (deleted/archived — a fresh
// one is created so the slice does not stay orphaned), or when the caller
// explicitly asked for regeneration.
func reusableSliceFeature(p *workitem.Project, featureID string, regenerate bool) *finalizedFeature {
	if regenerate || featureID == "" {
		return nil
	}
	feat, err := p.Features.Get(featureID)
	if err != nil || feat == nil {
		return nil
	}
	return &finalizedFeature{id: feat.ID, title: feat.Title}
}

// nodeHasEdge reports whether node already carries an edge of relationship
// rel pointing at targetID. Node.AddEdge appends unconditionally, so callers
// that may run more than once (finalize after reopen) must check first.
func nodeHasEdge(node *models.Node, targetID string, rel models.RelationshipType) bool {
	if node == nil {
		return false
	}
	for _, e := range node.Edges[string(rel)] {
		if e.TargetID == targetID {
			return true
		}
	}
	return false
}

// addFeatureEdgeOnce adds e to featureID unless an equivalent edge exists.
func addFeatureEdgeOnce(p *workitem.Project, featureID string, e models.Edge) {
	node, err := p.Features.Get(featureID)
	if err == nil && nodeHasEdge(node, e.TargetID, e.Relationship) {
		return
	}
	p.Features.AddEdge(featureID, e) //nolint:errcheck
}

// addPlanEdgeOnce adds e to planID unless an equivalent edge exists.
func addPlanEdgeOnce(p *workitem.Project, planID string, e models.Edge) {
	node, err := p.Plans.Get(planID)
	if err == nil && nodeHasEdge(node, e.TargetID, e.Relationship) {
		return
	}
	p.Plans.AddEdge(planID, e) //nolint:errcheck
}

// sliceFeatureIDsByNum snapshots slice num -> feature_id before finalize
// mutates the plan, so --regenerate can report what it orphaned.
func sliceFeatureIDsByNum(slices []planyaml.PlanSlice) map[int]string {
	out := map[int]string{}
	for _, s := range slices {
		if s.FeatureID != "" {
			out[s.Num] = s.FeatureID
		}
	}
	return out
}

// warnRegeneratedFeatures tells the operator which features were orphaned by
// --regenerate so they can be cleaned up by hand.
func warnRegeneratedFeatures(old map[int]string, numToFeat map[int]finalizedFeature) {
	for num, oldID := range old {
		if cf, ok := numToFeat[num]; ok && cf.id != oldID {
			fmt.Fprintf(stderr, "Warning: slice-%d: --regenerate replaced %s with %s; the old feature is NOT deleted\n", num, oldID, cf.id)
		}
	}
}
