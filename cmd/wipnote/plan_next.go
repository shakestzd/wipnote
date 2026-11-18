package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planNextState is the plan's position in the lifecycle, derived from the plan
// file alone. feat-bcc2297f: the step rules reached agents only if a command
// printed them, so `plan next` names the step and prints that step's rules.
type planNextState struct {
	Step string   // Draft | Validate | Critique | Review | Decisions | Promote | Spec | Close
	Nums []string // slice numbers or feature ids the step applies to
	Errs []string // validation errors, for the Validate step
}

func planNextCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "next [plan-id]",
		Short: "Print the next plan step and its rules, or the triage rule with no id",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			id := ""
			if len(args) == 1 {
				id = args[0]
			}
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runPlanNext(os.Stdout, wipnoteDir, id)
		},
	}
}

func runPlanNext(w io.Writer, wipnoteDir, planID string) error {
	if planID == "" {
		_, err := io.WriteString(w, planNextTriage)
		return err
	}
	plan, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", planID+".yaml"))
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	st := derivePlanNext(wipnoteDir, plan)
	fmt.Fprintf(w, "Next step: %s (%s)\n", st.Step, planID)
	if len(st.Errs) > 0 {
		fmt.Fprintln(w, "Errors: "+planNextErrs(st.Errs))
	}
	if len(st.Nums) > 0 {
		fmt.Fprintln(w, "Applies to: "+strings.Join(st.Nums, ", "))
	}
	_, err = io.WriteString(w, strings.ReplaceAll(planNextRules[st.Step], "<plan-id>", planID))
	return err
}

func planNextErrs(errs []string) string {
	const maxShown = 3
	if len(errs) <= maxShown {
		return strings.Join(errs, "; ")
	}
	return fmt.Sprintf("%s; and %d more", strings.Join(errs[:maxShown], "; "), len(errs)-maxShown)
}

// derivePlanNext picks the first step whose condition is unmet.
func derivePlanNext(wipnoteDir string, plan *planyaml.PlanYAML) planNextState {
	if len(plan.Slices) == 0 {
		return planNextState{Step: "Draft"}
	}
	if errs := planyaml.Validate(plan); len(errs) > 0 {
		return planNextState{Step: "Validate", Errs: errs}
	}
	if planNeedsCritique(plan) && plan.Critique == nil {
		return planNextState{Step: "Critique"}
	}
	if nums := slicesWhere(plan, func(s planyaml.PlanSlice) bool { return !sliceDecided(s) }); len(nums) > 0 {
		return planNextState{Step: "Review", Nums: nums}
	}
	live := func(s planyaml.PlanSlice) bool { return sliceApproved(s) && s.FeatureID == "" }
	if nums := slicesWhere(plan, func(s planyaml.PlanSlice) bool {
		return live(s) && strings.TrimSpace(s.DecisionsNotes) == ""
	}); len(nums) > 0 {
		return planNextState{Step: "Decisions", Nums: nums}
	}
	// promote-slice refuses a slice whose deps are not done, so only a slice
	// with every dep finished is offered. The rest wait under Spec or Close.
	ready := func(s planyaml.PlanSlice) bool { return live(s) && depsFinished(plan, s) }
	if nums := slicesWhere(plan, ready); len(nums) > 0 {
		return planNextState{Step: "Promote", Nums: nums}
	}
	var noSpec []string
	for _, s := range plan.Slices {
		if s.FeatureID != "" && !sliceFinished(s) && !featureHasSpec(wipnoteDir, s.FeatureID) {
			noSpec = append(noSpec, s.FeatureID)
		}
	}
	if len(noSpec) > 0 {
		return planNextState{Step: "Spec", Nums: noSpec}
	}
	open := slicesWhere(plan, func(s planyaml.PlanSlice) bool { return sliceApproved(s) && !sliceFinished(s) })
	return planNextState{Step: "Close", Nums: open}
}

// planNeedsCritique is true for 3+ slices or any complex slice.
func planNeedsCritique(plan *planyaml.PlanYAML) bool {
	if len(plan.Slices) >= 3 {
		return true
	}
	for _, s := range plan.Slices {
		if s.Complexity == "complex" {
			return true
		}
	}
	return false
}

func sliceApproved(s planyaml.PlanSlice) bool {
	return s.ApprovalStatus == "approved" || (s.ApprovalStatus == "" && s.Approved)
}

// sliceDecided is true once the owner approved or rejected the slice.
func sliceDecided(s planyaml.PlanSlice) bool {
	return sliceApproved(s) || s.ApprovalStatus == "rejected"
}

func sliceFinished(s planyaml.PlanSlice) bool {
	return s.ExecutionStatus == "done" || s.ExecutionStatus == "superseded"
}

// depsFinished is true when every slice that s depends on is done or superseded.
func depsFinished(plan *planyaml.PlanYAML, s planyaml.PlanSlice) bool {
	for _, d := range s.Deps {
		for _, o := range plan.Slices {
			if o.Num == d && !sliceFinished(o) {
				return false
			}
		}
	}
	return true
}

func slicesWhere(plan *planyaml.PlanYAML, pred func(planyaml.PlanSlice) bool) []string {
	var out []string
	for _, s := range plan.Slices {
		if pred(s) {
			out = append(out, fmt.Sprint(s.Num))
		}
	}
	return out
}

// featureHasSpec reads the feature page the way the completion gate does.
func featureHasSpec(wipnoteDir, featureID string) bool {
	raw, err := os.ReadFile(filepath.Join(wipnoteDir, "features", featureID+".html"))
	if err != nil {
		return false
	}
	spec := extractSpecSection(string(raw))
	return spec != "" && len(parseCriteria(unwrapPreBlock(spec))) > 0
}
