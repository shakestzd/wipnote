package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/htmlparse"
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

	// Written slice details, only with --details.
	What     string   `json:"what,omitempty"`
	Why      string   `json:"why,omitempty"`
	Files    []string `json:"files,omitempty"`
	Deps     []int    `json:"deps,omitempty"`
	DoneWhen []string `json:"done_when,omitempty"`
	Tests    string   `json:"tests,omitempty"`
}

type planSlicesResult struct {
	PlanID string         `json:"plan_id"`
	Title  string         `json:"title"`
	Status string         `json:"status"`
	Slices []planSliceRow `json:"slices"`
	// Source is "yaml", or "html" for a legacy plan that has no YAML and so no slices.
	Source string `json:"source"`
}

// planSlicesCmd prints a plan's slices and their review state as JSON. The
// wipnote-plan-review mod polls it to draw its side pane.
func planSlicesCmd() *cobra.Command {
	var details bool
	cmd := &cobra.Command{
		Use:   "slices <plan-id>",
		Short: "Print a plan's slices and their approval state as JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runPlanSlicesWith(os.Stdout, wipnoteDir, args[0], details)
		},
	}
	cmd.Flags().BoolVar(&details, "details", false,
		"Include each slice's what, why, files, deps, done_when and tests")
	return cmd
}

func runPlanSlices(w io.Writer, wipnoteDir, planID string) error {
	return runPlanSlicesWith(w, wipnoteDir, planID, false)
}

func runPlanSlicesWith(w io.Writer, wipnoteDir, planID string, details bool) error {
	yamlPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")
	plan, err := planyaml.Load(yamlPath)
	if err != nil {
		if _, statErr := os.Stat(yamlPath); !os.IsNotExist(statErr) {
			return fmt.Errorf("load plan: %w", err)
		}
		legacy, legacyErr := legacyPlanSlices(wipnoteDir, planID)
		if legacyErr != nil {
			return fmt.Errorf("load plan: %w", err)
		}
		return writePlanSlices(w, legacy)
	}
	result := planSlicesResult{
		PlanID: planID,
		Title:  plan.Meta.Title,
		Status: plan.Meta.Status,
		Slices: make([]planSliceRow, 0, len(plan.Slices)),
		Source: "yaml",
	}
	for _, s := range plan.Slices {
		row := planSliceRow{
			Num:             s.Num,
			Title:           s.Title,
			Complexity:      sliceComplexity(s.Complexity),
			Effort:          s.Effort,
			Risk:            s.Risk,
			ApprovalStatus:  sliceApprovalStatus(s),
			ExecutionStatus: s.ExecutionStatus,
			FeatureID:       s.FeatureID,
		}
		if details {
			row.What, row.Why, row.Files, row.Deps, row.DoneWhen, row.Tests = s.What, s.Why, s.Files, s.Deps, s.DoneWhen, s.Tests
		}
		result.Slices = append(result.Slices, row)
	}
	return writePlanSlices(w, result)
}

// legacyPlanSlices describes a plan written before plan YAML existed: it has
// only an HTML file and no slices.
func legacyPlanSlices(wipnoteDir, planID string) (planSlicesResult, error) {
	node, err := htmlparse.ParseFile(filepath.Join(wipnoteDir, "plans", planID+".html"))
	if err != nil {
		return planSlicesResult{}, err
	}
	return planSlicesResult{
		PlanID: planID,
		Title:  strings.TrimPrefix(node.Title, "Plan: "),
		Status: string(node.Status),
		Slices: []planSliceRow{},
		Source: "html",
	}, nil
}

func writePlanSlices(w io.Writer, result planSlicesResult) error {
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
