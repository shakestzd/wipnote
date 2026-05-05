package planyaml

import "fmt"

// Validate checks a PlanYAML for schema errors. Returns a list of error
// strings. Empty list means the plan is valid.
//
// V2 slice-card fields (ApprovalStatus, ExecutionStatus, Questions,
// CriticRevisions) are additive — legacy plans that omit them validate
// without errors.
func Validate(plan *PlanYAML) []string {
	var errs []string
	if plan.Meta.ID == "" {
		errs = append(errs, "meta.id is required")
	}
	if plan.Meta.Title == "" {
		errs = append(errs, "meta.title is required")
	}
	switch plan.Meta.Status {
	case "draft", "review", "finalized", "active", "completed":
	default:
		errs = append(errs, fmt.Sprintf("meta.status %q must be draft|review|finalized|active|completed", plan.Meta.Status))
	}
	if plan.Design.Problem == "" {
		errs = append(errs, "design.problem is required")
	}
	if len(plan.Design.Goals) == 0 {
		errs = append(errs, "design.goals must have at least 1 entry")
	}
	if len(plan.Design.Constraints) == 0 {
		errs = append(errs, "design.constraints must have at least 1 entry")
	}

	// Collect slice nums and IDs for duplicate/dep checks.
	nums := map[int]bool{}
	ids := map[string]bool{}

	for i, s := range plan.Slices {
		prefix := fmt.Sprintf("slices[%d]", i)
		if s.What == "" {
			errs = append(errs, prefix+".what is required")
		}
		if s.Why == "" {
			errs = append(errs, prefix+".why is required")
		}
		if len(s.Files) == 0 {
			errs = append(errs, prefix+".files must have at least 1 entry")
		}
		if len(s.DoneWhen) == 0 {
			errs = append(errs, prefix+".done_when must have at least 1 entry")
		}
		if s.Tests == "" {
			errs = append(errs, prefix+".tests is required")
		}
		switch s.Effort {
		case "S", "M", "L":
		default:
			errs = append(errs, fmt.Sprintf("%s.effort %q must be S|M|L", prefix, s.Effort))
		}
		switch s.Risk {
		case "Low", "Med", "High":
		default:
			errs = append(errs, fmt.Sprintf("%s.risk %q must be Low|Med|High", prefix, s.Risk))
		}
		if nums[s.Num] {
			errs = append(errs, fmt.Sprintf("%s.num %d is duplicate", prefix, s.Num))
		}
		nums[s.Num] = true
		for _, d := range s.Deps {
			if d == s.Num {
				errs = append(errs, fmt.Sprintf("%s.deps: self-reference %d", prefix, d))
			}
		}

		// Duplicate slice IDs (non-empty IDs only).
		if s.ID != "" {
			if ids[s.ID] {
				errs = append(errs, fmt.Sprintf("%s.id %q is duplicate", prefix, s.ID))
			}
			ids[s.ID] = true
		}

		// V2: approval_status enum (empty = unset, valid for legacy plans).
		switch s.ApprovalStatus {
		case "", "pending", "approved", "rejected", "changes_requested":
		default:
			errs = append(errs, fmt.Sprintf("%s.approval_status %q must be pending|approved|rejected|changes_requested", prefix, s.ApprovalStatus))
		}

		// V2: execution_status enum (empty = unset, valid for legacy plans).
		switch s.ExecutionStatus {
		case "", "not_started", "promoted", "in_progress", "done", "blocked", "superseded":
		default:
			errs = append(errs, fmt.Sprintf("%s.execution_status %q must be not_started|promoted|in_progress|done|blocked|superseded", prefix, s.ExecutionStatus))
		}

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
	}

	// Check dep references after collecting all nums.
	for i, s := range plan.Slices {
		for _, d := range s.Deps {
			if !nums[d] {
				errs = append(errs, fmt.Sprintf("slices[%d].deps: references nonexistent slice %d", i, d))
			}
		}
	}

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
