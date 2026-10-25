package planyaml

import (
	"fmt"
	"strings"
)

// hasAnsweredQuestion reports whether any slice-local question has a
// non-empty answer (after TrimSpace).
func hasAnsweredQuestion(qs []SliceQuestion) bool {
	for _, q := range qs {
		if strings.TrimSpace(q.Answer) != "" {
			return true
		}
	}
	return false
}

// validateSliceQuestions rejects duplicate slice-local question IDs and
// validates the structured form.
func validateSliceQuestions(prefix string, s PlanSlice) []string {
	var errs []string
	// V2: slice-local questions — reject duplicate IDs; validate structured form.
	qIDs := map[string]bool{}
	for j, q := range s.Questions {
		qPfx := fmt.Sprintf("%s.questions[%d]", prefix, j)
		if q.ID != "" {
			if qIDs[q.ID] {
				errs = append(errs, fmt.Sprintf("%s.id %q is duplicate within slice", qPfx, q.ID))
			}
			qIDs[q.ID] = true
		}
		// Structured form: when options are present, recommended must match a key.
		if q.Recommended != "" && len(q.Options) > 0 {
			found := false
			for _, o := range q.Options {
				if o.Key == q.Recommended {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s.recommended %q not in options", qPfx, q.Recommended))
			}
		}
	}
	return errs
}

// validateCriticRevisions requires source, severity and summary on every
// critic revision.
func validateCriticRevisions(prefix string, s PlanSlice) []string {
	var errs []string
	// V2: critic_revisions — require source, severity, summary.
	for j, cr := range s.CriticRevisions {
		crPrefix := fmt.Sprintf("%s.critic_revisions[%d]", prefix, j)
		if cr.Source == "" {
			errs = append(errs, crPrefix+".source is required")
		}
		if cr.Severity == "" {
			errs = append(errs, crPrefix+".severity is required")
		}
		if cr.Summary == "" {
			errs = append(errs, crPrefix+".summary is required")
		}
	}
	return errs
}

// validatePlanQuestions validates the plan-level questions.
func validatePlanQuestions(plan *PlanYAML) []string {
	var errs []string
	for i, q := range plan.Questions {
		prefix := fmt.Sprintf("questions[%d]", i)
		if q.Text == "" {
			errs = append(errs, prefix+".text is required")
		}
		if q.Description == "" {
			errs = append(errs, prefix+".description is required")
		}
		if len(q.Options) < 2 {
			errs = append(errs, prefix+".options must have at least 2 entries")
		}
		if q.Recommended != "" {
			found := false
			for _, o := range q.Options {
				if o.Key == q.Recommended {
					found = true
					break
				}
			}
			if !found {
				errs = append(errs, fmt.Sprintf("%s.recommended %q not in options", prefix, q.Recommended))
			}
		}
	}
	return errs
}

// validateCritique validates the critique section if present.
func validateCritique(plan *PlanYAML) []string {
	var errs []string
	// Validate critique section if present.
	if plan.Critique != nil {
		c := plan.Critique
		if c.ReviewedAt == "" {
			errs = append(errs, "critique.reviewed_at is required")
		}
		for i, a := range c.Assumptions {
			prefix := fmt.Sprintf("critique.assumptions[%d]", i)
			switch a.Status {
			case "verified", "plausible", "unverified", "questionable", "falsified":
			default:
				errs = append(errs, fmt.Sprintf("%s.status %q is invalid", prefix, a.Status))
			}
			if a.Text == "" {
				errs = append(errs, prefix+".text is required")
			}
		}
		for i, r := range c.Risks {
			switch r.Severity {
			case "High", "Medium", "Low":
			default:
				errs = append(errs, fmt.Sprintf("critique.risks[%d].severity %q is invalid", i, r.Severity))
			}
		}
	}
	return errs
}
