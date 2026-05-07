package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
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

	var out planFeedbackOutput

	// Prefer API if server is running.
	if isServerRunning("http://localhost:8080") {
		out, err = fetchFeedbackFromAPI(planID)
		if err == nil {
			return printFeedbackJSON(out)
		}
	}

	// Fall back to HTML file parsing.
	planPath := filepath.Join(wipnoteDir, "plans", planID+".html")
	out, err = parseFeedbackFromHTML(planID, planPath)
	if err != nil {
		return fmt.Errorf("read plan feedback: %w", err)
	}
	return printFeedbackJSON(out)
}

// fetchFeedbackFromAPI retrieves feedback via GET /api/plans/{id}/feedback
// and maps the server response to planFeedbackOutput.
func fetchFeedbackFromAPI(planID string) (planFeedbackOutput, error) {
	url := "http://localhost:8080/api/plans/" + planID + "/feedback"
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get(url) //nolint:gosec,noctx
	if err != nil {
		return planFeedbackOutput{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return planFeedbackOutput{}, fmt.Errorf("API returned %d", resp.StatusCode)
	}

	var apiResp struct {
		PlanID   string `json:"plan_id"`
		Status   string `json:"status"`
		Sections map[string]struct {
			Approved bool   `json:"approved"`
			Comment  string `json:"comment"`
		} `json:"sections"`
		Questions map[string]string `json:"questions"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&apiResp); err != nil {
		return planFeedbackOutput{}, err
	}

	out := planFeedbackOutput{
		PlanID:           apiResp.PlanID,
		Status:           apiResp.Status,
		SectionApprovals: make(map[string]bool),
		QuestionAnswers:  apiResp.Questions,
		SliceApprovals:   make(map[string]bool),
		Comments:         make(map[string]string),
	}
	if out.QuestionAnswers == nil {
		out.QuestionAnswers = make(map[string]string)
	}

	for sec, fb := range apiResp.Sections {
		out.SectionApprovals[sec] = fb.Approved
		if strings.HasPrefix(sec, "slice-") {
			out.SliceApprovals[sec] = fb.Approved
		}
		if fb.Comment != "" {
			out.Comments[sec] = fb.Comment
		}
	}

	return out, nil
}

// parseFeedbackFromHTML reads the plan HTML file directly and extracts
// feedback from data-* attributes and form element state.
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

	out := planFeedbackOutput{
		PlanID:           planID,
		SectionApprovals: make(map[string]bool),
		QuestionAnswers:  make(map[string]string),
		SliceApprovals:   make(map[string]bool),
		Comments:         make(map[string]string),
	}

	// Status from the top-level article.
	out.Status, _ = doc.Find("article").First().Attr("data-status")
	if out.Status == "" {
		out.Status = "draft"
	}

	// Approval checkboxes: input[data-action="approve"]
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

	// Comments: textarea[data-comment-for]
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

	return out, nil
}

// printFeedbackJSON marshals the output struct and writes it to stdout.
func printFeedbackJSON(out planFeedbackOutput) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(out)
}
