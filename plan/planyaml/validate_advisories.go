package planyaml

import (
	"fmt"
	"net/url"
	"strings"
)

// validateSliceResearch enforces a slice's research basis. Source URLs are always
// shape-checked (http/https). When enforced (v4) and the slice is non-trivial,
// the slice must carry at least one research source OR a non-empty
// research_waiver, so external claims are evidence-backed and battle-tested
// packages are considered before building custom.
func validateSliceResearch(prefix string, s PlanSlice, enforced bool) []string {
	var errs []string
	for i, r := range s.Research {
		if !isResearchURL(r.URL) {
			errs = append(errs, fmt.Sprintf("%s.research[%d].url %q must be an http(s) URL", prefix, i, r.URL))
		}
	}
	if !enforced || s.Complexity == "trivial" {
		return errs
	}
	if len(s.Research) == 0 && strings.TrimSpace(s.ResearchWaiver) == "" {
		errs = append(errs, prefix+".research must cite at least 1 web/doc source (or set research_waiver) for standard/complex slices")
	}
	return errs
}

// isResearchURL reports whether u is a usable http(s) source URL — it must parse,
// carry an http/https scheme, AND have a non-empty host (so bare "https://" or a
// scheme-only string does not satisfy the gate).
func isResearchURL(u string) bool {
	parsed, err := url.Parse(strings.TrimSpace(u))
	if err != nil {
		return false
	}
	return (parsed.Scheme == "http" || parsed.Scheme == "https") && parsed.Hostname() != ""
}

// ValidateResearchAdvisories returns NON-BLOCKING advisories nudging research on
// plans that do not yet enforce it (legacy/v3). It mirrors the v4 gate so the
// validate-yaml CLI and the critique pass can surface research gaps without
// failing the build. v4 plans return nil — the hard gate in Validate covers them.
func ValidateResearchAdvisories(plan *PlanYAML) []string {
	if plan == nil || plan.Meta.SchemaVersion == "v4" {
		return nil
	}
	var adv []string
	if len(plan.Design.Research) == 0 {
		adv = append(adv, "design.research: no cited research basis; web research is expected for non-trivial plans (set schema_version: v4 to enforce)")
	}
	for i, s := range plan.Slices {
		if s.Complexity == "trivial" {
			continue
		}
		if len(s.Research) == 0 && strings.TrimSpace(s.ResearchWaiver) == "" {
			adv = append(adv, fmt.Sprintf("slices[%d] (%s): no research sources cited and no research_waiver", i, s.ID))
		}
	}
	return adv
}

// ValidateBlockAdvisories returns NON-BLOCKING advisories for standard and
// complex slices that have no visual blocks. Under blocks-first planning the
// visual artifact (data-model / file-tree / wireframe / diagram / tabs)
// is authored INLINE during the plan interview and the prose is derived from it,
// so a qualifying slice with zero blocks signals the blocks-first interview was
// skipped or compressed for that slice. Trivial slices are always exempt — they
// have minimal field requirements and no design surface worth visualising. A
// slice carrying an explicit "blocks_waiver: <reason>" marker in
// decisions_notes (see blocksWaiverReason) is also exempt, mirroring how
// ValidateResearchAdvisories respects ResearchWaiver.
//
// This advisory has been superseded by a hard gate in Validate for
// non-finalized plans (a non-blocking signal was measured to go unheeded at
// the "standard" tier — see the Validate doc comment). It remains here,
// unconditional on Meta.Status, so finalized plans — exempt from the hard
// gate — still get a nudge, and so the critique pass keeps a channel for
// this signal that doesn't fail the build.
//
// Callers surface these the same way research advisories are surfaced: as
// warnings in the validate-yaml / validate CLI paths.
func ValidateBlockAdvisories(plan *PlanYAML) []string {
	if plan == nil {
		return nil
	}
	var adv []string
	for i, s := range plan.Slices {
		complexity := effectiveComplexity(s)
		if complexity == "trivial" {
			continue
		}
		if len(s.Blocks) == 0 && blocksWaiverReason(s) == "" {
			id := s.ID
			if id == "" {
				id = fmt.Sprintf("num=%d", s.Num)
			}
			adv = append(adv, fmt.Sprintf(
				"slices[%d] (%s, %s) has no visual blocks — blocks should be authored during the interview (blocks-first): add grounded data-model/file-tree/wireframe/diagram blocks, or follow ENRICH.md in the wipnote plan skill to enrich this slice after the fact",
				i, id, complexity,
			))
		}
	}
	return adv
}

// ValidateProseAdvisories returns NON-BLOCKING advisories for slices whose
// why/tests/done_when prose runs unusually long — over that field's own
// 75th-percentile norm in the .wipnote/plans/*.yaml corpus audit (see
// whyWordAdvisoryCap/testsWordAdvisoryCap/doneWhenWordAdvisoryCap). Unlike
// whatWordCap, these never fail wipnote plan validate-yaml — `what` was the
// field measured to be disproportionately inflating plan prose (33% of all
// slice-prose words); why/tests/done_when only warn, mirroring the pattern
// established by ValidateBlockAdvisories/ValidateResearchAdvisories.
func ValidateProseAdvisories(plan *PlanYAML) []string {
	if plan == nil {
		return nil
	}
	var adv []string
	for i, s := range plan.Slices {
		id := s.ID
		if id == "" {
			id = fmt.Sprintf("num=%d", s.Num)
		}
		if wc := wordCount(s.Why); wc > whyWordAdvisoryCap {
			adv = append(adv, fmt.Sprintf(
				"slices[%d] (%s): why is %d words — over the %d-word norm; consider trimming into decisions_notes",
				i, id, wc, whyWordAdvisoryCap,
			))
		}
		if wc := wordCount(s.Tests); wc > testsWordAdvisoryCap {
			adv = append(adv, fmt.Sprintf(
				"slices[%d] (%s): tests is %d words — over the %d-word norm; consider trimming",
				i, id, wc, testsWordAdvisoryCap,
			))
		}
		doneWC := 0
		for _, d := range s.DoneWhen {
			doneWC += wordCount(d)
		}
		if doneWC > doneWhenWordAdvisoryCap {
			adv = append(adv, fmt.Sprintf(
				"slices[%d] (%s): done_when is %d words total — over the %d-word norm; consider trimming or splitting criteria",
				i, id, doneWC, doneWhenWordAdvisoryCap,
			))
		}
	}
	return adv
}
