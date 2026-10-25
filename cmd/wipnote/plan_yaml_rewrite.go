package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planRewriteYAMLCmd replaces an existing YAML plan file with validated new content.
func planRewriteYAMLCmd() *cobra.Command {
	var filePath string

	cmd := &cobra.Command{
		Use:   "rewrite-yaml <plan-id>",
		Short: "Replace a YAML plan file with validated new content",
		Long: `Replace an existing YAML plan file with new content from a file or stdin.

The new content is validated before writing. The meta.id in the new content
must match the existing plan ID to prevent accidental overwrites.

Example:
  wipnote plan rewrite-yaml plan-abc12345 --file /tmp/updated-plan.yaml
  cat /tmp/updated-plan.yaml | wipnote plan rewrite-yaml plan-abc12345`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runRewriteYAML(args[0], filePath)
		},
	}
	cmd.Flags().StringVar(&filePath, "file", "", "path to YAML file (reads stdin if empty)")
	return cmd
}

func runRewriteYAML(planID, filePath string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole read-existing→build-new→save window
	// (defect 4, feat-fc3cc9e0): rewrite-yaml reads the CURRENT on-disk plan
	// below (to preserve meta.track_id and feedback across the overwrite)
	// and then saves an entirely new document — the same lost-update hazard
	// as a load→mutate→save cycle, just with the "mutation" being a
	// wholesale replacement. Mirrors storePlanFeedbackEntry.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	// Confirm the plan exists.
	if _, err := os.Stat(planPath); err != nil {
		return fmt.Errorf("plan %q not found at %s", planID, planPath)
	}

	// Read the new YAML content.
	var yamlBytes []byte
	if filePath != "" {
		yamlBytes, err = os.ReadFile(filePath)
		if err != nil {
			return fmt.Errorf("read file %q: %w", filePath, err)
		}
	} else {
		yamlBytes, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}

	// Parse with planyaml.Load equivalent — unmarshal into PlanYAML.
	newPlan, err := planyaml.LoadBytes(yamlBytes)
	if err != nil {
		return fmt.Errorf("parse YAML: %w", err)
	}

	// Validate.
	errs := planyaml.Validate(newPlan)
	if len(errs) > 0 {
		for _, e := range errs {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("%d validation error(s)", len(errs))
	}

	// Enforce meta.id matches the canonical plan ID (derived from HTML filename).
	if newPlan.Meta.ID != planID {
		fmt.Fprintf(os.Stderr, "warning: meta.id %q overwritten to match plan ID %q\n", newPlan.Meta.ID, planID)
		newPlan.Meta.ID = planID
	}

	// Preserve the existing plan-track linkage across rewrites (bug-fddf5820,
	// finding 15). meta.track_id is structural — a hand- or LLM-authored rewrite
	// of the plan body almost always omits it, which silently severed the plan
	// from its track and caused finalize to create a brand-new track instead of
	// attaching to the existing one (plan-edeb2163 → lost trk-23232c8d).
	if existing, lerr := planyaml.Load(planPath); lerr == nil {
		if preserved := preserveTrackLinkage(existing, newPlan); preserved != "" {
			fmt.Fprintf(os.Stderr, "note: preserved meta.track_id %q from the existing plan (rewrite did not specify one)\n", preserved)
		}
		if newPlan.Feedback == nil {
			newPlan.Feedback = existing.Feedback
		}
	}

	// Apply accepted amendments from plan_feedback before saving.
	amendmentsApplied, err := applyAcceptedAmendments(wipnoteDir, planID, newPlan)
	if err != nil {
		return fmt.Errorf("apply amendments: %w", err)
	}

	// Write validated content.
	if err := planyaml.SaveLocked(planPath, newPlan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): rewrite — %d slices, %d questions",
		planID, len(newPlan.Slices), len(newPlan.Questions))); err != nil {
		return fmt.Errorf("autocommit rewrite: %w", err)
	}

	if amendmentsApplied > 0 {
		fmt.Printf("Amendments applied: %d\n", amendmentsApplied)
	}
	fmt.Printf("Plan %s rewritten: %d slices, %d questions\n", planID, len(newPlan.Slices), len(newPlan.Questions))
	return nil
}

// preserveTrackLinkage carries the existing plan's meta.track_id forward onto
// newPlan when the rewrite did not specify one (bug-fddf5820, finding 15).
// An explicit non-empty track_id in newPlan is respected (allows intentional
// re-targeting). Returns the track_id that was preserved, or "" when no
// preservation happened. Pure (no I/O) so it is unit-testable.
func preserveTrackLinkage(existing, newPlan *planyaml.PlanYAML) string {
	if existing == nil || newPlan == nil {
		return ""
	}
	if strings.TrimSpace(newPlan.Meta.TrackID) != "" {
		return ""
	}
	existingTrack := strings.TrimSpace(existing.Meta.TrackID)
	if existingTrack == "" {
		return ""
	}
	newPlan.Meta.TrackID = existing.Meta.TrackID
	return existingTrack
}

// amendmentValue is the JSON payload stored in plan_feedback.value for amendments.
type amendmentValue struct {
	SliceNum  int    `json:"slice_num"`
	Operation string `json:"operation"`
	Field     string `json:"field"`
	Content   string `json:"content"`
}

// applyAcceptedAmendments applies accepted canonical amendments and marks them
// applied in the in-memory YAML feedback state.
func applyAcceptedAmendments(wipnoteDir, planID string, plan *planyaml.PlanYAML) (int, error) {
	type pendingAmendment struct {
		index      int
		questionID string
		amendment  amendmentValue
	}
	var pending []pendingAmendment
	if plan == nil || plan.Feedback == nil {
		return 0, nil
	}
	for i, entry := range plan.Feedback.Entries {
		if entry.Section != "amendment" || entry.Action != "accepted" {
			continue
		}
		var a amendmentValue
		if err := json.Unmarshal([]byte(entry.Value), &a); err != nil {
			return 0, fmt.Errorf("parse amendment JSON (question_id=%s): %w", entry.QuestionID, err)
		}
		pending = append(pending, pendingAmendment{index: i, questionID: entry.QuestionID, amendment: a})
	}

	if len(pending) == 0 {
		return 0, nil
	}

	// Apply each amendment to the in-memory plan.
	applied := 0
	for _, p := range pending {
		a := p.amendment
		// Find target slice by slice_num (1-indexed).
		sliceIdx := -1
		for i, s := range plan.Slices {
			if s.Num == a.SliceNum {
				sliceIdx = i
				break
			}
		}
		if sliceIdx < 0 {
			// Slice not found — skip but still mark applied so we don't retry forever.
			fmt.Fprintf(os.Stderr, "amendment %s: slice_num %d not found, skipping\n", p.questionID, a.SliceNum)
		} else {
			switch a.Operation {
			case "add":
				switch a.Field {
				case "done_when":
					plan.Slices[sliceIdx].DoneWhen = append(plan.Slices[sliceIdx].DoneWhen, a.Content)
				case "files":
					plan.Slices[sliceIdx].Files = append(plan.Slices[sliceIdx].Files, a.Content)
				}
			case "remove":
				switch a.Field {
				case "done_when":
					plan.Slices[sliceIdx].DoneWhen = removeString(plan.Slices[sliceIdx].DoneWhen, a.Content)
				case "files":
					plan.Slices[sliceIdx].Files = removeString(plan.Slices[sliceIdx].Files, a.Content)
				}
			case "set":
				switch a.Field {
				case "title":
					plan.Slices[sliceIdx].Title = a.Content
				case "what":
					plan.Slices[sliceIdx].What = a.Content
				case "why":
					plan.Slices[sliceIdx].Why = a.Content
				case "effort":
					plan.Slices[sliceIdx].Effort = a.Content
				case "risk":
					plan.Slices[sliceIdx].Risk = a.Content
				}
			}
		}

		// Mark as applied regardless of whether the slice was found.
		plan.Feedback.Entries[p.index].Action = "applied"
		plan.Feedback.Entries[p.index].UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		applied++
	}

	return applied, nil
}

// removeString returns a copy of ss with all occurrences of s removed.
func removeString(ss []string, s string) []string {
	var result []string
	for _, v := range ss {
		if v != s {
			result = append(result, v)
		}
	}
	return result
}
