package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/shakestzd/wipnote/plan/projectbrief"
	"github.com/spf13/cobra"
)

// planProjectInstructionsCmd prints a plan as the brief a Claude project takes
// in Project settings > Memory > Project instructions. The plan YAML stays
// canonical; the printed text is a copy that nothing reads back.
func planProjectInstructionsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "project-instructions <plan-id>",
		Short: "Print a plan as Claude project instructions (at most 16,000 characters)",
		Long: "Print a project-instructions brief derived from the plan YAML: what the project is for, " +
			"where the work happens, how a thread checks its work, what to do when something is missing, " +
			"and what needs the owner's go-ahead. Done and superseded slices (by the slice's execution_status " +
			"or its linked work item's status) are left out; trimmed items are named at the end.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runPlanProjectInstructions(os.Stdout, wipnoteDir, args[0])
		},
	}
}

func runPlanProjectInstructions(w io.Writer, wipnoteDir, planID string) error {
	if planID == "" || strings.ContainsAny(planID, `/\`) || strings.Contains(planID, "..") {
		return fmt.Errorf("invalid plan id %q", planID)
	}
	plan, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", planID+".yaml"))
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	opts := projectbrief.Options{FeatureStatus: workItemStatusReader(wipnoteDir)}
	_, err = io.WriteString(w, projectbrief.BuildWith(plan, opts).Text)
	return err
}

// workItemStatusReader returns a lookup of a linked work item's status from
// its canonical HTML under .wipnote/<kind>s/. An unreadable or missing item
// reads as "" so its slice stays open.
func workItemStatusReader(wipnoteDir string) func(string) string {
	return func(id string) string {
		if strings.ContainsAny(id, `/\`) || strings.Contains(id, "..") {
			return ""
		}
		switch kind := workitem.KindForID(id); kind {
		case "feature", "bug", "spike":
			node, err := htmlparse.ParseFile(filepath.Join(wipnoteDir, kind+"s", id+".html"))
			if err != nil {
				return ""
			}
			return string(node.Status)
		}
		return ""
	}
}
