package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/shakestzd/wipnote/internal/launcher"
	"github.com/shakestzd/wipnote/internal/launcher/plan"
)

// launchCodexDefault launches Codex interactively with wipnote env injection.
// Corresponds to: wipnote codex
//
// Phase B: if no marketplace is registered, auto-register the bundled local
// marketplace tree resolved via resolveSharedTreePath("codex-marketplace").
// This replaces the historical "user must run --init first" path with the
// brew/curl-install bundled tree.
type codexMarketplaceSource int

const (
	codexMarketplaceSourceBundled codexMarketplaceSource = iota
	codexMarketplaceSourceDev
)

func launchCodexDefault(resumeID, trackID, featureID, worktreePath, workItem string, noWorktree, yolo bool, extraArgs []string) error {
	return launchCodexDefaultWithMarketplace(resumeID, trackID, featureID, worktreePath, workItem, noWorktree, yolo, extraArgs, codexMarketplaceSourceBundled, false, false)
}

func launchCodexDefaultWithMarketplace(resumeID, trackID, featureID, worktreePath, workItem string, noWorktree, yolo bool, extraArgs []string, source codexMarketplaceSource, cleanup, dryRun bool) error {
	l := &codexDefaultLaunch{
		resumeID: resumeID, trackID: trackID, featureID: featureID,
		worktreePath: worktreePath, workItem: workItem,
		noWorktree: noWorktree, yolo: yolo, extraArgs: extraArgs,
	}
	if err := l.resolveIntent(); err != nil {
		return err
	}

	// Apply isolation plan and HONOR it (slice-9): a RefuseLaunch plan aborts
	// before Codex starts. noWorktree here is effectiveInPlace (--in-place || --no-worktree).
	// When the plan will create a managed worktree, suppress the generic dirty-main
	// advisory — we emit an accurate message after carryover instead (bug-938e56ae).
	willCreateWorktree := !l.noWorktree && (l.trackID != "" || l.featureID != "" || l.workItem != "")
	launchPlan := applyLaunchPlanOpts(l.canonicalRoot, l.projectRoot, effectiveWorkItemID(l.workItem, l.trackID, l.featureID), l.noWorktree, willCreateWorktree, os.Stderr)
	if err := enforceLaunchPlan(launchPlan, os.Stderr); err != nil {
		return err
	}
	configPath := codexConfigPath()

	setupDetails, err := prepareCodexMarketplace(configPath, source, dryRun)
	if err != nil {
		return err
	}

	if source == codexMarketplaceSourceDev && dryRun {
		l.printDryRun(launchPlan, setupDetails)
		return nil
	}
	// Work item attribution: emit `wipnote feature start <id>` before launching.
	if l.workItem != "" {
		if err := runCodexFeatureStartFn(l.workItem); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not start work item %s: %v\n", l.workItem, err)
		}
	}

	workDir, wipnoteRoot, err := l.resolveWorkDir(launchPlan)
	if err != nil {
		return err
	}

	err = l.exec(launchPlan, setupDetails, willCreateWorktree, workDir, wipnoteRoot)
	if source == codexMarketplaceSourceDev && cleanup && !dryRun {
		cleanupCodexDevRegistrations(configPath)
	}
	return err
}

// codexDefaultLaunch is the state of one launchCodexDefaultWithMarketplace
// call: the requested target, refined by the chooser intent and the continue
// context.
type codexDefaultLaunch struct {
	resumeID, trackID, featureID, worktreePath, workItem string
	noWorktree, yolo                                     bool
	extraArgs                                            []string

	projectRoot, canonicalRoot string
	intentResult               codexIntentResult
	continueCtx                continueLaunchContext
}

// resolveIntent resolves the project roots, the chooser intent and the
// continue context, refining resumeID, worktreePath and workItem.
func (l *codexDefaultLaunch) resolveIntent() error {
	projectRoot, _ := resolveProjectRoot()
	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	canonicalRoot := canonicalProjectRoot(projectRoot)
	if canonicalRoot == "" {
		canonicalRoot = projectRoot
	}
	l.projectRoot, l.canonicalRoot = projectRoot, canonicalRoot
	intent, err := resolveLaunchIntentForDefaultLaunch(projectRoot, canonicalRoot, "codex", chooserEligibility{
		TTY:       isInteractiveTerminalFile(os.Stdin) && isInteractiveTerminalFile(os.Stdout),
		CI:        os.Getenv("CI") == "true" || os.Getenv("GITHUB_ACTIONS") != "",
		ResumeID:  l.resumeID,
		WorkItem:  l.workItem,
		Targeted:  l.trackID != "" || l.featureID != "" || l.worktreePath != "",
		InPlace:   l.noWorktree,
		Yolo:      l.yolo,
		ExtraArgs: l.extraArgs,
	}, os.Stdin, os.Stdout)
	if err != nil {
		return err
	}
	l.intentResult = applyCodexLaunchIntent(l.worktreePath, l.workItem, l.resumeID, l.yolo, intent)
	l.resumeID = l.intentResult.resumeID
	l.worktreePath = l.intentResult.worktreePath
	l.workItem = l.intentResult.workItem
	l.continueCtx, err = resolveContinueLaunchContext(projectRoot, canonicalRoot, "codex", intent)
	if err != nil {
		return err
	}
	l.applyContinueContext(intent)
	return nil
}

// applyContinueContext folds the continue context's warnings, work item,
// worktree and transcript into the launch.
func (l *codexDefaultLaunch) applyContinueContext(intent launcher.LaunchIntent) {
	continueCtx := l.continueCtx
	for _, warning := range continueCtx.Warnings {
		fmt.Fprintln(os.Stderr, warning)
	}
	if l.workItem == "" && continueCtx.WorkItemID != "" {
		l.workItem = continueCtx.WorkItemID
	}
	// When continuing, always prefer the validated/normalized worktree path from
	// resolveContinueLaunchContext (which absolutizes relative paths and clears
	// stale/missing ones). The chooser-derived worktreePath may be relative or
	// stale — resolveContinueLaunchContext has already validated it as one of its
	// inputs, so its result is authoritative. An empty continueCtx.WorktreePath
	// means validation failed; we clear the stale chooser path rather than
	// launching in a missing directory.
	if intent.WantsContinue() {
		l.worktreePath = continueCtx.WorktreePath
	} else if l.worktreePath == "" && continueCtx.WorktreePath != "" {
		l.worktreePath = continueCtx.WorktreePath
	}
	if l.resumeID == "" && continueCtx.TranscriptResumeID != "" {
		l.resumeID = continueCtx.TranscriptResumeID
	}
}

// resolveWorkDir resolves the directory Codex runs in and the root injected
// as WIPNOTE_PROJECT_DIR, creating a worktree when the target needs one.
//
// Note: canonicalRoot was already computed and used for the isolation plan.
// It is also the value injected as WIPNOTE_PROJECT_DIR AND used as the base
// for worktree creation.
func (l *codexDefaultLaunch) resolveWorkDir(launchPlan plan.LaunchPlan) (workDir, wipnoteRoot string, err error) {
	canonicalRoot := l.canonicalRoot
	workDir, wipnoteRoot, worktreeCreated, resolved, err := l.resolveTargetWorktree()
	if err != nil {
		return "", "", err
	}

	// Honor a managed-worktree plan (slice-9) when no explicit/track/feature
	// worktree was resolved above. WIPNOTE_PROJECT_DIR stays the canonical root.
	if wt, created, werr := resolveManagedWorktreeStatusFn(launchPlan, canonicalRoot, l.trackID, l.featureID, l.workItem, workDir, resolved, os.Stdout); werr != nil {
		return "", "", werr
	} else if wt != "" && wt != workDir {
		workDir = wt
		wipnoteRoot = canonicalRoot
		worktreeCreated = created
	}

	// Carry the canonical main repo's uncommitted tracked changes into the
	// freshly-created worktree so the session builds on the user's latest
	// working state (bug-938e56ae). Only for newly-created worktrees: a reused
	// worktree may already contain prior work and re-applying would double-apply
	// or fail. Main is never mutated. Carryover failure is non-fatal.
	if workDir != l.projectRoot {
		effectiveRoot := canonicalRoot
		emitWorktreeCarryoverMessage(launchPlan, effectiveRoot, workDir, worktreeCreated, os.Stdout)
	}
	return workDir, wipnoteRoot, nil
}

// resolveTargetWorktree resolves an explicit worktree path, or the track or
// feature worktree when one was requested.
func (l *codexDefaultLaunch) resolveTargetWorktree() (workDir, wipnoteRoot string, created, resolved bool, err error) {
	canonicalRoot := l.canonicalRoot
	workDir = l.projectRoot
	wipnoteRoot = canonicalRoot
	switch {
	case l.worktreePath != "":
		// Explicit path — use as-is; set WIPNOTE_PROJECT_DIR to canonical root.
		return l.worktreePath, canonicalRoot, false, true, nil
	case !l.noWorktree && l.trackID != "":
		wt, created, err := ensureForTrackStatusFn(l.trackID, canonicalRoot, os.Stdout)
		if err != nil {
			return "", "", false, false, err
		}
		return wt, canonicalRoot, created, true, nil
	case !l.noWorktree && l.featureID != "":
		wt, created, err := ensureForFeatureStatusFn(l.featureID, canonicalRoot, os.Stdout)
		if err != nil {
			return "", "", false, false, err
		}
		return wt, canonicalRoot, created, true, nil
	}
	return workDir, wipnoteRoot, false, false, nil
}

// printDryRun prints the dev dry-run banner and the exec it would run.
func (l *codexDefaultLaunch) printDryRun(launchPlan plan.LaunchPlan, setupDetails []launchtui.BannerDetail) {
	previewTarget := plannedCodexLaunchTarget(launchPlan, l.worktreePath, l.trackID, l.featureID, l.workItem, l.noWorktree, l.canonicalRoot)
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline: "Launching Codex CLI with wipnote context (dev, dry-run)...",
		Details:  setupDetails,
	}))
	fmt.Printf("[dry-run] would exec: codex (resume=%q, target=%s) in %s\n", l.resumeID, previewTarget, l.projectRoot)
}

// exec prints the launch banner and execs Codex in workDir.
func (l *codexDefaultLaunch) exec(launchPlan plan.LaunchPlan, setupDetails []launchtui.BannerDetail, willCreateWorktree bool, workDir, wipnoteRoot string) error {
	// Render a single launch banner combining prep/setup details and the
	// launch headline — mirrors claude.go's one-banner-per-launch-path idiom
	// (bug-2a6a8076). The sandbox-degradation warning fires inside execCodexFn
	// and is the only banner that may follow (conditional, visually distinct).
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        "Launching Codex CLI with wipnote context...",
		Details:         setupDetails,
		Warning:         bannerDirtyWarning(launchPlan, willCreateWorktree),
		WarningSeverity: "amber",
	}))
	return execCodexFn(codexLaunchOpts{
		ResumeID:     l.resumeID,
		ExtraArgs:    l.extraArgs,
		ProjectRoot:  workDir,
		WorktreeRoot: workDir,
		WipnoteRoot:  wipnoteRoot,
		Mode:         l.intentResult.mode,
		Yolo:         l.yolo,
		ExtraEnv:     l.continueCtx.ExtraEnv(),
	})
}

// cleanupCodexDevRegistrations removes the local marketplace registration a
// dev launch added.
func cleanupCodexDevRegistrations(configPath string) {
	fmt.Println("Cleaning up local marketplace registration...")
	removed, rmErr := removeCodexWipnoteRegistrations(configPath)
	if rmErr != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove marketplace registration: %v\n", rmErr)
	} else if !removed {
		fmt.Println("No wipnote registrations found to clean up.")
	}
}

type codexIntentResult struct {
	mode         codexLaunchMode
	resumeID     string
	worktreePath string
	workItem     string
}

func applyCodexLaunchIntent(worktreePath, workItem, resumeID string, yolo bool, intent launcher.LaunchIntent) codexIntentResult {
	mode := codexLaunchModeDefault
	if yolo {
		mode = codexLaunchModeYolo
	}
	result := codexIntentResult{
		mode:         mode,
		resumeID:     resumeID,
		worktreePath: worktreePath,
		workItem:     workItem,
	}
	if !intent.WantsContinue() {
		return result
	}
	if yolo {
		result.mode = codexLaunchModeYoloCont
	} else {
		result.mode = codexLaunchModeContinue
	}
	if result.workItem == "" && intent.WorkItemID != "" {
		result.workItem = intent.WorkItemID
	}
	if result.worktreePath == "" && intent.WorktreePath != "" {
		result.worktreePath = intent.WorktreePath
	}
	if result.resumeID == "" {
		result.resumeID = intent.ResumeForHarness("codex")
	}
	return result
}

// runFeatureStart runs `wipnote feature start <id>` for work item attribution.
func runFeatureStart(id string) error {
	return runFeatureStartWithEnv(id, nil)
}

func runCodexFeatureStart(id string) error {
	return runFeatureStartWithEnv(id, buildCodexAgentEnv(nil))
}

var (
	runCodexFeatureStartFn         = runCodexFeatureStart
	ensureForTrackStatusFn         = EnsureForTrackStatus
	ensureForFeatureStatusFn       = EnsureForFeatureStatus
	resolveManagedWorktreeStatusFn = resolveManagedWorktreeStatus
)

func plannedCodexLaunchTarget(launchPlan plan.LaunchPlan, worktreePath, trackID, featureID, workItem string, noWorktree bool, canonicalRoot string) string {
	if launchPlan.IsolationMode == plan.IsolationManagedWorktree && launchPlan.PlannedWorktreePath != "" {
		return fmt.Sprintf("worktree=%q", launchPlan.PlannedWorktreePath)
	}
	switch {
	case worktreePath != "":
		return fmt.Sprintf("worktree=%q", worktreePath)
	case !noWorktree && trackID != "":
		return fmt.Sprintf("track=%q root=%q", trackID, canonicalRoot)
	case !noWorktree && featureID != "":
		return fmt.Sprintf("feature=%q root=%q", featureID, canonicalRoot)
	case workItem != "":
		return fmt.Sprintf("work-item=%q", workItem)
	default:
		return "project-root"
	}
}

func runFeatureStartWithEnv(id string, extraEnv []string) error {
	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("could not determine executable: %w", err)
	}
	cmd := exec.Command(exe, "feature", "start", id)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if len(extraEnv) > 0 {
		cmd.Env = append(os.Environ(), extraEnv...)
	}
	return cmd.Run()
}

// launchCodexContinue resumes the last Codex session.
// Corresponds to: wipnote codex --continue
func launchCodexContinue(resumeID string, yolo bool, extraArgs []string) error {
	projectRoot, _ := resolveProjectRoot()
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline: "Resuming last Codex session...",
	}))
	return execCodex(codexLaunchOpts{
		ResumeLast:  resumeID == "", // only pass --last when no specific ID
		ResumeID:    resumeID,
		ExtraArgs:   extraArgs,
		ProjectRoot: projectRoot,
		Mode:        codexLaunchModeContinue,
		Yolo:        yolo,
	})
}

func launchCodexDev(resumeID string, cleanup, dryRun, yolo bool, extraArgs []string, trackID, featureID, worktreePath, workItem string, noWorktree bool) error {
	_ = computeLauncherMode("", true, false)
	return launchCodexDefaultWithMarketplace(resumeID, trackID, featureID, worktreePath, workItem, noWorktree, yolo, extraArgs, codexMarketplaceSourceDev, cleanup, dryRun)
}
