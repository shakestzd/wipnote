package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planSliceRow is one slice in `wipnote plan slices` output: the fields a
// reviewer needs to see where each slice stands.
type planSliceRow struct {
	Num             int    `json:"num"`
	Title           string `json:"title"`
	Complexity      string `json:"complexity"`
	Effort          string `json:"effort,omitempty"`
	Risk            string `json:"risk,omitempty"`
	ApprovalStatus  string `json:"approval_status"`
	ExecutionStatus string `json:"execution_status,omitempty"`
	FeatureID       string `json:"feature_id,omitempty"`
}

type planSlicesResult struct {
	PlanID string         `json:"plan_id"`
	Title  string         `json:"title"`
	Status string         `json:"status"`
	Slices []planSliceRow `json:"slices"`
}

// planSlicesCmd prints a plan's slices and their review state as JSON. The
// wipnote-plan-review mod polls it to draw its side pane.
func planSlicesCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "slices <plan-id>",
		Short: "Print a plan's slices and their approval state as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runPlanSlices(os.Stdout, wipnoteDir, args[0])
		},
	}
}

func runPlanSlices(w io.Writer, wipnoteDir, planID string) error {
	plan, err := planyaml.Load(filepath.Join(wipnoteDir, "plans", planID+".yaml"))
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	result := planSlicesResult{
		PlanID: planID,
		Title:  plan.Meta.Title,
		Status: plan.Meta.Status,
		Slices: make([]planSliceRow, 0, len(plan.Slices)),
	}
	for _, s := range plan.Slices {
		result.Slices = append(result.Slices, planSliceRow{
			Num:             s.Num,
			Title:           s.Title,
			Complexity:      sliceComplexity(s.Complexity),
			Effort:          s.Effort,
			Risk:            s.Risk,
			ApprovalStatus:  sliceApprovalStatus(s),
			ExecutionStatus: s.ExecutionStatus,
			FeatureID:       s.FeatureID,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

// sliceApprovalStatus reads approval_status, falling back to the legacy
// approved flag for plans written before approval_status existed.
func sliceApprovalStatus(s planyaml.PlanSlice) string {
	if s.ApprovalStatus != "" {
		return s.ApprovalStatus
	}
	if s.Approved {
		return "approved"
	}
	return "pending"
}

// sliceComplexity reports an unset complexity as "standard", as the validator does.
func sliceComplexity(c string) string {
	if c == "" {
		return "standard"
	}
	return c
}
