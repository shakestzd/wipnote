package db

// Per-work-item outcome rollup reads (feat-7ee73444).
//
// These are the derived-index half of the rollup that Collection.Complete
// persists into a work item's canonical HTML. They are deliberately shaped as
// *counted* aggregates rather than bare values: every metric ships with the
// number of rows that actually supplied it and the number of rows that were
// eligible to, because the writer's contract is "omit, never zero" — a metric
// with no underlying rows must be absent from the artifact, and a metric with
// thin coverage must say so rather than pass itself off as complete.
//
// All of these read the generic item ID column (`feature_id`), which is keyed
// by work-item ID regardless of type — features, bugs and spikes alike.

// ItemOutcomeStats is the telemetry half of a work item's rollup. Every value
// is paired with the counts needed to state its coverage honestly.
type ItemOutcomeStats struct {
	// Signals is every otel_signals row attributed to the item. Zero means
	// the item has no telemetry at all (a Codex or Antigravity item, or a
	// Claude item completed with telemetry disabled) and the whole telemetry
	// half must be omitted.
	Signals int

	// OutcomeEligible counts rows in the itemOutcomeEligibleSQL set;
	// OutcomeObserved counts the subset that actually carried a non-null
	// success. Their ratio is the failure rate's coverage.
	OutcomeEligible int
	OutcomeObserved int
	Failures        int

	// DurationRows/DurationMsTotal sum span durations. Concurrent agents
	// produce overlapping spans, so the total is NOT elapsed wall clock —
	// see ElapsedMs for that.
	DurationRows    int
	DurationMsTotal int64

	// ElapsedMs is last-signal minus first-signal: real elapsed wall clock
	// across the item's telemetry, valid whenever Signals > 0.
	ElapsedMs int64

	// AttemptRows counts rows carrying an attempt number; Retries counts
	// those past the first try.
	AttemptRows int
	Retries     int

	// CostRows/CostUSDTotal are the known-degraded cost figures: only a
	// minority of signals carry cost_usd, so the total systematically
	// under-reports. Callers must surface CostRows as coverage.
	CostRows     int
	CostUSDTotal float64
}

// ItemEditChurn is the per-file thrash signal for a work item.
//
// It is derived from edit tool calls rather than from commits, because there
// is no commit-to-file join in the derived index at all: git_commits is keyed
// (commit_hash, session_id, feature_id) with no path column, and feature_files
// is UNIQUE(feature_id, file_path), which collapses repeat touches by
// construction. Edit-call multiplicity is the only surviving record of "the
// same file was rewritten more than once", so that is what this measures —
// and the caller labels it as such rather than claiming commit-level churn.
type ItemEditChurn struct {
	// EditEvents is every edit-shaped tool call attributed to the item,
	// including those whose tool_input was not persisted. FilesResolved is
	// the number of distinct paths recoverable from tool_input; their ratio
	// is this metric's coverage.
	EditEvents    int
	FilesResolved int

	// ChurnedFiles counts resolved paths edited more than once, and
	// MaxEditsPerFile is the deepest single-file rewrite count.
	ChurnedFiles    int
	MaxEditsPerFile int
}
