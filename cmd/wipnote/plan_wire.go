package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planWireCmd wires existing features to a plan and track via graph edges,
// then marks the plan as finalized. This is structural-only — it does not
// create new features; the agent must have already created them.
func planWireCmd() *cobra.Command {
	var trackID string

	cmd := &cobra.Command{
		Use:   "wire <plan-id>",
		Short: "Wire existing features to plan and track, then finalize",
		Long: `Connect existing features to the plan and track via graph edges,
wire blocked_by dependency edges from slice deps, and mark the plan
as finalized. Features are matched to plan slices by title (case-insensitive).

The agent must have already created the features before running this command.

Example:
  wipnote plan wire plan-abc123 --track trk-def456`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return wirePlan(wipnoteDir, args[0], trackID)
		},
	}

	cmd.Flags().StringVar(&trackID, "track", "", "Track ID to wire the plan to (required)")
	_ = cmd.MarkFlagRequired("track")

	return cmd
}

// wirePlan is the testable implementation of planWireCmd.
func wirePlan(wipnoteDir, planID, trackID string) error {
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0): wire reads the plan, wires edges via workitem writes in
	// between, then saves the whole document back. A bare Load-then-Save let
	// two concurrent writers interleave and the second save silently clobber
	// the first. Mirrors storePlanFeedbackEntry.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, p, err := loadWireInputs(wipnoteDir, planPath, trackID)
	if err != nil {
		return err
	}
	defer p.Close()

	approvedTitles := approvedSliceTitles(plan)

	trackFeats, err := planTrackFeatures(p, trackID)
	if err != nil {
		return err
	}

	titleToFeat, unmatched := matchFeaturesToSlices(trackFeats, approvedTitles)

	// Wire edges for each matched feature.
	now := time.Now().UTC()
	linkedCount := wireMatchedFeatures(p, planID, trackID, titleToFeat, now)

	depsWired := wireSliceDepEdges(p, planID, titleToFeat, now)

	// implemented_in: plan → track
	p.Plans.AddEdge(planID, models.Edge{ //nolint:errcheck
		TargetID:     trackID,
		Relationship: models.RelImplementedIn,
		Title:        trackID,
		Since:        now,
	})

	if err := saveWiredPlan(planPath, plan, planID, trackID, linkedCount); err != nil {
		return err
	}

	printWireSummary(planID, trackID, plan.Meta.Version, linkedCount, depsWired, unmatched, approvedTitles, titleToFeat)
	return nil
}

// loadWireInputs loads the plan, opens the project and validates that the
// track exists. The caller closes the returned project.
func loadWireInputs(wipnoteDir, planPath, trackID string) (*planyaml.PlanYAML, *workitem.Project, error) {
	plan, err := planyaml.Load(planPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load plan: %w", err)
	}

	p, err := workitem.Open(wipnoteDir, agentForClaim())
	if err != nil {
		return nil, nil, fmt.Errorf("open project: %w", err)
	}

	// Validate that the track exists.
	if _, err := p.Tracks.Get(trackID); err != nil {
		p.Close()
		return nil, nil, fmt.Errorf("track %s not found: %w", trackID, err)
	}
	return plan, p, nil
}

// wireMatchedFeatures wires planned_in (feature → plan) and part_of +
// contains edges for each matched feature and returns how many it linked.
func wireMatchedFeatures(p *workitem.Project, planID, trackID string, titleToFeat map[string]planWiredFeat, now time.Time) int {
	linkedCount := 0
	for _, wf := range titleToFeat {
		// planned_in: feature → plan
		p.Features.AddEdge(wf.id, models.Edge{ //nolint:errcheck
			TargetID:     planID,
			Relationship: models.RelPlannedIn,
			Title:        planID,
			Since:        now,
		})

		// part_of + contains (idempotent helper)
		wireTrackEdges(p, wf.id, trackID, wf.title) //nolint:errcheck

		linkedCount++
	}
	return linkedCount
}

// saveWiredPlan updates the YAML status to finalized, saves it and commits
// the change.
func saveWiredPlan(planPath string, plan *planyaml.PlanYAML, planID, trackID string, linkedCount int) error {
	plan.Meta.Status = "finalized"
	plan.Meta.TrackID = trackID
	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): wire — %d features linked", planID, linkedCount)); err != nil {
		return fmt.Errorf("autocommit wire: %w", err)
	}
	return nil
}

// planWiredFeat is a track feature matched to a plan slice by title.
type planWiredFeat struct {
	id    string
	title string
	slice planyaml.PlanSlice
}

// approvedSliceTitles collects approved slice titles (lower-cased) for
// matching.
func approvedSliceTitles(plan *planyaml.PlanYAML) map[string]planyaml.PlanSlice {
	approvedTitles := map[string]planyaml.PlanSlice{}
	for _, s := range plan.Slices {
		if s.Approved {
			approvedTitles[strings.ToLower(s.Title)] = s
		}
	}
	// If no slices are marked approved, treat all as approved (agent-created path).
	if len(approvedTitles) == 0 {
		for _, s := range plan.Slices {
			approvedTitles[strings.ToLower(s.Title)] = s
		}
	}

	return approvedTitles
}

// planTrackFeatures finds all features belonging to the track (by TrackID
// field or contains edges).
func planTrackFeatures(p *workitem.Project, trackID string) ([]*models.Node, error) {
	// Use WithTrackID filter first (works when FeatWithTrack was used at creation).
	trackFeats, err := p.Features.List(workitem.WithTrackID(trackID))
	if err != nil {
		return nil, fmt.Errorf("list features for track: %w", err)
	}

	// Also include features linked via contains edges on the track (alternative wiring).
	containsIDs := findFeaturesForTrack(p, trackID)
	seenIDs := map[string]bool{}
	for _, f := range trackFeats {
		seenIDs[f.ID] = true
	}
	for _, id := range containsIDs {
		if !seenIDs[id] {
			if fn, ferr := p.Features.Get(id); ferr == nil {
				trackFeats = append(trackFeats, fn)
				seenIDs[id] = true
			}
		}
	}

	return trackFeats, nil
}

// matchFeaturesToSlices matches each track feature to a plan slice by title.
func matchFeaturesToSlices(trackFeats []*models.Node, approvedTitles map[string]planyaml.PlanSlice) (map[string]planWiredFeat, []string) {
	titleToFeat := map[string]planWiredFeat{}
	var unmatched []string

	for _, featNode := range trackFeats {
		key := strings.ToLower(featNode.Title)
		if slice, ok := approvedTitles[key]; ok {
			titleToFeat[key] = planWiredFeat{id: featNode.ID, title: featNode.Title, slice: slice}
		} else {
			unmatched = append(unmatched, featNode.ID)
		}
	}

	return titleToFeat, unmatched
}

// wireSliceDepEdges wires blocked_by edges based on slice deps (match by
// slice num → feature) and returns how many it wired.
// These encode plan authoring order, not an asserted cross-project
// dependency, so they carry the same origin tag reindex_plan_edges.go
// stamps on the equivalent edge it rebuilds from the same slice.deps
// field — graph.FindBottlenecks excludes both (bug-d0489158).
func wireSliceDepEdges(p *workitem.Project, planID string, titleToFeat map[string]planWiredFeat, now time.Time) int {
	numToFeat := map[int]planWiredFeat{}
	for _, wf := range titleToFeat {
		numToFeat[wf.slice.Num] = wf
	}

	depsWired := 0
	for _, wf := range titleToFeat {
		for _, depNum := range wf.slice.Deps {
			depFeat, ok := numToFeat[depNum]
			if !ok {
				continue
			}
			p.Features.AddEdge(wf.id, models.Edge{ //nolint:errcheck
				TargetID:     depFeat.id,
				Relationship: models.RelBlockedBy,
				Since:        now,
				Properties: map[string]string{
					"origin":        graph.EdgeOriginPlanSlice,
					"plan_id":       planID,
					"slice_num":     fmt.Sprintf("%d", wf.slice.Num),
					"dep_slice_num": fmt.Sprintf("%d", depNum),
				},
			})
			depsWired++
		}
	}

	return depsWired
}

// printWireSummary prints the wire summary and the unmatched features and
// slices.
func printWireSummary(planID, trackID string, version, linkedCount, depsWired int, unmatched []string, approvedTitles map[string]planyaml.PlanSlice, titleToFeat map[string]planWiredFeat) {
	// Print summary.
	fmt.Printf("Wired plan %s to track %s\n", planID, trackID)
	fmt.Printf("Features linked: %d\n", linkedCount)
	fmt.Printf("Dependencies wired: %d\n", depsWired)
	fmt.Printf("Status: finalized (v%d)\n", version)

	if len(unmatched) > 0 {
		fmt.Printf("\nNote: %d feature(s) under track not matched to any slice:\n", len(unmatched))
		for _, id := range unmatched {
			fmt.Printf("  %s\n", id)
		}
	}

	if linkedCount < len(approvedTitles) {
		fmt.Printf("\nNote: %d approved slice(s) had no matching feature under the track:\n",
			len(approvedTitles)-linkedCount)
		for title := range approvedTitles {
			if _, found := titleToFeat[title]; !found {
				fmt.Printf("  %s\n", approvedTitles[title].Title)
			}
		}
	}
}
