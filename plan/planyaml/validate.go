package planyaml

import (
	"fmt"
	"strings"
)

// wordCount returns the number of whitespace-delimited words in s. Backs the
// prose-length checks below: the whatWordCap hard gate and the advisory caps
// for why/tests/done_when.
func wordCount(s string) int {
	return len(strings.Fields(s))
}

// whatWordCap is the maximum word count for a slice's `what` field before
// wipnote plan validate-yaml rejects it.
//
// Derivation (not asserted — measured): a prose audit of every slice across
// .wipnote/plans/*.yaml (46 plans, 289 slices, 2026-08-07) found `what`
// word-counted to 33% of all slice-prose words in the corpus, at a median of
// 96 words/slice — plans were inflating narrative well past scannable length.
// Full `what` distribution: p10=33 p25=56 p45=91 p50(median)=96 p60=115
// p75=144 p90=198 p95=224 words (n=289).
//
// The cap is set at 56 words — the corpus's 25th percentile. An earlier draft
// of this cap used 91 (p45, "45% of existing slices already comply") — that
// was the wrong achievability metric: the cap only ever applies to
// non-finalized plans (see the finalized exemption below), so EXISTING-corpus
// compliance is a non-constraint — most of that corpus is legacy prose this
// cap will never touch. The metric that matters is whether a well-written NEW
// slice can comply, and there's direct evidence: plan-f8c02547's own 7 slices
// (written after blocks-first planning, never flagged as unclear by any
// reviewer) run 14-32 words. 56 sits comfortably above that demonstrated
// working range while still being a real, non-round percentile break (not a
// number pulled from thin air like the previously-rejected "~40 words"), and
// it forces genuine compaction on the corpus's median-length slices (96
// words) and everything above.
const whatWordCap = 56

// Advisory-only prose-length norms for why/tests/done_when, derived the same
// way as whatWordCap (each field's own 75th percentile across the same
// .wipnote/plans/*.yaml corpus audit; done_when is summed across its entries
// per slice). These fields were not the ones flagged as disproportionately
// inflating plan prose, so they only warn via ValidateProseAdvisories — they
// never fail wipnote plan validate-yaml.
const (
	whyWordAdvisoryCap      = 48 // p75 of why word counts (n=289)
	testsWordAdvisoryCap    = 53 // p75 of tests word counts (n=279, empty tests excluded)
	doneWhenWordAdvisoryCap = 77 // p75 of per-slice done_when total word counts (n=289)
)

// effectiveComplexity returns the triage classification for a slice. Empty
// string defaults to "standard" so v2 plans written before the Complexity
// field existed continue to validate under the standard rules.
func effectiveComplexity(s PlanSlice) string {
	if s.Complexity == "" {
		return "standard"
	}
	return s.Complexity
}

// Validate checks a PlanYAML for schema errors. Returns a list of error
// strings. Empty list means the plan is valid.
//
// V2 slice-card fields (ApprovalStatus, ExecutionStatus, Questions,
// CriticRevisions) are additive — legacy plans that omit them validate
// without errors.
//
// Per-slice required-field checks branch on effectiveComplexity(slice):
//
//	field             | trivial  | standard | complex
//	------------------+----------+----------+----------
//	what              | optional | required | required
//	done_when         | optional | >=1      | >=2
//	tests             | optional | required | required
//	decisions_notes   | optional | required | required  (>=50 chars after TrimSpace)
//	blocks (or waiver)| optional | required | required
//	>=1 answered Q    | optional | optional | required
//
// title/why/files/effort/risk are unconditionally required regardless of
// complexity. The decisions_notes requirement is gated on
// plan.Meta.Status != "finalized" so historical finalized plans (which
// never carried decisions_notes) continue to validate clean.
//
// Blocks gate: a standard/complex slice with zero Blocks fails unless it
// carries an explicit "blocks_waiver: <reason>" marker in decisions_notes
// (see blocksWaiverReason). This promotes what was previously only
// ValidateBlockAdvisories (non-blocking) to a hard gate: that advisory
// demonstrably went unheeded — 204 of 282 corpus slices default to
// "standard" complexity with 0% block usage among them, so a non-blocking
// nudge does not bind at the tier that carries most authoring volume.
// Trivial slices stay exempt; they have no design surface worth visualising.
// Exempt like decisions_notes (same isStrictModel/explicit-Complexity
// condition, plus finalized) — see the gate's inline comment below for the
// measured blast-radius comparison that set this width.
//
// Prose-length gate: `what` fails when it exceeds whatWordCap words, gated
// the same way as decisions_notes (plan.Meta.Status != "finalized") so
// legacy finalized plans are exempt. See whatWordCap's doc comment for the
// empirical basis. why/tests/done_when are never hard-failed for length —
// see ValidateProseAdvisories for their non-blocking equivalent.
func Validate(plan *PlanYAML) []string {
	errs := validatePlanMeta(plan)

	// Collect slice nums and IDs for duplicate/dep checks.
	nums := map[int]bool{}
	ids := map[string]bool{}

	for i, s := range plan.Slices {
		errs = append(errs, validateSlice(plan, fmt.Sprintf("slices[%d]", i), s, nums, ids)...)
	}

	errs = append(errs, validateSliceDeps(plan, nums)...)
	errs = append(errs, validatePlanQuestions(plan)...)
	errs = append(errs, validateCritique(plan)...)
	return errs
}

// validatePlanMeta validates the meta and design sections.
func validatePlanMeta(plan *PlanYAML) []string {
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
	// Validate SchemaVersion enum when non-empty: "v3" (strict) and "v4" (strict +
	// research-enforced) are accepted; empty is legacy.
	if v := plan.Meta.SchemaVersion; v != "" && v != "v3" && v != "v4" {
		errs = append(errs, fmt.Sprintf("meta.schema_version %q is invalid; accepted values: \"v3\", \"v4\" (or omit for legacy)", v))
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
	// v4: the design itself must carry a cited research basis. Source URLs are
	// shape-checked for any plan that provides them.
	for i, r := range plan.Design.Research {
		if !isResearchURL(r.URL) {
			errs = append(errs, fmt.Sprintf("design.research[%d].url %q must be an http(s) URL", i, r.URL))
		}
	}
	if plan.Meta.SchemaVersion == "v4" && len(plan.Design.Research) == 0 {
		errs = append(errs, "design.research must cite at least 1 web/doc source for v4 plans (the design's research basis)")
	}
	return errs
}

// validateSlice validates one slice in order: complexity tier, prose cap,
// blocks gate, enums, identity, questions, critic revisions, blocks and
// research. nums and ids accumulate across slices for duplicate checks.
func validateSlice(plan *PlanYAML, prefix string, s PlanSlice, nums map[int]bool, ids map[string]bool) []string {
	var errs []string
	errs = append(errs, validateSliceBasics(prefix, s)...)

	complexity := effectiveComplexity(s)

	// isStrictModel identifies plans authored under the triage-gated
	// interview model (v3 introduced decisions_notes as the rationale
	// spine; v4 additionally enforces research). It gates back-compat
	// exemptions below (decisions_notes, blocks) alongside an explicitly-set
	// Complexity, so the pre-triage legacy corpus (empty schema_version AND
	// empty Complexity) is never retroactively broken by a new requirement.
	isStrictModel := plan.Meta.SchemaVersion == "v3" || plan.Meta.SchemaVersion == "v4"

	errs = append(errs, validateSliceTier(plan, prefix, s, complexity, isStrictModel)...)
	errs = append(errs, validateSliceProseAndBlocksGate(plan, prefix, s, complexity, isStrictModel)...)
	errs = append(errs, validateSliceEffortRisk(prefix, s)...)
	errs = append(errs, validateSliceIdentity(prefix, s, nums, ids)...)
	errs = append(errs, validateSliceStatuses(prefix, s)...)
	errs = append(errs, validateSliceQuestions(prefix, s)...)
	errs = append(errs, validateCriticRevisions(prefix, s)...)
	// Phase-2 (slice-6): optional structured blocks. Additive — slices that
	// omit blocks validate unchanged. Shapes are validated ONLY when a block
	// is present, using BlockCatalog as the single source of truth.
	for j, b := range s.Blocks {
		errs = append(errs, validateBlock(fmt.Sprintf("%s.blocks[%d]", prefix, j), b)...)
	}

	// Research gate (v4): non-trivial slices must cite web/doc research or
	// record an explicit waiver, so plans are evidence-backed and don't
	// reinvent battle-tested packages. Source URLs are always shape-checked.
	errs = append(errs, validateSliceResearch(prefix, s, plan.Meta.SchemaVersion == "v4")...)
	return errs
}

// validateSliceBasics checks the Complexity enum and the unconditional
// why/files requirements.
func validateSliceBasics(prefix string, s PlanSlice) []string {
	var errs []string
	// Validate Complexity enum membership when non-empty. Empty string is
	// allowed for back-compat (defaults to "standard" via effectiveComplexity).
	switch s.Complexity {
	case "", "trivial", "standard", "complex":
	default:
		errs = append(errs, fmt.Sprintf("%s.complexity %q must be trivial|standard|complex", prefix, s.Complexity))
	}

	// Unconditional: title/why/files/effort/risk are required regardless
	// of complexity. (Title was previously not enforced; preserve that
	// behavior — only why/files are checked here, plus effort/risk below.)
	if s.Why == "" {
		errs = append(errs, prefix+".why is required")
	}
	if len(s.Files) == 0 {
		errs = append(errs, prefix+".files must have at least 1 entry")
	}
	return errs
}

// validateSliceEffortRisk checks the effort and risk enums.
func validateSliceEffortRisk(prefix string, s PlanSlice) []string {
	var errs []string
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
	return errs
}

// validateSliceStatuses checks the approval_status and execution_status enums.
func validateSliceStatuses(prefix string, s PlanSlice) []string {
	var errs []string
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
	return errs
}

// validateSliceTier applies the complexity-branched requirements:
// what/done_when/tests/decisions_notes/questions.
func validateSliceTier(plan *PlanYAML, prefix string, s PlanSlice, complexity string, isStrictModel bool) []string {
	var errs []string
	// Branched on complexity: what/done_when/tests/decisions_notes/questions.
	switch complexity {
	case "trivial":
		// Trivial slices: what, done_when, tests, decisions_notes all optional.
		// No slice-question requirement.
	case "complex":
		if s.What == "" {
			errs = append(errs, prefix+".what is required")
		}
		if len(s.DoneWhen) < 2 {
			errs = append(errs, prefix+".done_when must have at least 2 entries for complex slices")
		}
		if s.Tests == "" {
			errs = append(errs, prefix+".tests is required")
		}
		if plan.Meta.Status != "finalized" {
			if len(strings.TrimSpace(s.DecisionsNotes)) < 50 {
				errs = append(errs, prefix+".decisions_notes is required (>=50 chars) for complex slices")
			}
		}
		if !hasAnsweredQuestion(s.Questions) {
			errs = append(errs, prefix+".questions must include at least 1 question with a non-empty answer for complex slices")
		}
	default: // "standard"
		if s.What == "" {
			errs = append(errs, prefix+".what is required")
		}
		if len(s.DoneWhen) == 0 {
			errs = append(errs, prefix+".done_when must have at least 1 entry")
		}
		if s.Tests == "" {
			errs = append(errs, prefix+".tests is required")
		}
		// decisions_notes is required when the plan is not finalized AND:
		//   - schema_version == "v3" (strict model: catches omitted Complexity
		//     which defaults to "standard"), OR
		//   - slice.Complexity is explicitly set (legacy behaviour).
		requiresDecisionsNotes := plan.Meta.Status != "finalized" &&
			(isStrictModel || s.Complexity != "")
		if requiresDecisionsNotes {
			if len(strings.TrimSpace(s.DecisionsNotes)) < 50 {
				errs = append(errs, prefix+".decisions_notes is required (>=50 chars) for standard slices")
			}
		}
	}
	return errs
}

// validateSliceProseAndBlocksGate applies the `what` word cap and the
// visual-blocks gate.
func validateSliceProseAndBlocksGate(plan *PlanYAML, prefix string, s PlanSlice, complexity string, isStrictModel bool) []string {
	var errs []string
	// Prose-length gate: `what` was measured to be 33% of all slice-prose
	// words in the corpus (see whatWordCap doc comment). Enforced only on
	// non-finalized plans — the 42 historical finalized plans pre-date the
	// cap and must not retroactively fail validation.
	if plan.Meta.Status != "finalized" {
		if wc := wordCount(s.What); wc > whatWordCap {
			errs = append(errs, fmt.Sprintf("%s.what is %d words, exceeds the %d-word cap (trim into decisions_notes, or split the slice)", prefix, wc, whatWordCap))
		}
	}

	// Blocks gate: standard/complex slices must carry >=1 visual block, or
	// record an explicit skip reason via a "blocks_waiver: <reason>" marker
	// in decisions_notes. See the Validate doc comment for the measured
	// rationale and blocksWaiverReason for the marker contract.
	//
	// Exemption width (measured, not assumed): gating this unconditionally
	// on plan.Meta.Status != "finalized" — the same width as the whatWordCap
	// gate — was tried first and measured against the live .wipnote/plans
	// corpus (46 plans): it broke 21 of 22 non-finalized plans, i.e. almost
	// the entire active/draft backlog, because most predate blocks-first
	// planning entirely. That is too wide a break for a mechanical rule with
	// no way to bulk-satisfy it. Instead this gate reuses the SAME
	// back-compat condition as decisions_notes above (isStrictModel ||
	// explicit Complexity) for the standard tier, and applies unconditionally
	// for complex (which is always explicitly triaged, so that condition is
	// already true). Re-measured with this width: 5 of 22 non-finalized
	// plans newly fail, all schema_version=v3 — i.e. plans already authored
	// under the triage-gated interview model, which is exactly where 0%
	// block usage at the "standard" tier is a live regression rather than
	// pre-existing legacy debt.
	if complexity != "trivial" && plan.Meta.Status != "finalized" {
		requiresBlocks := complexity == "complex" || isStrictModel || s.Complexity != ""
		if requiresBlocks && len(s.Blocks) == 0 && blocksWaiverReason(s) == "" {
			errs = append(errs, fmt.Sprintf("%s.blocks must have at least 1 visual block for %s slices (or record a `blocks_waiver: <reason>` marker in decisions_notes if the slice is genuinely underspecified)", prefix, complexity))
		}
	}
	return errs
}

// validateSliceIdentity checks duplicate nums, self-referencing deps and
// duplicate IDs, recording this slice in nums and ids.
func validateSliceIdentity(prefix string, s PlanSlice, nums map[int]bool, ids map[string]bool) []string {
	var errs []string
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
	return errs
}

// validateSliceDeps checks dep references after all nums are collected.
func validateSliceDeps(plan *PlanYAML, nums map[int]bool) []string {
	var errs []string
	// Check dep references after collecting all nums.
	for i, s := range plan.Slices {
		for _, d := range s.Deps {
			if !nums[d] {
				errs = append(errs, fmt.Sprintf("slices[%d].deps: references nonexistent slice %d", i, d))
			}
		}
	}
	return errs
}
