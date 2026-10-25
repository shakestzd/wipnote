package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/plan/planyaml"
	"github.com/spf13/cobra"
)

// stderr is the writer used for diagnostic output from plan-commit helpers.
// Tests override it to capture output without racing on the process-global os.Stderr.
var stderr io.Writer = os.Stderr

// commitPlanChange stages and commits the plan YAML and HTML together as an
// atomic mutation record. The plan YAML is the source of truth; the HTML is
// a rendered view derived from it. Both must be committed atomically so git
// history becomes the full audit trail of plan state changes (bug-9ec0cf31).
//
// If the project is not inside a git repo, the function logs a skip and
// returns nil — this makes it test-compatible without requiring every plan
// test to set up a git repo.
//
// Pre-commit hooks run. --no-verify is deliberately NOT used. If a hook
// rejects the commit, the function logs a non-fatal warning and returns nil
// — the mutation is already persisted to disk; the caller reports success and
// the user is directed to commit manually (Fix 1 of bug-365a84d9). Only
// staging and filesystem errors are fatal.
//
// HTML is always re-rendered from YAML before staging, so callers that only
// write YAML (add-slice-yaml, add-question-yaml, set-design-yaml, etc.) never
// commit stale HTML (Fix 2 of bug-365a84d9).
func commitPlanChange(planPath, message string) error {
	htmlPath := strings.TrimSuffix(planPath, ".yaml") + ".html"

	// Detect git repo. Uses the plan file's directory as the cwd.
	planDir := filepath.Dir(planPath)
	if !isGitRepo(planDir) {
		// Not in a git repo — silent skip. This is normal in tests and in
		// non-git projects. Log to stderr for diagnosability.
		fmt.Fprintf(stderr, "autocommit skipped: %s is not inside a git repository\n", planDir)
		return nil
	}

	// Re-render HTML from YAML before staging so commits always contain a fresh
	// view — even when the caller only mutated the YAML (Fix 2, bug-365a84d9).
	// Derive wipnoteDir and planID from the path: .../plans/<planID>.yaml
	planID := strings.TrimSuffix(filepath.Base(planPath), ".yaml")
	wipnoteDir := filepath.Dir(filepath.Dir(planPath)) // .../plans/.. → wipnote dir
	if err := renderPlanToFileQuiet(wipnoteDir, planID); err != nil {
		return fmt.Errorf("autocommit: re-render HTML: %w", err)
	}

	// Resolve the true repo root so runGitMutationBatch keys the advisory lock on
	// the same path used by work-item artifact commits. Using planDir
	// (e.g. .wipnote/plans) would produce a different lock-file hash and defeat
	// the serialization guarantee (roborev finding on feat-c0307d7a).
	repoRoot, err := paths.GitTopLevel(planDir)
	if err != nil {
		repoRoot = planDir // fallback: best-effort, still serialized within itself
	}

	// Absolutize the file paths. runGitMutationBatch anchors with `git -C <repoRoot>`,
	// so a relative planPath/htmlPath (e.g. from `--project-dir relative/repo`)
	// would be resolved against repoRoot and fail to stage. Absolute paths are
	// anchor-independent (roborev finding job 3633).
	addPlanPath, htmlAbs := planPath, htmlPath
	if abs, aerr := filepath.Abs(planPath); aerr == nil {
		addPlanPath = abs
	}
	if abs, aerr := filepath.Abs(htmlPath); aerr == nil {
		htmlAbs = abs
	}

	// Stage both files then commit under ONE advisory-lock acquisition so the
	// staged-index transaction cannot interleave with another wipnote git
	// mutation (roborev #3713). Explicit absolute paths — never `git add -A`.
	// Inside the lock we call runGitWithLockRetry directly (the external-writer
	// backoff layer), not runGitMutationBatch (which would re-acquire the lock).
	var resultErr error
	_, lockErr := withGitMutationLock(repoRoot, func() ([]byte, error) {
		if out, err := runGitWithLockRetry(repoRoot, "add", "--", addPlanPath, htmlAbs); err != nil {
			resultErr = fmt.Errorf("autocommit: git add failed: %s: %w", strings.TrimSpace(string(out)), err)
			return nil, err
		}
		// Commit. No --no-verify. Pre-commit hooks run.
		commitOut, err := runGitWithLockRetry(repoRoot, "commit", "-m", message, "--", addPlanPath, htmlAbs)
		if err != nil {
			outStr := string(commitOut)
			// "nothing to commit" (e.g. re-finalize with unchanged YAML) is a no-op.
			if strings.Contains(outStr, "nothing to commit") || strings.Contains(outStr, "no changes added") {
				return nil, nil
			}
			// Any other commit failure (hook rejection, locked index) is non-fatal:
			// the mutation is persisted to disk; warn and let the command report
			// success rather than rolling back on a git concern (Fix 1 of bug-365a84d9).
			fmt.Fprintf(stderr, "autocommit warning: git commit failed (mutation persisted to disk — please commit manually): %s\n", strings.TrimSpace(outStr))
		}
		return nil, nil
	})
	// Propagate a lock-wrapper/callback error if none was captured (#3721).
	if resultErr == nil {
		resultErr = lockErr
	}
	return resultErr
}

// isGitRepo returns true if the given directory is inside a git repository.
func isGitRepo(dir string) bool {
	err := exec.Command("git", "-C", dir, "rev-parse", "--git-dir").Run()
	return err == nil
}

// planCreateYAMLCmd creates a YAML plan file with empty design, slices,
// questions, and nil critique. This is the YAML counterpart of "plan create".
func planCreateYAMLCmd() *cobra.Command {
	var description string
	var trackID string

	cmd := &cobra.Command{
		Use:   "create-yaml <title>",
		Short: "Create a YAML plan file",
		Long: `Create a plan file in YAML format with empty design, slices,
questions, and no critique section.

Unlike the HTML 'plan create', this produces a machine-readable YAML file
suitable for programmatic editing by agents and scripts.

Example:
  wipnote plan create-yaml "Auth Middleware Rewrite" --description "Rewrite for compliance" --track trk-abc12345`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runPlanCreateYAML(args[0], description, trackID)
		},
	}
	cmd.Flags().StringVar(&description, "description", "", "plan description")
	cmd.Flags().StringVar(&trackID, "track", "", "parent track ID (e.g. trk-abc12345)")
	return cmd
}

// runPlanCreateYAML generates a YAML plan file and prints its path.
func runPlanCreateYAML(title, description, trackID string) error {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	planID := workitem.GenerateID("plan", title)
	plan := planyaml.NewPlan(planID, title, description)

	if trackID != "" {
		plan.Meta.TrackID = trackID
	}

	plansDir := filepath.Join(wipnoteDir, "plans")
	if err := os.MkdirAll(plansDir, 0o755); err != nil {
		return fmt.Errorf("create plans dir: %w", err)
	}

	outPath := filepath.Join(plansDir, planID+".yaml")
	if err := planyaml.Save(outPath, plan); err != nil {
		return fmt.Errorf("save plan YAML: %w", err)
	}

	if err := commitPlanChange(outPath, fmt.Sprintf("plan(%s): create — %s", planID, title)); err != nil {
		return fmt.Errorf("autocommit create: %w", err)
	}

	fmt.Println(outPath)
	return nil
}

// planAddSliceYAMLCmd appends a typed slice to an existing YAML plan file.
func planAddSliceYAMLCmd() *cobra.Command {
	var what, why, files, doneWhen, tests, effort, risk, deps string

	cmd := &cobra.Command{
		Use:   "add-slice-yaml <plan-id> <title>",
		Short: "Append a typed slice to a YAML plan file",
		Long: `Append a new delivery slice to an existing YAML plan file.
The slice num is auto-assigned as len(slices)+1. The slice id is generated
from the title. Files and done-when are comma-separated lists. Deps is a
comma-separated list of slice nums (integers).

Example:
  wipnote plan add-slice-yaml plan-abc12345 "Auth Middleware" \
    --what "Implement JWT middleware" \
    --why "Required for compliance" \
    --files "cmd/main.go,internal/auth.go" \
    --done-when "Tests pass,CI green" \
    --tests "Unit: TestAuth\nIntegration: TestAuthFlow" \
    --effort M \
    --risk Low \
    --deps "1,2"`,
		Args: cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			wipnoteDir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runPlanAddSliceYAML(wipnoteDir, args[0], args[1],
				what, why, files, doneWhen, tests, effort, risk, deps)
		},
	}

	cmd.Flags().StringVar(&what, "what", "", "what to implement (required)")
	cmd.Flags().StringVar(&why, "why", "", "why this slice matters")
	cmd.Flags().StringVar(&files, "files", "", "comma-separated list of file paths")
	cmd.Flags().StringVar(&doneWhen, "done-when", "", "comma-separated done criteria")
	cmd.Flags().StringVar(&tests, "tests", "", "test description")
	cmd.Flags().StringVar(&effort, "effort", "S", "effort estimate: S, M, or L")
	cmd.Flags().StringVar(&risk, "risk", "Low", "risk level: Low, Med, or High")
	cmd.Flags().StringVar(&deps, "deps", "", "comma-separated slice nums this slice depends on")

	return cmd
}

// runPlanAddSliceYAML loads the YAML plan, validates inputs, builds a PlanSlice,
// appends it, and saves. Called by the CLI command and directly by tests.
func runPlanAddSliceYAML(wipnoteDir, planID, title, what, why, files, doneWhen, tests, effort, risk, deps string) error {
	if what == "" {
		return fmt.Errorf("--what is required")
	}

	validEffort := map[string]bool{"S": true, "M": true, "L": true}
	if !validEffort[effort] {
		return fmt.Errorf("--effort must be S, M, or L (got %q)", effort)
	}

	validRisk := map[string]bool{"Low": true, "Med": true, "High": true}
	if !validRisk[risk] {
		return fmt.Errorf("--risk must be Low, Med, or High (got %q)", risk)
	}

	planPath := filepath.Join(wipnoteDir, "plans", planID+".yaml")

	// Hold the lock across the whole load→mutate→save window (defect 4,
	// feat-fc3cc9e0) — see storePlanFeedbackEntry for the canonical pattern
	// this mirrors.
	releaseFile := filelock.Guard(planPath)
	defer releaseFile()
	releasePlan := planyaml.LockPlanForWrite(planPath)
	defer releasePlan()

	plan, err := planyaml.Load(planPath)
	if err != nil {
		return fmt.Errorf("load plan %q: %w", planID, err)
	}

	var fileList []string
	if files != "" {
		for _, f := range strings.Split(files, ",") {
			if s := strings.TrimSpace(f); s != "" {
				fileList = append(fileList, s)
			}
		}
	}

	var doneWhenList []string
	if doneWhen != "" {
		for _, d := range strings.Split(doneWhen, ",") {
			if s := strings.TrimSpace(d); s != "" {
				doneWhenList = append(doneWhenList, s)
			}
		}
	}

	var depsList []int
	if deps != "" {
		for _, d := range strings.Split(deps, ",") {
			s := strings.TrimSpace(d)
			if s == "" {
				continue
			}
			n, parseErr := strconv.Atoi(s)
			if parseErr != nil {
				return fmt.Errorf("--deps: %q is not a valid integer: %w", s, parseErr)
			}
			depsList = append(depsList, n)
		}
	}

	slice := planyaml.PlanSlice{
		ID:       workitem.GenerateID("slice", title),
		Num:      len(plan.Slices) + 1,
		Title:    title,
		What:     what,
		Why:      why,
		Files:    fileList,
		Deps:     depsList,
		DoneWhen: doneWhenList,
		Effort:   effort,
		Risk:     risk,
		Tests:    tests,
	}

	plan.Slices = append(plan.Slices, slice)

	if err := planyaml.SaveLocked(planPath, plan); err != nil {
		return fmt.Errorf("save plan %q: %w", planID, err)
	}

	if err := commitPlanChange(planPath, fmt.Sprintf("plan(%s): add slice %d — %s", planID, slice.Num, title)); err != nil {
		return fmt.Errorf("autocommit add-slice: %w", err)
	}

	fmt.Printf("Slice %d added\n", slice.Num)
	return nil
}

func openPlanDB(wipnoteDir string) (*sql.DB, error) {
	return openDB(wipnoteDir)
}
