// Package main — compliance auto subcommand.
// Invoked as: wipnote compliance auto <feature-id> [flags]
//
// Pipeline:
//  1. Resolve git root; skip with "no git history" if not a repo.
//  2. Acquire per-feature lockfile; exit 1 if already running.
//  3. Load feature node; error if not found.
//  4. Extract spec; skip with "no spec" if absent.
//  5. Compute spec hash.
//  6. Build diff blob from the commits linked to the item (message convention
//     plus any committed_in edges); skip when none.
//  7. Truncate diff at --max-diff-chars at line boundary.
//  8. --dry-run: print prompt, exit 0.
//  9. Call headless claude -p; capture stdout/stderr.
//  10. Parse JSON result.
//  11. --preview: print findings to stdout, no HTML write.
//  12. Write <section class="compliance-findings"> atomically.
//  13. Print one-line summary.
package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// complianceAutoFlags holds all parsed flags for the auto subcommand.
type complianceAutoFlags struct {
	model        string
	effort       string
	dryRun       bool
	preview      bool
	maxDiffChars int
	maxTurns     int
	maxWallClock time.Duration
	batchSince   string
}

// autoComplianceFinding is the JSON structure returned by the LLM.
type autoComplianceFinding struct {
	Summary  string                    `json:"summary"`
	Criteria []autoComplianceCriterion `json:"criteria"`
	Score    int                       `json:"score"`
	Notes    string                    `json:"notes"`
}

// autoComplianceCriterion is one criterion in the LLM response.
type autoComplianceCriterion struct {
	Text     string `json:"text"`
	Status   string `json:"status"` // "pass" | "fail" | "unclear"
	Evidence string `json:"evidence"`
}

// headlessRequest holds the parameters for a headless claude invocation.
type headlessRequest struct {
	model        string
	effort       string
	maxTurns     int
	maxBudgetUSD float64
	maxWallClock time.Duration
	systemPrompt string
	userPrompt   string
}

// headlessResult holds the response from a headless claude invocation.
type headlessResult struct {
	text    string
	costUSD float64
}

// BudgetExceededError is returned when the LLM hits the --max-budget-usd cap.
type BudgetExceededError struct {
	msg string
}

func (e *BudgetExceededError) Error() string { return e.msg }

// headlessInvokerFn is the type for the headless invoker function.
// Package-level var for test stub injection.
// NOTE: Tests that swap this MUST NOT call t.Parallel() — the var is global.
var headlessInvoker = realHeadlessInvoker

// diffBuilderFn is the type for the diff builder function.
// Package-level var for test stub injection — allows tests to bypass git/DB diff
// collection and provide a deterministic diff blob directly.
// NOTE: Tests that swap this MUST NOT call t.Parallel() — the var is global.
var diffBuilderFn = realBuildDiffBlob

// realBuildDiffBlob is the production diff builder; wraps buildDiffBlob.
func realBuildDiffBlob(ctx context.Context, database *sql.DB, featureID, gitRoot string, maxChars int) (string, bool, error) {
	return buildDiffBlob(ctx, database, featureID, gitRoot, maxChars)
}

// complianceAutoCmd returns the "auto" subcommand for "compliance".
// It should be added to the compliance command via:
//
//	compliance.AddCommand(complianceAutoCmd())
func complianceAutoCmd() *cobra.Command {
	var flags complianceAutoFlags

	cmd := &cobra.Command{
		Use:   "auto <feature-id>",
		Short: "Auto-grade a feature's compliance with its spec using claude -p (headless)",
		Long: `Compare a feature's spec against its implementation diff via claude -p and write
structured findings to a <section class="compliance-findings"> in the feature HTML.

Flags:
  --dry-run     Print the prompt that would be sent; skip LLM call.
  --preview     Run the full pipeline but print findings to stdout; do not write HTML.
  --batch-since Run against all done features completed since a date (format: 2006-01-02).`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if flags.batchSince != "" {
				return runComplianceAutoBatch(cmd.Context(), flags)
			}
			if len(args) == 0 {
				return fmt.Errorf("feature-id required (or use --batch-since <date>)")
			}
			return runComplianceAuto(cmd.Context(), args[0], flags)
		},
	}

	cmd.Flags().StringVar(&flags.model, "model", "sonnet", "Model to use for headless claude invocation")
	cmd.Flags().StringVar(&flags.effort, "effort", "medium", "Effort level for headless claude (low|medium|high)")
	cmd.Flags().BoolVar(&flags.dryRun, "dry-run", false, "Print the prompt; skip LLM call; exit 0")
	cmd.Flags().BoolVar(&flags.preview, "preview", false, "Run full pipeline; print findings to stdout; do not write HTML")
	cmd.Flags().IntVar(&flags.maxDiffChars, "max-diff-chars", 50000, "Max characters of diff to include in prompt (truncated at line boundary)")
	cmd.Flags().IntVar(&flags.maxTurns, "max-turns", 5, "Max tool-loop turns for claude -p")
	cmd.Flags().DurationVar(&flags.maxWallClock, "max-wall-clock", 5*time.Minute, "Self-enforced wall-clock timeout for the LLM call")
	cmd.Flags().StringVar(&flags.batchSince, "batch-since", "", "Iterate all done features completed since date (format: 2006-01-02); rate-limited to 1 call per 30s")

	return cmd
}

// runComplianceAuto runs the full compliance auto pipeline for a single feature.
func runComplianceAuto(ctx context.Context, featureID string, flags complianceAutoFlags) error {
	// Step 1: Resolve wipnote dir and project root.
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	projectDir := filepath.Dir(wipnoteDir)
	r := &complianceRun{
		ctx: ctx, featureID: featureID, flags: flags,
		featurePath: filepath.Join(wipnoteDir, "features", featureID+".html"),
	}

	gitRoot, err := paths.GitTopLevel(projectDir)
	if err != nil {
		// Not a git repo — write "no git history" finding, exit 0.
		return r.skip("no git history")
	}

	// Step 2: Acquire per-feature lockfile.
	lockPath := filepath.Join(wipnoteDir, "locks", "compliance-"+featureID+".lock")
	unlock, err := acquireComplianceLock(lockPath)
	if err != nil {
		return err
	}
	defer unlock()

	return r.run(wipnoteDir, gitRoot)
}

// run grades the feature once the lock is held: steps 3-13.
func (r *complianceRun) run(wipnoteDir, gitRoot string) error {
	ctx, featureID, flags := r.ctx, r.featureID, r.flags
	featureHTML, specContent, done, err := r.loadSpec()
	if done || err != nil {
		return err
	}

	// Step 6: Build diff blob.
	database, err := openDB(wipnoteDir)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer database.Close()

	diffBlob, diffTruncated, err := diffBuilderFn(ctx, database, featureID, gitRoot, flags.maxDiffChars)
	if err != nil {
		return fmt.Errorf("build diff: %w", err)
	}
	r.diffTruncated = diffTruncated

	if diffBlob == "" {
		return r.skip("no diff available")
	}

	outcomeEvidence := r.gatherOutcomeEvidence(database, featureHTML)

	// Step 7: Build prompt.
	systemPrompt := buildComplianceSystemPrompt()
	userPrompt := buildComplianceUserPrompt(specContent, diffBlob, outcomeEvidence)

	// Step 8: --dry-run: print prompt, exit 0.
	if flags.dryRun {
		fmt.Println("=== SYSTEM PROMPT ===")
		fmt.Println(systemPrompt)
		fmt.Println("=== USER PROMPT ===")
		fmt.Println(userPrompt)
		return nil
	}

	return r.grade(systemPrompt, userPrompt)
}

// loadSpec reads the feature file, extracts its spec and records the spec
// hashes. done reports that the run has ended (a skip finding was written).
func (r *complianceRun) loadSpec() (featureHTML []byte, specContent string, done bool, err error) {
	// Step 3: Load feature path; error if not found.
	if _, err := os.Stat(r.featurePath); err != nil {
		return nil, "", false, fmt.Errorf("no feature: %s", r.featureID)
	}

	featureHTML, err = os.ReadFile(r.featurePath)
	if err != nil {
		return nil, "", false, fmt.Errorf("read feature file: %w", err)
	}

	// Step 4: Extract spec.
	specContent = extractSpecSection(string(featureHTML))
	if specContent == "" {
		return nil, "", true, r.skip("no spec")
	}

	// Step 5: Compute spec hash.
	r.specHash = computeSpecHash(specContent)

	// Check if existing section has a different spec hash (for stale-spec reporting).
	r.prevSpecHash = extractPrevSpecHash(string(featureHTML))
	return featureHTML, specContent, false, nil
}

// complianceRun is the state of one runComplianceAuto call that the
// skip, grading and finding-write steps share.
type complianceRun struct {
	ctx         context.Context
	featureID   string
	flags       complianceAutoFlags
	featurePath string

	specHash, prevSpecHash string
	diffTruncated          bool
	gatePass, gateFail     int
}

// skip writes a "compliance skipped: <reason>" finding and reports the skip.
func (r *complianceRun) skip(reason string) error {
	if err := writeSkipFinding(r.featurePath, "compliance skipped: "+reason); err != nil {
		return err
	}
	fmt.Printf("compliance %s: skipped — %s\n", r.featureID, reason)
	return nil
}

// gatherOutcomeEvidence is step 6.5: gather outcome evidence (feat-f9118b9c). Up to this point
// compliance auto has only ever compared the diff against the spec — it
// has never asked whether the result actually worked. rollupSig reads
// the data-rollup-* attributes feat-7ee73444 persists directly onto this
// same feature HTML (re-parsed from the featureHTML bytes already read
// in step 3, no extra disk I/O); gatePass/gateFail come from this item's
// own quality-gate history. Both sources degrade to "no evidence" rather
// than aborting the run — a feature file that predates the <article>
// convention, or a gate query error, must not block grading — and
// buildOutcomeEvidence says so explicitly in the prompt text rather than
// letting an absence read as a clean result.
func (r *complianceRun) gatherOutcomeEvidence(database *sql.DB, featureHTML []byte) string {
	var rollupSig workitem.ItemRollupSignal
	if node, parseErr := htmlparse.ParseString(string(featureHTML)); parseErr == nil {
		rollupSig = workitem.RollupSignalFor(node)
	}
	gatePass, gateFail, gateErr := dbpkg.GateRecordCountsForWorkItem(database, r.featureID)
	if gateErr != nil {
		fmt.Fprintf(os.Stderr, "wipnote: compliance %s: gate history unavailable: %v\n", r.featureID, gateErr)
		gatePass, gateFail = 0, 0
	}
	r.gatePass, r.gateFail = gatePass, gateFail
	return buildOutcomeEvidence(rollupSig, gatePass, gateFail)
}

// findingAttrs builds the compliance section attributes for a finding.
func (r *complianceRun) findingAttrs(score, costUSD string) map[string]string {
	return map[string]string{
		"score":          score,
		"cost-usd":       costUSD,
		"model":          r.flags.model,
		"spec-hash":      r.specHash,
		"timestamp":      time.Now().UTC().Format(time.RFC3339),
		"diff-truncated": strconv.FormatBool(r.diffTruncated),
		"gate-pass":      strconv.Itoa(r.gatePass),
		"gate-fail":      strconv.Itoa(r.gateFail),
	}
}

// recordErrorFinding writes an error finding to the feature, or prints it in
// --preview mode. Write errors are ignored: the caller returns the error that
// produced the finding.
func (r *complianceRun) recordErrorFinding(attrs map[string]string, finding *autoComplianceFinding) {
	body := renderFindingsHTML(finding)
	if !r.flags.preview {
		_ = writeComplianceSection(r.featurePath, attrs, body)
	} else {
		fmt.Println(body)
	}
}

// grade calls headless claude, parses its finding and records it.
func (r *complianceRun) grade(systemPrompt, userPrompt string) error {
	featureID, flags := r.featureID, r.flags
	// Step 9: Call headless claude.
	result, err := headlessInvoker(r.ctx, r.headlessRequest(systemPrompt, userPrompt))
	if err != nil {
		return r.invokeFailed(err)
	}

	// Step 10: Parse JSON result.
	finding, err := parseComplianceFinding(result.text)
	if err != nil {
		// Write parse-failure finding, exit 1.
		r.recordErrorFinding(r.findingAttrs("0", fmt.Sprintf("%.6f", result.costUSD)), &autoComplianceFinding{
			Summary: "compliance error: parse failure",
			Notes:   fmt.Sprintf("raw response: %s", truncateStr(result.text, 500)),
		})
		return fmt.Errorf("compliance %s: JSON parse failure: %w", featureID, err)
	}

	// Step 11: --preview: print findings to stdout, no HTML write.
	attrs := r.findingAttrs(strconv.Itoa(finding.Score), fmt.Sprintf("%.6f", result.costUSD))
	body := renderFindingsHTML(finding)

	if flags.preview {
		fmt.Println(body)
		return nil
	}

	// Step 12: Write compliance findings section atomically.
	if err := writeComplianceSection(r.featurePath, attrs, body); err != nil {
		return fmt.Errorf("write compliance section: %w", err)
	}

	// Step 13: Print one-line summary.
	staleNote := ""
	if r.prevSpecHash != "" && r.prevSpecHash != r.specHash {
		staleNote = " [stale-spec]"
	}
	fmt.Printf("compliance %s score=%d cost=$%.6f%s\n", featureID, finding.Score, result.costUSD, staleNote)

	return nil
}

// headlessRequest builds the headless claude request for the prompts.
func (r *complianceRun) headlessRequest(systemPrompt, userPrompt string) headlessRequest {
	flags := r.flags
	return headlessRequest{
		model:        flags.model,
		effort:       flags.effort,
		maxTurns:     flags.maxTurns,
		maxBudgetUSD: 0.50,
		maxWallClock: flags.maxWallClock,
		systemPrompt: systemPrompt,
		userPrompt:   userPrompt,
	}
}

// invokeFailed records a budget-exceeded or timeout finding and returns the
// headless invocation error.
func (r *complianceRun) invokeFailed(err error) error {
	featureID := r.featureID
	if budgetErr, ok := err.(*BudgetExceededError); ok {
		r.recordErrorFinding(r.findingAttrs("0", "0"), &autoComplianceFinding{
			Summary: "compliance error: budget exceeded",
			Notes:   budgetErr.Error(),
		})
		return fmt.Errorf("compliance %s: budget exceeded: %w", featureID, budgetErr)
	}
	if strings.Contains(err.Error(), "timeout") {
		r.recordErrorFinding(r.findingAttrs("0", "0"), &autoComplianceFinding{
			Summary: "compliance error: wall-clock timeout",
			Notes:   err.Error(),
		})
		return fmt.Errorf("compliance %s: timeout: %w", featureID, err)
	}
	return fmt.Errorf("compliance %s: headless invoke: %w", featureID, err)
}
