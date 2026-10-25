package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/internal/launcher/plan"
)

// emitYoloDirtyMainMessage carries the canonical main repo's uncommitted tracked
// changes into a freshly-created worktree and prints an accurate dirty-main
// advisory (bug-bcf8a311 + bug-7d4b6c63).
//
// Delegates to the shared emitWorktreeCarryoverMessage helper in worktree_helpers.go
// so that yolo and claude launch paths share identical carryover semantics.
func emitYoloDirtyMainMessage(p plan.LaunchPlan, canonicalRoot, worktreePath string, created bool, w io.Writer) {
	emitWorktreeCarryoverMessage(p, canonicalRoot, worktreePath, created, w)
}

// yoloDefaultRoots resolves the project root and the canonical main repo root
// when CWD is a linked worktree (slice-3). Do NOT overwrite projectRoot: it
// stays the child working directory for --no-worktree/planning launches. Only
// WIPNOTE_PROJECT_DIR (WipnoteRoot) and worktree creation use the canonical
// main root.
func yoloDefaultRoots() (projectRoot, canonicalRoot string) {
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectRoot = filepath.Dir(wipnoteDir)
	}
	canonicalRoot = projectRoot
	if canonical := canonicalProjectRoot(projectRoot); canonical != "" {
		canonicalRoot = canonical
	}
	return projectRoot, canonicalRoot
}

// resolveYoloDevPluginDir resolves the plugin from local source, NOT the
// marketplace, and checks wipnote is on PATH.
func resolveYoloDevPluginDir() (string, error) {
	pluginDir := resolveProjectPluginDir()
	if pluginDir == "" {
		return "", fmt.Errorf("could not find plugin/ directory relative to project root. Run from the project directory containing .wipnote/ and plugin/")
	}
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); os.IsNotExist(err) {
		return "", fmt.Errorf("plugin.json not found at %s",
			filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	}
	if err := requireWipnoteOnPath(); err != nil {
		return "", err
	}
	return pluginDir, nil
}

// yoloIsolation is a validated YOLO work item and the isolation resolved for
// it.
type yoloIsolation struct {
	id, kind, workDir  string
	launchPlan         plan.LaunchPlan
	willCreateWorktree bool
}

// isolateYoloWorkItem validates the work item, honors the isolation plan and
// creates the isolation worktree when one is needed.
func isolateYoloWorkItem(trackID, featureID, canonicalRoot, projectRoot string, noWorktree bool) (*yoloIsolation, error) {
	// Validate the provided work item exists.
	id, kind, err := validateWorkItem(trackID, featureID, canonicalRoot)
	if err != nil {
		return nil, err
	}

	// A managed worktree is created for isolation unless --in-place/--no-worktree
	// was passed (noWorktree) or we could not resolve a canonical root. When a
	// worktree WILL be created, suppress the plan's generic dirty-main advisory
	// (which wrongly tells the user to "use a managed worktree (--work-item <id>)"
	// — that's already happening) and emit an accurate message after carryover
	// instead (bug-7d4b6c63).
	willCreateWorktree := !noWorktree && canonicalRoot != ""

	// Honor the isolation plan (slice-9): a RefuseLaunch plan aborts before the
	// harness starts. The validated work item id drives the dirty-main/enforce guard.
	launchPlan := applyLaunchPlanOpts(canonicalRoot, projectRoot, id, noWorktree, willCreateWorktree, os.Stderr)
	if err := enforceLaunchPlan(launchPlan, os.Stderr); err != nil {
		return nil, err
	}

	// Resolve track title once — used for both the session name and the worktree directory.
	trackTitle := resolveTrackTitle(trackID, featureID, canonicalRoot)

	// Create a worktree for isolation (skip for --no-worktree). Worktrees are
	// always created under the canonical main root, never a linked worktree copy.
	workDir, err := yoloWorkDir(launchPlan, willCreateWorktree, trackID, featureID, trackTitle, canonicalRoot, projectRoot)
	if err != nil {
		return nil, err
	}
	return &yoloIsolation{id: id, kind: kind, workDir: workDir, launchPlan: launchPlan, willCreateWorktree: willCreateWorktree}, nil
}

// launch names the session, prints the YOLO banner and launches Claude with
// the YOLO system prompt.
func (iso *yoloIsolation) launch(headline, trackID, featureID, name, canonicalRoot string, opts LaunchOpts) error {
	sessionName := yoloSessionName(name, opts.ResumeID, trackID, featureID, canonicalRoot)
	yoloPrompt := buildYoloSystemPrompt(iso.id, iso.kind)

	emitYoloBanner(headline, opts.PluginDir, sessionName, iso.id,
		joinYoloWarnings(yoloPermissionWarning(opts.PermissionMode), bannerDirtyWarning(iso.launchPlan, iso.willCreateWorktree)), os.Stdout)

	opts.Name = sessionName
	return launchYoloClaude(opts, yoloPrompt)
}

// yoloWorkDir returns the session working directory: projectRoot, or the
// isolation worktree when one will be created.
//
// After creating it, carry the canonical main repo's uncommitted TRACKED
// changes into the freshly-created worktree so the session builds on the
// user's latest WORKING state (bug-bcf8a311). Only for NEWLY-created
// worktrees: a reused worktree may already contain (committed or carried)
// work and re-applying would double-apply or fail. Main is never mutated.
// Carryover failure is non-fatal: the launch proceeds and the changes remain
// on main.
func yoloWorkDir(launchPlan plan.LaunchPlan, willCreateWorktree bool, trackID, featureID, trackTitle, canonicalRoot, projectRoot string) (string, error) {
	workDir := projectRoot
	worktreeCreated := false
	if willCreateWorktree {
		if wt, created, ok, wtErr := ensureYoloWorktree(trackID, featureID, trackTitle, canonicalRoot); wtErr != nil {
			return "", wtErr
		} else if ok {
			workDir = wt
			worktreeCreated = created
		}
		emitYoloDirtyMainMessage(launchPlan, canonicalRoot, workDir, worktreeCreated, os.Stdout)
	}
	return workDir, nil
}

// ensureYoloWorktree creates (or reuses) the isolation worktree for the work
// item under canonicalRoot: the titled track worktree for a track, or for a
// feature its parent track's worktree, else the feature's own. ok is false
// when neither a track nor a feature was given.
func ensureYoloWorktree(trackID, featureID, trackTitle, canonicalRoot string) (workDir string, created, ok bool, err error) {
	if trackID != "" {
		worktreePath, created, wtErr := EnsureForTrackWithTitleStatus(trackTitle, trackID, canonicalRoot, os.Stdout)
		if wtErr != nil {
			return "", false, false, wtErr
		}
		return worktreePath, created, true, nil
	} else if featureID != "" {
		// Resolve the parent track so features use the titled track worktree.
		parentTrackID := resolveTrackForFeature(featureID, canonicalRoot)
		if parentTrackID != "" {
			parentTitle := resolveTrackTitle(parentTrackID, "", canonicalRoot)
			worktreePath, created, wtErr := EnsureForTrackWithTitleStatus(parentTitle, parentTrackID, canonicalRoot, os.Stdout)
			if wtErr != nil {
				return "", false, false, wtErr
			}
			return worktreePath, created, true, nil
		}
		worktreePath, created, wtErr := EnsureForFeatureStatus(featureID, canonicalRoot, os.Stdout)
		if wtErr != nil {
			return "", false, false, wtErr
		}
		return worktreePath, created, true, nil
	}
	return "", false, false, nil
}

// yoloSessionName returns the session name. Only synthesize a default name
// for new sessions. When resuming an existing session, skip default-name
// generation so we don't rename or conflict with the resumed session. The
// user can still override with an explicit --name.
func yoloSessionName(name, resumeID, trackID, featureID, canonicalRoot string) string {
	sessionName := name
	if sessionName == "" && resumeID == "" {
		sessionName = yoloDefaultName(trackID, featureID, canonicalRoot)
	}
	return sessionName
}

// launchYoloClaude writes the combined prompt to a temp file so launchClaude
// can pass it via --append-system-prompt-file without needing a new field,
// then launches Claude with the YOLO launch env.
func launchYoloClaude(opts LaunchOpts, yoloPrompt string) error {
	tmpFile, err := os.CreateTemp("", "yolo-prompt-*.md")
	if err != nil {
		return fmt.Errorf("could not create temp prompt file: %w", err)
	}
	defer os.Remove(tmpFile.Name())
	if _, err := tmpFile.WriteString(yoloPrompt); err != nil {
		return fmt.Errorf("could not write temp prompt file: %w", err)
	}
	tmpFile.Close()

	opts.SystemPromptFile = tmpFile.Name()
	opts.ExtraEnv = yoloLaunchEnv()
	return launchClaude(opts)
}

func launchYoloDefault(permMode, trackID, featureID string, noWorktree bool, resumeID, name string, extraArgs []string) error {
	projectRoot, canonicalRoot := yoloDefaultRoots()

	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}

	// No work item provided — fall back to planning mode. The child cwd is the
	// (un-rewritten) projectRoot per the slice-3 contract.
	// NOTE: planning mode intentionally bypasses the isolation plan (applyLaunchPlanOpts).
	// Planning mode is lightweight, designed to help create work items without enforcing
	// the project's launch_isolation config. This means "launch_isolation: auto" does NOT
	// create an ad-hoc managed worktree in planning mode. Once a work item is created,
	// the user restarts with --track/--feature, and the isolation plan applies normally.
	if trackID == "" && featureID == "" {
		_ = computeLauncherMode("", false, false)
		return launchYoloPlanningMode(projectRoot, extraArgs)
	}

	iso, err := isolateYoloWorkItem(trackID, featureID, canonicalRoot, projectRoot, noWorktree)
	if err != nil {
		return err
	}
	workDir := iso.workDir

	// Compute launcher mode for preflight logging AFTER the effective worktree
	// path is resolved (slice-1 fix): passing the resolved workDir means
	// WIPNOTE_DEBUG reports execution=worktree for isolated YOLO instead of
	// always reporting execution=in-place.
	effectiveWorktree := ""
	if workDir != projectRoot {
		effectiveWorktree = workDir
	}
	_ = computeLauncherMode(effectiveWorktree, false, false)

	return iso.launch(fmt.Sprintf("Launching Claude Code in YOLO mode (%s)...", permMode), trackID, featureID, name, canonicalRoot, LaunchOpts{
		Mode:           "yolo",
		PluginDir:      pluginDir,
		ResumeID:       resumeID,
		PermissionMode: permMode,
		ExtraArgs:      extraArgs,
		ProjectRoot:    workDir,
		WipnoteRoot:    canonicalRoot,
	})
}

func launchYoloDev(permMode, trackID, featureID string, noWorktree bool, resumeID, name string, extraArgs []string) error {
	pluginDir, err := resolveYoloDevPluginDir()
	if err != nil {
		return err
	}

	projectRoot := ""
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectRoot = filepath.Dir(wipnoteDir)
	}

	// No work item provided — fall back to planning mode.
	if trackID == "" && featureID == "" {
		return launchYoloPlanningMode(projectRoot, extraArgs)
	}

	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	canonicalRoot := canonicalProjectRoot(projectRoot)
	if canonicalRoot == "" {
		canonicalRoot = projectRoot
	}

	// Create a worktree for isolation (skip for --no-worktree), mirroring the
	// default path. Use canonicalRoot for all worktree operations so that
	// launching from a linked worktree doesn't create nested worktrees or set
	// WipnoteRoot to the linked root.
	iso, err := isolateYoloWorkItem(trackID, featureID, canonicalRoot, projectRoot, noWorktree)
	if err != nil {
		return err
	}

	// Remove any installed marketplace plugin after isolation resolution succeeds
	// so --plugin-dir cannot be shadowed by a stale install.
	removeMarketplaceWipnote()

	return iso.launch(fmt.Sprintf("Launching Claude Code in YOLO dev mode (%s)...", permMode), trackID, featureID, name, canonicalRoot, LaunchOpts{
		Mode:           "yolo-dev",
		PluginDir:      pluginDir,
		ResumeID:       resumeID,
		PermissionMode: permMode,
		ExtraArgs:      extraArgs,
		ProjectRoot:    iso.workDir,
		WipnoteRoot:    canonicalRoot,
	})
}
