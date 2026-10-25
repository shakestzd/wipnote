package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

func planListYAMLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-yaml",
		Short: "List all YAML plans sorted by created_at descending",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runListYAML()
		},
	}
}

func runListYAML() error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	plansDir := filepath.Join(wipnoteDir, "plans")
	entries, err := os.ReadDir(plansDir)
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Println("No YAML plans found.")
			return nil
		}
		return fmt.Errorf("read plans dir: %w", err)
	}

	type planInfo struct {
		id        string
		status    string
		slices    int
		createdAt string
		title     string
	}

	var plans []planInfo

	// Scan for YAML plan files
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".yaml") {
			continue
		}

		planPath := filepath.Join(plansDir, entry.Name())

		plan, err := planyaml.Load(planPath)
		if err != nil {
			// Skip files that fail to parse
			continue
		}

		plans = append(plans, planInfo{
			id:        plan.Meta.ID,
			status:    plan.Meta.Status,
			slices:    len(plan.Slices),
			createdAt: plan.Meta.CreatedAt,
			title:     plan.Meta.Title,
		})
	}

	// Sort by created_at descending (newest first)
	sort.Slice(plans, func(i, j int) bool {
		if plans[i].createdAt != plans[j].createdAt {
			return plans[i].createdAt > plans[j].createdAt
		}
		return plans[i].id < plans[j].id
	})

	if len(plans) == 0 {
		fmt.Println("No YAML plans found.")
		return nil
	}

	fmt.Printf("%-18s  %-11s  %-7s  %-12s  %s\n", "ID", "STATUS", "SLICES", "CREATED", "TITLE")
	fmt.Println(strings.Repeat("-", 100))
	for _, p := range plans {
		title := p.title
		if len(title) > 50 {
			title = title[:47] + "..."
		}
		fmt.Printf("%-18s  %-11s  %-7d  %-12s  %s\n",
			p.id, p.status, p.slices, p.createdAt, title)
	}
	fmt.Printf("\n%d YAML plan(s)\n", len(plans))
	return nil
}

func planAddQuestionYAMLCmd() *cobra.Command {
	var description, recommended, options string
	cmd := &cobra.Command{
		Use:   "add-question-yaml <plan-id> <question-text>",
		Short: "Add a question with description and recommended option to a YAML plan",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			return runAddQuestionYAML(args[0], args[1], description, recommended, options)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "context paragraph (required)")
	cmd.Flags().StringVar(&recommended, "recommended", "", "recommended option key")
	cmd.Flags().StringVar(&options, "options", "", "comma-separated key:label pairs (min 2)")
	return cmd
}

func runAddQuestionYAML(planID, text, description, recommended, optionsStr string) error {
	if description == "" {
		return fmt.Errorf("--description is required")
	}
	opts := parseQuestionOptions(optionsStr)
	if len(opts) < 2 {
		return fmt.Errorf("--options must have at least 2 entries (got %d)", len(opts))
	}
	if recommended != "" {
		found := false
		for _, o := range opts {
			if o.Key == recommended {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("--recommended %q not found in options", recommended)
		}
	}
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0) — see storePlanFeedbackEntry for the canonical pattern
	// this mirrors.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	qid := "q-" + kebabCase(text, 40)
	plan.Questions = append(plan.Questions, planyaml.PlanQuestion{
		ID: qid, Text: text, Description: description,
		Recommended: recommended, Options: opts, Answer: nil,
	})
	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	truncated := text
	if len(truncated) > 50 {
		truncated = truncated[:50]
	}
	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): add question — %s", planID, truncated)); err != nil {
		return fmt.Errorf("autocommit add-question: %w", err)
	}

	fmt.Printf("Added question: %s (%d options)\n", qid, len(opts))
	return nil
}

func parseQuestionOptions(s string) []planyaml.QuestionOption {
	if s == "" {
		return nil
	}
	var opts []planyaml.QuestionOption
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		key, label, found := strings.Cut(part, ":")
		if !found {
			continue
		}
		opts = append(opts, planyaml.QuestionOption{
			Key: strings.TrimSpace(key), Label: strings.TrimSpace(label),
		})
	}
	return opts
}

func kebabCase(s string, maxLen int) string {
	s = strings.ToLower(s)
	s = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			return r
		}
		return '-'
	}, s)
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	s = strings.Trim(s, "-")
	if len(s) > maxLen {
		s = s[:maxLen]
		s = strings.TrimRight(s, "-")
	}
	return s
}

func planSetCritiqueYAMLCmd() *cobra.Command {
	var data string
	cmd := &cobra.Command{
		Use:   "set-critique-yaml <plan-id>",
		Short: "Write AI critique data to a YAML plan (from --data or stdin)",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSetCritiqueYAML(args[0], data)
		},
	}
	cmd.Flags().StringVar(&data, "data", "", "critique JSON (reads stdin if empty)")
	return cmd
}

func runSetCritiqueYAML(planID, dataStr string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0) — see storePlanFeedbackEntry for the canonical pattern
	// this mirrors.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	var jsonBytes []byte
	if dataStr != "" {
		jsonBytes = []byte(dataStr)
	} else {
		jsonBytes, err = io.ReadAll(os.Stdin)
		if err != nil {
			return fmt.Errorf("read stdin: %w", err)
		}
	}
	var critique planyaml.PlanCritique
	if err := json.Unmarshal(jsonBytes, &critique); err != nil {
		return fmt.Errorf("parse critique JSON: %w", err)
	}
	plan.Critique = &critique
	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): set critique — %d assumptions, %d risks",
		planID, len(critique.Assumptions), len(critique.Risks))); err != nil {
		return fmt.Errorf("autocommit set-critique: %w", err)
	}

	fmt.Printf("Critique set for %s: %d assumptions, %d risks\n",
		planID, len(critique.Assumptions), len(critique.Risks))
	return nil
}

func planValidateYAMLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate-yaml <plan-id>",
		Short: "Validate a YAML plan's schema",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runValidateYAML(args[0])
		},
	}
}

func runValidateYAML(planID string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")
	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	errors := planyaml.Validate(plan)
	if len(errors) > 0 {
		for _, e := range errors {
			fmt.Fprintf(os.Stderr, "  - %s\n", e)
		}
		return fmt.Errorf("%d validation errors", len(errors))
	}
	// Non-fatal research advisories: nudge legacy/v3 plans toward a cited research
	// basis. v4 plans enforce this in Validate above, so this is silent for them.
	for _, a := range planyaml.ValidateResearchAdvisories(plan) {
		fmt.Fprintf(os.Stderr, "  ! research advisory: %s\n", a)
	}
	// Non-fatal block advisories: nudge standard/complex slices toward visual-block
	// authorship. Trivial slices are always exempt. Never fails validation.
	for _, a := range planyaml.ValidateBlockAdvisories(plan) {
		fmt.Fprintf(os.Stderr, "  ! blocks advisory: %s\n", a)
	}
	// Non-fatal prose-length advisories: nudge why/tests/done_when back toward
	// their corpus norms. Unlike `what` (a hard cap in Validate above), these
	// only warn. Never fails validation.
	for _, a := range planyaml.ValidateProseAdvisories(plan) {
		fmt.Fprintf(os.Stderr, "  ! prose advisory: %s\n", a)
	}
	fmt.Printf("Plan valid: %d slices, %d questions\n", len(plan.Slices), len(plan.Questions))
	return nil
}

// planSetDesignYAMLCmd sets the structured design subsections on a YAML plan.
func planSetDesignYAMLCmd() *cobra.Command {
	var problem, goals, constraints string

	cmd := &cobra.Command{
		Use:   "set-design-yaml <plan-id>",
		Short: "Set problem, goals, and constraints on a YAML plan",
		Long: `Set the structured design subsections on a YAML plan.

Example:
  wipnote plan set-design-yaml plan-a1b2c3d4 \
    --problem "The current system has X limitation..." \
    --goals "Goal 1,Goal 2,Goal 3" \
    --constraints "Must not break X,Must support Y"`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runSetDesignYAML(args[0], problem, goals, constraints)
		},
	}
	cmd.Flags().StringVar(&problem, "problem", "", "problem statement (what's wrong and why)")
	cmd.Flags().StringVar(&goals, "goals", "", "comma-separated measurable goals")
	cmd.Flags().StringVar(&constraints, "constraints", "", "comma-separated constraints")
	return cmd
}

func runSetDesignYAML(planID, problem, goals, constraints string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0) — see storePlanFeedbackEntry for the canonical pattern
	// this mirrors.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	if problem != "" {
		plan.Design.Problem = problem
	}
	if goals != "" {
		plan.Design.Goals = splitTrimmed(goals)
	}
	if constraints != "" {
		plan.Design.Constraints = splitTrimmed(constraints)
	}
	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan: %w", err)
	}

	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): update design", planID)); err != nil {
		return fmt.Errorf("autocommit set-design: %w", err)
	}

	fmt.Printf("Design updated for %s: problem=%v goals=%d constraints=%d\n",
		planID, problem != "", len(plan.Design.Goals), len(plan.Design.Constraints))
	return nil
}

func splitTrimmed(s string) []string {
	parts := strings.Split(s, ",")
	var result []string
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}

// planReadFeedbackYAMLCmd reads canonical feedback for a YAML plan and outputs JSON.
func planReadFeedbackYAMLCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read-feedback-yaml <plan-id>",
		Short: "Read human feedback for a YAML plan",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runReadFeedbackYAML(args[0])
		},
	}
}

func runReadFeedbackYAML(planID string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	// Read YAML status.
	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")
	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan: %w", err)
	}
	entries, err := readPlanFeedbackEntries(wipnoteDir, planID)
	if err != nil {
		return fmt.Errorf("read feedback: %w", err)
	}

	type feedbackResult struct {
		PlanID          string                    `json:"plan_id"`
		Status          string                    `json:"status"`
		DesignApproved  bool                      `json:"design_approved"`
		DesignComment   string                    `json:"design_comment,omitempty"`
		SliceApprovals  map[string]bool           `json:"slice_approvals"`
		QuestionAnswers map[string]string         `json:"question_answers"`
		Comments        map[string]string         `json:"comments"`
		Annotations     []planyaml.PlanAnnotation `json:"annotations"`
	}
	result := feedbackResult{
		PlanID:          planID,
		Status:          plan.Meta.Status,
		SliceApprovals:  make(map[string]bool),
		QuestionAnswers: make(map[string]string),
		Comments:        make(map[string]string),
		Annotations:     []planyaml.PlanAnnotation{},
	}
	for _, e := range entries {
		switch e.Action {
		case "approve":
			if e.Section == "design" {
				result.DesignApproved = approvalStatusFromValue(e.Value) == "approved"
			} else {
				result.SliceApprovals[e.Section] = approvalStatusFromValue(e.Value) == "approved"
			}
		case "comment":
			if e.Section == "design" {
				result.DesignComment = e.Value
			} else {
				result.Comments[e.Section] = e.Value
			}
		case "answer":
			result.QuestionAnswers[e.QuestionID] = e.Value
		case "annotation":
			result.Annotations = append(result.Annotations, planyaml.PlanAnnotation{
				Section:          e.Section,
				Anchor:           e.Anchor,
				Comment:          e.Value,
				QuestionID:       e.QuestionID,
				Consumed:         e.Consumed,
				Resolved:         e.Resolved,
				ResolutionTarget: e.ResolutionTarget,
			})
		}
	}
	return printJSON(result)
}
