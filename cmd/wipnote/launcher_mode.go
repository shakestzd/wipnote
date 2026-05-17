package main

import (
	"fmt"
	"io"
	"os"

	"github.com/shakestzd/wipnote/internal/launcher/mode"
	"github.com/shakestzd/wipnote/internal/launcher/plan"
	"github.com/shakestzd/wipnote/internal/paths"
)

// LauncherModeResult is the computed mode object exposed to preflight paths.
// Callers can log or inspect it without changing launcher behavior.
type LauncherModeResult = mode.LauncherMode

// computeLauncherMode returns a LauncherMode for the given launcher invocation.
// worktreePath should be non-empty when running in an isolated git worktree,
// devPlugin when launched with --dev (in-tree plugin source), and
// generatedPort when a harness-generated tree is active.
//
// This is the non-behavior-changing wiring point for all launchers.
// Future slices will act on the returned value; this slice only computes and
// optionally logs it.
func computeLauncherMode(worktreePath string, devPlugin, generatedPort bool) LauncherModeResult {
	m := mode.Compute(worktreePath, false, devPlugin, generatedPort)
	if os.Getenv("WIPNOTE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr,
			"wipnote [debug]: mode runtime=%s execution=%s plugin=%s dashboard=%s:%d\n",
			m.Runtime, m.Execution, m.Plugin, m.DashboardHost, m.DashboardPort,
		)
	}
	return m
}

// applyLaunchPlan computes the isolation plan for a mutating launcher invocation
// and prints any dirty-main warning to w. It does NOT create the worktree —
// that will happen when the managed-worktree path is executed (slice-3+).
//
// Rollout rules (HIGH critique honored):
//   - Host runtime → warn-only; RefuseLaunch is always false by default.
//   - Devcontainer → managed-worktree when a workItemID is provided.
//   - inPlace=true → IsolationExplicitInPlace; no warning.
func applyLaunchPlan(repoRoot, workItemID string, inPlace bool, w io.Writer) plan.LaunchPlan {
	m := mode.Compute("", false, false, false)
	p, err := plan.PlanLaunch(plan.Input{
		RepoRoot:    repoRoot,
		WorkItemID:  workItemID,
		RuntimeMode: m.Runtime,
		InPlace:     inPlace,
		EnforceIsolation: os.Getenv("WIPNOTE_ENFORCE_ISOLATION") == "true",
	})
	if err != nil {
		return p
	}
	if p.DirtyMainWarning != "" {
		fmt.Fprintln(w, p.DirtyMainWarning)
	}
	if os.Getenv("WIPNOTE_DEBUG") != "" {
		fmt.Fprintf(os.Stderr,
			"wipnote [debug]: launch-plan isolation=%s worktree=%s refuse=%v\n",
			p.IsolationMode, p.PlannedWorktreePath, p.RefuseLaunch,
		)
	}
	return p
}

// canonicalProjectRoot returns the canonical main repo root when projectRoot is
// a linked git worktree, or "" when it is the main worktree (or not a git repo).
//
// Use this to populate WipnoteRoot / wipnoteRoot in launcher opts so that
// WIPNOTE_PROJECT_DIR always points at the canonical main repo root regardless
// of whether the user ran wipnote from inside a linked worktree. It wraps
// paths.ResolveViaGitCommonDir and adds no new identity abstraction.
//
// Callers must preserve projectRoot as the working directory for the child
// process; only WIPNOTE_PROJECT_DIR (controlled via WipnoteRoot) is changed.
func canonicalProjectRoot(projectRoot string) string {
	return paths.ResolveViaGitCommonDir(projectRoot)
}
