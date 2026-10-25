package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/PuerkitoBio/goquery"
	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// planFeedbackOutput is the structured JSON written to stdout by read-feedback.
type planFeedbackOutput struct {
	PlanID           string            `json:"plan_id"`
	Status           string            `json:"status"`
	SectionApprovals map[string]bool   `json:"section_approvals"`
	QuestionAnswers  map[string]string `json:"question_answers"`
	SliceApprovals   map[string]bool   `json:"slice_approvals"`
	Comments         map[string]string `json:"comments"`
}

// planReadFeedbackCmd reads finalized plan feedback and outputs structured JSON.
func planReadFeedbackCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "read-feedback <plan-id>",
		Short: "Read finalized plan feedback as structured JSON",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runPlanReadFeedback(args[0])
		},
	}
}

func runPlanReadFeedback(planID string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	out, err := readPlanFeedback(wipnoteDir, planID)
	if err != nil {
		return fmt.Errorf("read plan feedback: %w", err)
	}
	return printFeedbackJSON(out)
}

// readPlanFeedback reads the canonical feedback recorded in the plan YAML.
// Plans that predate the YAML (HTML only) fall back to the rendered form.
func readPlanFeedback(wipnoteDir, planID string) (planFeedbackOutput, error) {
	yamlPath := planYAMLPath(wipnoteDir, planID)
	if _, err := os.Stat(yamlPath); err == nil {
		plan, err := planyaml.Load(yamlPath)
		if err != nil {
			return planFeedbackOutput{}, err
		}
		entries := feedbackEntriesFromPlan(planID, plan)
		return feedbackOutputFromEntries(planID, plan.Meta.Status, entries), nil
	}
	return parseFeedbackFromHTML(planID, filepath.Join(wipnoteDir, "plans", planID+".html"))
}

// feedbackOutputFromEntries groups canonical feedback entries the way the
// former dashboard feedback API (buildFeedbackResponse) did. Every section
// with an approve OR a comment entry counts; the last approve entry per
// section wins, and a comment-only section counts as not approved. The plan
// reads "approved" once every counted section is approved, else "draft".
//
// metaStatus is the plan's real meta.status. A plan whose review is over
// (finalized, completed, done) reports that status as-is, as the HTML read
// path always did; see feedbackStatus.
func feedbackOutputFromEntries(planID, metaStatus string, entries []dbpkg.PlanFeedback) planFeedbackOutput {
	out := newFeedbackOutput(planID)
	for _, e := range entries {
		if e.Section != "chat" {
			applyFeedbackEntry(&out, e)
		}
	}
	for section, approved := range out.SectionApprovals {
		if strings.HasPrefix(section, "slice-") {
			out.SliceApprovals[section] = approved
		}
	}
	out.Status = feedbackStatus(metaStatus, out.SectionApprovals)
	return out
}

// applyFeedbackEntry folds one canonical feedback entry into out.
func applyFeedbackEntry(out *planFeedbackOutput, e dbpkg.PlanFeedback) {
	switch e.Action {
	case "approve":
		out.SectionApprovals[e.Section] = dbpkg.IsPlanApprovalValueApproved(e.Value)
	case "comment":
		if _, decided := out.SectionApprovals[e.Section]; !decided {
			out.SectionApprovals[e.Section] = false
		}
		if e.Value != "" {
			out.Comments[e.Section] = e.Value
		}
	case "answer":
		if e.QuestionID != "" {
			out.QuestionAnswers[e.QuestionID] = e.Value
		}
	}
}

// feedbackStatus returns metaStatus for a plan whose review is over
// (finalized, completed, done), else "approved" when there is at least one
// section and all are approved, else "draft". Statuses a plan can hold while
// slices are still being decided (draft, review, todo, active, in-progress)
// get the feedback-derived status.
func feedbackStatus(metaStatus string, sections map[string]bool) string {
	switch metaStatus {
	case "finalized", "completed", "done":
		return metaStatus
	}
	if len(sections) == 0 {
		return "draft"
	}
	for _, approved := range sections {
		if !approved {
			return "draft"
		}
	}
	return "approved"
}

// parseFeedbackFromHTML reads a legacy HTML-only plan and extracts feedback
// from data-* attributes and form element state.
func parseFeedbackFromHTML(planID, planPath string) (planFeedbackOutput, error) {
	f, err := os.Open(planPath)
	if err != nil {
		return planFeedbackOutput{}, fmt.Errorf("open plan %s: %w", planPath, err)
	}
	defer f.Close()

	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		return planFeedbackOutput{}, fmt.Errorf("parse HTML: %w", err)
	}

	out := newFeedbackOutput(planID)

	// Status from the top-level article.
	out.Status, _ = doc.Find("article").First().Attr("data-status")
	if out.Status == "" {
		out.Status = "draft"
	}
	readHTMLApprovals(doc, &out)
	readHTMLCommentsAndAnswers(doc, &out)
	return out, nil
}

// newFeedbackOutput returns an output for planID with every map allocated.
func newFeedbackOutput(planID string) planFeedbackOutput {
	return planFeedbackOutput{
		PlanID:           planID,
		SectionApprovals: make(map[string]bool),
		QuestionAnswers:  make(map[string]string),
		SliceApprovals:   make(map[string]bool),
		Comments:         make(map[string]string),
	}
}

// readHTMLApprovals reads approval checkboxes: input[data-action="approve"].
func readHTMLApprovals(doc *goquery.Document, out *planFeedbackOutput) {
	doc.Find(`input[data-action="approve"]`).Each(func(_ int, sel *goquery.Selection) {
		section, _ := sel.Attr("data-section")
		if section == "" {
			return
		}
		_, checked := sel.Attr("checked")
		out.SectionApprovals[section] = checked
		if strings.HasPrefix(section, "slice-") {
			out.SliceApprovals[section] = checked
		}
	})
}

// readHTMLCommentsAndAnswers reads comment textareas and checked question
// radios from a legacy plan form.
func readHTMLCommentsAndAnswers(doc *goquery.Document, out *planFeedbackOutput) {
	doc.Find("textarea[data-comment-for]").Each(func(_ int, sel *goquery.Selection) {
		section, _ := sel.Attr("data-comment-for")
		if section == "" {
			return
		}
		text := strings.TrimSpace(sel.Text())
		if text != "" {
			out.Comments[section] = text
		}
	})

	// Radio question answers: input[type="radio"][checked][data-question]
	doc.Find(`input[type="radio"][data-question]`).Each(func(_ int, sel *goquery.Selection) {
		if _, checked := sel.Attr("checked"); !checked {
			return
		}
		questionID, _ := sel.Attr("data-question")
		value, _ := sel.Attr("value")
		if questionID != "" && value != "" {
			out.QuestionAnswers[questionID] = value
		}
	})
}

// printFeedbackJSON marshals the output struct and writes it to stdout.
func printFeedbackJSON(out planFeedbackOutput) error {
	return printJSON(out)
}
