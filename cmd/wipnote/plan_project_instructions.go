package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

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
			"and what needs the owner's go-ahead. Trimmed items are named at the end.",
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
	_, err = io.WriteString(w, projectbrief.Build(plan).Text)
	return err
}
