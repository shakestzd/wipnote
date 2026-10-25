package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/plan/planyaml"
)

// buildFeatureContent constructs a feature description from a slice's "what" field
// plus an "Accepted Design Decisions" section derived from answered questions.
func buildFeatureContent(what string, questions []planyaml.PlanQuestion, answers map[string]string) string {
	if len(questions) == 0 {
		return what
	}

	var sb strings.Builder
	sb.WriteString(what)

	hasDecisions := false
	for _, q := range questions {
		optionKey := answers[q.ID]
		if optionKey == "" && q.Recommended == "" {
			continue // nothing to embed
		}
		hasDecisions = true
		break
	}
	if !hasDecisions {
		return what
	}

	sb.WriteString("\n\n## Accepted Design Decisions\n")
	for _, q := range questions {
		optionKey := answers[q.ID]
		label := ""
		isUnanswered := false

		if optionKey == "" {
			// Fall back to recommended option.
			optionKey = q.Recommended
			isUnanswered = true
		}

		for _, opt := range q.Options {
			if opt.Key == optionKey {
				label = opt.Label
				break
			}
		}
		if label == "" {
			label = optionKey
		}

		suffix := ""
		if isUnanswered {
			suffix = " (unanswered, using recommended)"
		}
		fmt.Fprintf(&sb, "- **%s** → %s (Q: %s)%s\n", q.Text, label, q.ID, suffix)
	}

	return sb.String()
}

// printFinalizeYAMLSummary prints the structured dispatch summary to stdout.
// featIDs may reference existing features (re-finalize) or newly created ones.
// rejectedTitles lists slice titles that were not approved.
func printFinalizeYAMLSummary(
	plan *planyaml.PlanYAML,
	track *models.Node,
	answers map[string]string,
	featIDs []string,
	rejectedTitles []string,
) {
	trackID := ""
	trackTitle := ""
	if track != nil {
		trackID = track.ID
		trackTitle = track.Title
	} else if plan.Meta.TrackID != "" {
		trackID = plan.Meta.TrackID
	}

	totalSlices := len(plan.Slices)
	approvedCount := len(featIDs)
	rejectedCount := len(rejectedTitles)
	explicitAnswers := 0
	recommendedFallbacks := 0
	for _, q := range plan.Questions {
		if answers[q.ID] != "" {
			explicitAnswers++
		} else if q.Recommended != "" {
			recommendedFallbacks++
		}
	}

	fmt.Printf("\nPlan %s dispatched.\n", plan.Meta.ID)
	fmt.Println()
	if trackTitle != "" {
		fmt.Printf("Track:        %s (%s)\n", trackID, trackTitle)
	} else {
		fmt.Printf("Track:        %s\n", trackID)
	}
	fmt.Printf("Approved:     %d of %d slices\n", approvedCount, totalSlices)
	if rejectedCount > 0 {
		fmt.Printf("Rejected:     %d slices (excluded from dispatch)\n", rejectedCount)
	}

	if len(featIDs) > 0 {
		fmt.Println("\nFeatures created:")
		for i, fid := range featIDs {
			sliceTitle := ""
			if i < len(plan.Slices) {
				// Find matching approved slice title.
				sliceTitle = plan.Slices[i].Title
			}
			fmt.Printf("  %-20s  %s\n", fid, sliceTitle)
		}
	}

	if len(rejectedTitles) > 0 {
		fmt.Println("\nRejected (excluded):")
		for _, t := range rejectedTitles {
			fmt.Printf("  %s  (not approved — excluded)\n", t)
		}
	}

	// Design decisions section: resolved questions with explicit/fallback breakdown.
	type resolvedDecision struct {
		qID   string
		text  string
		label string
		isRec bool // true if resolved via recommended fallback
	}
	var resolvedDecisions []resolvedDecision
	for _, q := range plan.Questions {
		optKey := answers[q.ID]
		isRec := false
		if optKey == "" {
			optKey = q.Recommended
			isRec = true
		}
		if optKey == "" {
			continue
		}
		label := optKey
		for _, opt := range q.Options {
			if opt.Key == optKey {
				label = opt.Label
				break
			}
		}
		resolvedDecisions = append(resolvedDecisions, resolvedDecision{
			qID:   q.ID,
			text:  q.Text,
			label: label,
			isRec: isRec,
		})
	}
	if len(resolvedDecisions) > 0 {
		fmt.Printf("\nDesign decisions (%d, %d explicit / %d recommended defaults):\n",
			len(resolvedDecisions), explicitAnswers, recommendedFallbacks)
		for _, d := range resolvedDecisions {
			fmt.Printf("  %-30s  → %s\n", d.qID, d.label)
		}
	}

	// Use the shared helper so the command format is defined in one place
	// (planNextCommands).
	nextCmd, yoloCmd := planNextCommands(plan.Meta.ID, trackID)
	fmt.Println("\nNext:")
	fmt.Printf("  %s   (in Claude — dispatches tasks)\n", nextCmd)
	fmt.Printf("  OR: %s   (autonomous mode)\n", yoloCmd)
}

// applyAmendments applies accepted amendment directives to plan slices in memory.
// Amendments are applied in order; later amendments for the same field win.
func applyAmendments(plan *planyaml.PlanYAML, amendments []planAmendment) {
	for _, a := range amendments {
		idx := -1
		for i, s := range plan.Slices {
			if s.Num == a.SliceNum {
				idx = i
				break
			}
		}
		if idx < 0 {
			fmt.Fprintf(os.Stderr, "  Amendment skipped: slice %d not found\n", a.SliceNum)
			continue
		}
		s := &plan.Slices[idx]

		switch a.Operation {
		case "add":
			switch a.Field {
			case "done_when":
				s.DoneWhen = append(s.DoneWhen, a.Content)
			case "files":
				s.Files = append(s.Files, a.Content)
			}
		case "remove":
			switch a.Field {
			case "done_when":
				s.DoneWhen = removeStr(s.DoneWhen, a.Content)
			case "files":
				s.Files = removeStr(s.Files, a.Content)
			}
		case "set":
			switch a.Field {
			case "title":
				s.Title = a.Content
			case "what":
				s.What = a.Content
			case "why":
				s.Why = a.Content
			case "effort":
				s.Effort = a.Content
			case "risk":
				s.Risk = a.Content
			}
		}
		fmt.Printf("  Applied amendment: slice-%d %s %s\n", a.SliceNum, a.Operation, a.Field)
	}
}

// removeStr returns a new slice with all occurrences of target removed.
func removeStr(slice []string, target string) []string {
	result := make([]string, 0, len(slice))
	for _, s := range slice {
		if s != target {
			result = append(result, s)
		}
	}
	return result
}

// planNextCommands returns the agentic dispatch commands for a finalized plan.
// Both planFinalizeHandler (API) and printFinalizeYAMLSummary (CLI) use this
// so the format string is defined in exactly one place.
func planNextCommands(planID, trackID string) (nextCommand, yoloCommand string) {
	nextCommand = fmt.Sprintf("/wipnote:execute %s", planID)
	yoloCommand = fmt.Sprintf("wipnote yolo --track %s", trackID)
	return
}
