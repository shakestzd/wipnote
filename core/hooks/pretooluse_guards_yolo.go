package hooks

// Work-item, research and YOLO steps of the PreToolUse guard pipeline.
//
// The work-item and research guards are required regardless of YOLO mode;
// research guards follow the adviseOrBlock posture. They are skipped during
// the subagent grace period (subagent just spawned, needs time to claim).

// guardYoloWorkItem requires an active work item before writes. It also
// extracts the target file path once for the guards that follow.
func guardYoloWorkItem(st *preToolState) *guardOutcome {
	if st.subagentGrace {
		return nil
	}
	ctx := st.ctx
	activeWorkItem := ctx.FeatureID
	if activeWorkItem == "" {
		activeWorkItem = st.claimedItem
	}
	// Extract the target file path once and reuse for both guards below.
	st.targetFile = extractFilePath(st.event.ToolInput)
	return blockIf(checkYoloWorkItemGuard(st.event.ToolName, activeWorkItem, ctx.IsYoloMode, ctx.SessionID, st.targetFile, ctx.ProjectDir))
}

// guardReadBeforeWrite is research-first: require at least one Read/Grep/Glob
// before writing.
func guardReadBeforeWrite(st *preToolState) *guardOutcome {
	if st.subagentGrace {
		return nil
	}
	ctx := st.ctx
	st.hasResearch = hasRecentResearch(ctx.SessionID, ctx.AgentID, ctx.ProjectDir)
	return st.adviseOrBlockOutcome(checkYoloResearchGuard(st.event.ToolName, ctx.IsYoloMode, st.hasResearch, st.targetFile, ctx.ProjectDir))
}

// guardExternalTechResearch is specificity-aware research: an
// external-technology change requires WEB/docs research — a local read does
// not suffice (spk-0a982f70). Two trigger sets:
//   - dependency-manifest edits (go.mod/package.json/…)        → feat-868c752b
//   - harness-contract edits (agent manifests, hook matrix, …) → feat-ff62b911
//
// editTargetPaths covers apply_patch (which bundles multiple paths in its
// payload, not a file_path field — roborev #563/#566). hasRecentWebResearch
// is queried once and only when a trigger path is present, so the normal
// Write/Edit hot path is unaffected.
func guardExternalTechResearch(st *preToolState) *guardOutcome {
	if st.subagentGrace {
		return nil
	}
	ctx, event := st.ctx, st.event
	editPaths := editTargetPaths(event)
	manifestPath := firstPathMatching(editPaths, isExternalTechEdit)
	harnessPath := firstPathMatching(editPaths, isHarnessContractEdit)
	if manifestPath == "" && harnessPath == "" {
		return nil
	}
	hasWebResearch := hasRecentWebResearch(ctx.SessionID, ctx.AgentID, ctx.ProjectDir)
	if manifestPath != "" {
		if out := st.adviseOrBlockOutcome(checkExternalTechResearchGuard(event.ToolName, hasWebResearch, manifestPath, ctx.ProjectDir)); out != nil {
			return out
		}
	}
	if harnessPath != "" {
		return st.adviseOrBlockOutcome(checkHarnessContractResearchGuard(event.ToolName, hasWebResearch, harnessPath, ctx.ProjectDir))
	}
	return nil
}

// guardYoloBashResearch applies the research-first rule to Bash writes.
func guardYoloBashResearch(st *preToolState) *guardOutcome {
	if st.subagentGrace {
		return nil
	}
	return st.adviseOrBlockOutcome(checkYoloBashResearchGuard(st.event, st.ctx.IsYoloMode, st.hasResearch))
}

// guardYoloWorktree is the worktree-isolation guard. It runs under a
// RESILIENT yolo-context signal — not just ctx.IsYoloMode (bug-0ed4e469). A
// coder subagent in a yolo run can have its own IsYoloMode resolve false
// (Claude reports a non-bypass permission_mode), yet a main/master-targeted
// edit must still be blocked. The resilient signal is true when EITHER primary
// detection says yolo, OR this is a subagent whose parent-session chain is
// yolo per isYoloFromDB. This is deliberately narrow: it does NOT block main
// edits in genuinely non-yolo sessions (plain `wipnote claude`, normal dev, or
// a top-level session deliberately running on main) because those have no
// yolo ancestor.
func guardYoloWorktree(st *preToolState) *guardOutcome {
	if !st.yoloContext {
		return nil
	}
	// Resolve branch from the target file's worktree, not the session CWD.
	targetFile := extractFilePath(st.event.ToolInput)
	cwdBranch := currentBranchIn(st.event.CWD)
	branch := branchForFilePath(targetFile, cwdBranch)
	if warn := checkYoloWorktreeGuard(st.event.ToolName, branch, st.yoloContext); warn != "" {
		return block(warn)
	}
	// Extend worktree guard to Bash file-write commands.
	return blockIf(checkYoloBashWorktreeGuard(st.event, branch, st.yoloContext))
}

// guardYoloSession runs the YOLO-only session guards: step/code-health and
// orchestrator-write warnings, and the commit, diff-review, UI-validation,
// budget and roborev blocks.
func guardYoloSession(st *preToolState) *guardOutcome {
	ctx, event := st.ctx, st.event
	if !ctx.IsYoloMode {
		return nil
	}
	// Warn (not block) when starting a work item without steps.
	if warn := checkYoloStepsGuard(event, ctx.IsYoloMode, ctx.HgDir); warn != "" {
		debugLog(ctx.ProjectDir, "[wipnote] YOLO steps warning: %s", warn)
	}

	// Warn (not block) about code health — files already oversized should be
	// allowed to be edited so they can be refactored smaller.
	if warn := checkYoloCodeHealthGuard(event, ctx.IsYoloMode); warn != "" {
		debugLog(ctx.ProjectDir, "[wipnote] YOLO code health warning: %s", warn)
	}
	testRan := hasRecentTestRun(ctx.ProjectDir, ctx.SessionID)
	if warn := checkYoloCommitGuard(event, ctx.IsYoloMode, testRan); warn != "" {
		return block(warn)
	}
	if warn := checkYoloDiffReviewGuard(event, ctx.IsYoloMode, hasRecentDiffReview(ctx.ProjectDir, ctx.SessionID)); warn != "" {
		return block(warn)
	}
	if warn := checkYoloUIValidationGuard(event, ctx.IsYoloMode, ctx.SessionID, ctx.ProjectDir); warn != "" {
		return block(warn)
	}
	if warn := checkYoloBudgetGuard(event, ctx.IsYoloMode); warn != "" {
		return block(warn)
	}
	if warn := checkYoloRoborevGuard(event, ctx.IsYoloMode); warn != "" {
		return block(warn)
	}

	// Warn (not block) when the orchestrator writes directly instead of
	// delegating to a subagent (bug-06627817).
	if warn := checkYoloOrchestratorWriteGuard(event, ctx.IsSubagent); warn != "" {
		debugLog(ctx.ProjectDir, "[wipnote] YOLO orchestrator write warning: %s", warn)
	}
	return nil
}
