package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planAmendment represents a structured amendment directive from the chat review system.
type planAmendment struct {
	SliceNum  int    `json:"slice_num"`
	Field     string `json:"field"`
	Operation string `json:"operation"`
	Content   string `json:"content"`
}

// planFinalizeYAMLCmd creates track + features from approved slices in a YAML plan.
func planFinalizeYAMLCmd() *cobra.Command {
	var regenerate bool
	cmd := &cobra.Command{
		Use:   "finalize-yaml <plan-id>",
		Short: "Create track and features from approved YAML plan slices (review flow)",
		Long: `Read a YAML plan and the slice approvals recorded in it, create a track
and features for approved slices, wire dependency edges. Updates YAML status
to finalized.

This is the review workflow: only slices approved (for example with
'plan approve-slice') get promoted, and the track is created from scratch
when one does not yet exist.

For the simpler hierarchy-only flow that requires an existing track and
promotes every slice unconditionally, use 'plan finalize' instead.

Re-finalizing after 'plan reopen' is additive: slices that already carry a
feature_id keep their feature (like promote-slice), and only slices without
one get a new feature. Pass --regenerate to mint fresh features for every
approved slice instead (the old features are left on the track, not deleted).

Example:
  wipnote plan finalize-yaml plan-a1b2c3d4`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runFinalizeYAML(args[0], finalizeYAMLOptions{Regenerate: regenerate})
		},
	}
	cmd.Flags().BoolVar(&regenerate, "regenerate", false,
		"create NEW features for every approved slice even if it already has a feature_id (old features are orphaned, not deleted)")
	return cmd
}

func runFinalizeYAML(planID string, opts finalizeYAMLOptions) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	return finalizeYAML(wipnoteDir, planID, opts)
}

// finalizeYAML is the testable inner implementation of runFinalizeYAML.
// It takes an explicit wipnoteDir rather than resolving it from the environment.
func finalizeYAML(wipnoteDir, planID string, opts finalizeYAMLOptions) error {
	featIDs, _, err := finalizeYAMLCanonicalOpts(wipnoteDir, planID, opts)
	if err != nil {
		return err
	}

	// Print summary via CLI path (load plan again for summary context).
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")
	plan, loadErr := planyaml.Load(planPath)
	if loadErr != nil {
		return nil // summary is optional; creation already succeeded
	}
	answers := loadPlanAnswersFromPlan(plan)
	var rejectedTitles []string
	approvals := loadPlanApprovalsFromPlan(plan)
	for _, s := range plan.Slices {
		if !approvals[fmt.Sprintf("slice-%d", s.Num)] {
			rejectedTitles = append(rejectedTitles, s.Title)
		}
	}
	var track *models.Node
	if plan.Meta.TrackID != "" {
		p, pErr := workitem.Open(wipnoteDir, agentForClaim())
		if pErr == nil {
			defer p.Close()
			track, _ = p.Tracks.Get(plan.Meta.TrackID)
		}
	}
	printFinalizeYAMLSummary(plan, track, answers, featIDs, rejectedTitles)
	return nil
}

func finalizeYAMLCanonicalOpts(wipnoteDir, planID string, opts finalizeYAMLOptions) (createdIDs []string, failures []finalizeFailure, err error) {
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0): finalize reads the plan, creates features and wires
	// edges in between, then saves the whole document back via
	// saveFinalizedPlan. A bare Load-then-Save let two concurrent writers
	// interleave and the second whole-document save silently clobber the
	// first. Mirrors storePlanFeedbackEntry.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, err := planyaml.Load(planPath)
	if err != nil {
		return nil, nil, fmt.Errorf("load plan: %w", err)
	}
	approvals := loadPlanApprovalsFromPlan(plan)
	answers := loadPlanAnswersFromPlan(plan)
	applyAmendments(plan, loadPlanAmendmentsFromPlan(plan))

	p, pErr := workitem.Open(wipnoteDir, agentForClaim())
	if pErr != nil {
		return nil, nil, fmt.Errorf("open project: %w", pErr)
	}
	defer p.Close()

	if plan.Meta.Status == "finalized" {
		existing := existingFeatureIDsFromPlan(plan)
		if len(existing) == 0 {
			existing = findFeaturesForPlanCanonical(p, planID)
		}
		if len(existing) > 0 {
			return existing, nil, nil
		}
		plan.Meta.Status = ""
	}

	track, err := ensurePlanTrack(p, plan)
	if err != nil {
		return nil, nil, err
	}
	priorIDs := sliceFeatureIDsByNum(plan.Slices)
	numToFeat, failures := createApprovedPlanFeatures(p, plan, track, approvals, answers, opts.Regenerate)
	if opts.Regenerate {
		warnRegeneratedFeatures(priorIDs, numToFeat)
	}
	linkPlanFinalizationEdges(p, planID, track.ID, plan.Slices, numToFeat)
	if saveErr := saveFinalizedPlan(planPath, plan, track.ID, approvals, numToFeat); saveErr != nil {
		return nil, failures, saveErr
	}
	if commitErr := commitPlanChange(planPath, fmt.Sprintf("plan(%s): finalize — %d slices approved", planID, len(numToFeat))); commitErr != nil {
		return nil, failures, fmt.Errorf("autocommit finalize-yaml: %w", commitErr)
	}
	return createdFeatureIDs(plan.Slices, numToFeat), failures, nil
}

type finalizedFeature struct {
	id    string
	title string
}

// finalizeFailure records a slice number and error for feature creation failures.
type finalizeFailure struct {
	SliceNum int    `json:"slice_num"`
	Title    string `json:"title"`
	Error    string `json:"error"`
}

func ensurePlanTrack(p *workitem.Project, plan *planyaml.PlanYAML) (*models.Node, error) {
	if plan.Meta.TrackID != "" {
		existing, err := p.Tracks.Get(plan.Meta.TrackID)
		if err == nil && existing != nil {
			return existing, nil
		}
	}
	track, err := p.Tracks.Create(plan.Meta.Title)
	if err != nil {
		return nil, fmt.Errorf("create track: %w", err)
	}
	return track, nil
}

// createApprovedPlanFeatures resolves a feature for every approved slice.
// A slice whose feature_id still points at an existing feature keeps it
// (promote-slice Rule 3, GH-#116) unless regenerate is set; only slices
// without a live feature get one created.
func createApprovedPlanFeatures(
	p *workitem.Project,
	plan *planyaml.PlanYAML,
	track *models.Node,
	approvals map[string]bool,
	answers map[string]string,
	regenerate bool,
) (map[int]finalizedFeature, []finalizeFailure) {
	numToFeat := map[int]finalizedFeature{}
	var failures []finalizeFailure
	for _, s := range plan.Slices {
		if !approvals[fmt.Sprintf("slice-%d", s.Num)] {
			continue
		}
		if existing := reusableSliceFeature(p, s.FeatureID, regenerate); existing != nil {
			numToFeat[s.Num] = *existing
			continue
		}
		content := buildFeatureContent(s.What, plan.Questions, answers)
		feat, err := p.Features.Create(s.Title, workitem.FeatWithTrack(track.ID), workitem.FeatWithContent(content))
		if err != nil {
			failures = append(failures, finalizeFailure{SliceNum: s.Num, Title: s.Title, Error: err.Error()})
			continue
		}
		numToFeat[s.Num] = finalizedFeature{id: feat.ID, title: feat.Title}
		p.Features.AddEdge(feat.ID, models.Edge{
			TargetID:     plan.Meta.ID,
			Relationship: models.RelPlannedIn,
			Title:        plan.Meta.ID,
			Since:        time.Now().UTC(),
		})
		wireTrackEdges(p, feat.ID, track.ID, feat.Title) //nolint:errcheck
	}
	return numToFeat, failures
}

func linkPlanFinalizationEdges(
	p *workitem.Project,
	planID, trackID string,
	slices []planyaml.PlanSlice,
	numToFeat map[int]finalizedFeature,
) {
	// Idempotent: reused features (finalize after reopen) already carry these
	// edges, and Node.AddEdge appends unconditionally.
	addPlanEdgeOnce(p, planID, models.Edge{TargetID: trackID, Relationship: models.RelImplementedIn, Title: trackID, Since: time.Now().UTC()})
	for _, s := range slices {
		cf, ok := numToFeat[s.Num]
		if !ok {
			continue
		}
		for _, depNum := range s.Deps {
			if depCF, ok := numToFeat[depNum]; ok {
				addFeatureEdgeOnce(p, cf.id, models.Edge{TargetID: depCF.id, Relationship: "blocked_by"})
			}
		}
	}
}

// saveFinalizedPlan's only caller, finalizeYAMLCanonicalOpts, already holds
// planyaml.LockPlanForWrite(planPath) across its whole load→mutate→save
// window (defect 4, feat-fc3cc9e0) — SaveLocked (not Save) avoids re-entering
// that non-reentrant mutex.
func saveFinalizedPlan(
	planPath string,
	plan *planyaml.PlanYAML,
	trackID string,
	approvals map[string]bool,
	numToFeat map[int]finalizedFeature,
) error {
	plan.Meta.Status = "finalized"
	plan.Meta.TrackID = trackID
	for i := range plan.Slices {
		num := plan.Slices[i].Num
		plan.Slices[i].Approved = approvals[fmt.Sprintf("slice-%d", num)]
		if cf, ok := numToFeat[num]; ok {
			plan.Slices[i].FeatureID = cf.id
			plan.Slices[i].ExecutionStatus = "promoted"
		}
	}
	if err := storeFinalizedFeedbackMarker(plan); err != nil {
		return err
	}
	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}
	return nil
}

func storeFinalizedFeedbackMarker(plan *planyaml.PlanYAML) error {
	upsertPlanFeedback(plan, planyaml.PlanFeedbackEntry{Section: "meta", Action: "finalize", Value: "true"})
	mirrorPlanFeedback(plan, planyaml.PlanFeedbackEntry{Section: "meta", Action: "finalize", Value: "true"})
	return nil
}

func createdFeatureIDs(slices []planyaml.PlanSlice, numToFeat map[int]finalizedFeature) []string {
	var ids []string
	for _, s := range slices {
		if cf, ok := numToFeat[s.Num]; ok {
			ids = append(ids, cf.id)
		}
	}
	return ids
}

func existingFeatureIDsFromPlan(plan *planyaml.PlanYAML) []string {
	var ids []string
	for _, s := range plan.Slices {
		if s.FeatureID != "" {
			ids = append(ids, s.FeatureID)
		}
	}
	return ids
}

func loadPlanApprovalsFromPlan(plan *planyaml.PlanYAML) map[string]bool {
	approvals := map[string]bool{}
	if plan == nil {
		return approvals
	}
	for _, s := range plan.Slices {
		approvals[fmt.Sprintf("slice-%d", s.Num)] = s.ApprovalStatus == "approved" || s.Approved
	}
	for _, e := range feedbackYAMLEntries(plan) {
		if e.Action == "approve" {
			approvals[e.Section] = approvalStatusFromValue(e.Value) == "approved"
		}
	}
	return approvals
}

func loadPlanAnswersFromPlan(plan *planyaml.PlanYAML) map[string]string {
	answers := map[string]string{}
	if plan == nil {
		return answers
	}
	for _, q := range plan.Questions {
		if q.Answer != nil {
			answers[q.ID] = *q.Answer
		}
	}
	for _, e := range feedbackYAMLEntries(plan) {
		if e.Action == "answer" && e.QuestionID != "" {
			answers[e.QuestionID] = e.Value
		}
	}
	return answers
}

func loadPlanAmendmentsFromPlan(plan *planyaml.PlanYAML) []planAmendment {
	var amendments []planAmendment
	for _, e := range feedbackYAMLEntries(plan) {
		if e.Section != "amendment" || e.Action != "accepted" {
			continue
		}
		var a planAmendment
		if json.Unmarshal([]byte(e.Value), &a) == nil {
			amendments = append(amendments, a)
		}
	}
	return amendments
}

func findFeaturesForPlanCanonical(p *workitem.Project, planID string) []string {
	features, err := p.Features.List()
	if err != nil {
		return nil
	}
	var ids []string
	for _, feat := range features {
		for _, edge := range feat.Edges[string(models.RelPlannedIn)] {
			if edge.TargetID == planID {
				ids = append(ids, feat.ID)
				break
			}
		}
	}
	return ids
}
