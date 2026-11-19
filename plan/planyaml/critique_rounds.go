package planyaml

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

// MaxCritiqueRounds caps how many critique rounds one plan gets.
const MaxCritiqueRounds = 2

var pathLineRe = regexp.MustCompile(`^[^\s:]+:[0-9]+(-[0-9]+)?$`)

// ValidateCritique reports every problem in a critique section. It is the
// check set-critique-yaml runs before it stores a critique.
func ValidateCritique(c *PlanCritique) []string {
	return validateCritique(&PlanYAML{Critique: c})
}

// validateCritiqueRounds checks the critic status values and the citation on
// each contested finding.
func validateCritiqueRounds(c *PlanCritique) []string {
	var errs []string
	for i, cr := range c.Critics {
		switch cr.Status {
		case "", "completed", "not_run":
		default:
			errs = append(errs, fmt.Sprintf("critique.critics[%d].status %q is invalid: use completed or not_run", i, cr.Status))
		}
		for j, sub := range cr.Sections {
			for k, it := range sub.Items {
				if it.Contested && !validCitation(it.Citation) {
					errs = append(errs, fmt.Sprintf(
						"critique.critics[%d].sections[%d].items[%d] is contested and needs a citation as path:line or http(s) URL, got %q",
						i, j, k, it.Citation))
				}
			}
		}
	}
	for i, r := range c.AcceptedRisks {
		if strings.TrimSpace(r.Text) == "" || strings.TrimSpace(r.Reason) == "" {
			errs = append(errs, fmt.Sprintf("critique.accepted_risks[%d] needs both text and reason", i))
		}
	}
	return errs
}

// validCitation is true for path:line (or path:line-line) and http(s) URLs.
func validCitation(s string) bool {
	if pathLineRe.MatchString(s) && !strings.Contains(s, "://") {
		return true
	}
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// SettleCritique carries the accepted risks of prev into next and drops any
// finding in next that restates one of them, so a later round does not raise
// a settled risk again. A nil prev only drops findings that match next's own
// accepted risks.
func SettleCritique(prev, next *PlanCritique) {
	if prev != nil {
		seen := map[string]bool{}
		for _, r := range next.AcceptedRisks {
			seen[r.Text] = true
		}
		for _, r := range prev.AcceptedRisks {
			if !seen[r.Text] {
				next.AcceptedRisks = append(next.AcceptedRisks, r)
			}
		}
	}
	settled := map[string]bool{}
	for _, r := range next.AcceptedRisks {
		settled[strings.TrimSpace(r.Text)] = true
	}
	for i := range next.Critics {
		for j := range next.Critics[i].Sections {
			sub := &next.Critics[i].Sections[j]
			kept := sub.Items[:0]
			for _, it := range sub.Items {
				if !settled[strings.TrimSpace(it.Text)] {
					kept = append(kept, it)
				}
			}
			sub.Items = kept
		}
	}
}

// OpenFindings lists the warn and danger findings that no accepted risk
// settles and that are not contested.
func OpenFindings(c *PlanCritique) []CriticItem {
	if c == nil {
		return nil
	}
	settled := map[string]bool{}
	for _, r := range c.AcceptedRisks {
		settled[strings.TrimSpace(r.Text)] = true
	}
	var open []CriticItem
	for _, cr := range c.Critics {
		for _, sub := range cr.Sections {
			for _, it := range sub.Items {
				if (it.Kind == "warn" || it.Kind == "danger") && !it.Contested && !settled[strings.TrimSpace(it.Text)] {
					open = append(open, it)
				}
			}
		}
	}
	return open
}

// CheckCritiqueRound refuses a new round once MaxCritiqueRounds are recorded.
// The error names each open finding.
func CheckCritiqueRound(c *PlanCritique) error {
	if c == nil || c.Round < MaxCritiqueRounds {
		return nil
	}
	open := OpenFindings(c)
	if len(open) == 0 {
		return fmt.Errorf("critique is capped at %d rounds and %d are recorded; no findings are open, so hand the plan to review",
			MaxCritiqueRounds, c.Round)
	}
	var b strings.Builder
	fmt.Fprintf(&b, "critique is capped at %d rounds and %d are recorded. Open findings:", MaxCritiqueRounds, c.Round)
	for _, it := range open {
		fmt.Fprintf(&b, "\n  - [%s] %s %s", it.Kind, it.Badge, it.Text)
	}
	b.WriteString("\nFix them, record an accepted risk with its reason, or hand the plan to review.")
	return fmt.Errorf("%s", b.String())
}
