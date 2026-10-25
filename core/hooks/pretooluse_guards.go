package hooks

// The PreToolUse guard pipeline steps, in the order preToolUseGuards runs
// them. Each guard either returns an outcome that decides the call or nil to
// let the pipeline continue.

// guardWipnoteWrite never intercepts writes to .wipnote/ — mirror of
// pretooluse-wipnote-guard.py to prevent accidental DB corruption.
// Covers Write/Edit/MultiEdit tools AND Bash commands that target .wipnote/.
func guardWipnoteWrite(st *preToolState) *guardOutcome {
	if iswipnoteWrite(st.event) {
		return block(".wipnote/ is managed by wipnote SDK. Use SDK methods instead.")
	}
	if isBashwipnoteWrite(st.event) {
		return block(".wipnote/ is managed by wipnote CLI. Use `wipnote` commands instead of direct file manipulation.")
	}
	return nil
}

// guardBashCwd blocks bare `cd` in Bash commands that pollute the working
// directory.
func guardBashCwd(st *preToolState) *guardOutcome {
	return blockIf(checkBashCwdGuard(st.event))
}

// guardPlanModeBypass: when permission_mode is "plan", the agent is exploring
// (Read/Grep/Glob) and writing only to the plan file. Skip work-item and
// YOLO guards entirely — record the event for observability and allow.
func guardPlanModeBypass(st *preToolState) *guardOutcome {
	if st.event.PermissionMode != "plan" {
		return nil
	}
	debugLog(st.ctx.ProjectDir, "[wipnote] plan mode active — skipping write guards for %s",
		st.event.ToolName)
	result, err := recordEventAndAllow(st.event, st.ctx)
	return &guardOutcome{result: result, err: err}
}

// guardOrchestratorResearchAdvisory collects the orchestrator research
// delegation advisory. It never decides the call.
func guardOrchestratorResearchAdvisory(st *preToolState) *guardOutcome {
	st.orchestrationResearchAdvisory = checkOrchestratorResearchDelegationAdvisory(st.event, st.ctx)
	return nil
}

// guardOrchestratorStrict is orchestrator mode enforcement (feat-567c0211,
// GH-#19 / GH-#20). Runs for the root session only: in strict mode a
// non-whitelisted tool — Skill and non-`wipnote` Bash above all — is counted
// and, past max_violations, blocked. Guidance mode advises without counting.
// The bug-c8ac6a11 rescue escape hatch is honoured inside the guard.
func guardOrchestratorStrict(st *preToolState) *guardOutcome {
	advice, blockReason := checkOrchestratorStrictGuard(st.event, st.ctx)
	st.orchestratorAdvice = advice
	if blockReason != "" {
		return block(blockReason)
	}
	return nil
}

// guardSubagentWorkItem blocks Write/Edit/MultiEdit from subagents when THIS
// AGENT has no active claim. Subagents are checked per-agent via
// claimed_by_agent_id in the claims table (now supplied by the batch context
// query); the orchestrator falls back to session-scoped FeatureID.
// YOLO mode enforcement: subagents get a short grace period on session
// start to claim a work item before guards fire — the parent session's
// open canonical claim serves as confirmation that the orchestrator has
// already registered intent. This MUST run before the subagent work item
// guard so that freshly spawned subagents aren't blocked before they can
// claim. Both inputs are canonical (bug-7036b94f): the projection's
// sessions row that used to carry created_at + parent_session_id is never
// hydrated on the hook read path.
func guardSubagentWorkItem(st *preToolState) *guardOutcome {
	ctx := st.ctx
	st.subagentGrace = checkYoloSubagentGrace(
		ctx.IsYoloMode, ctx.IsSubagent,
		subagentStartedAt(ctx), ctx.ParentSessionID, ctx.HgDir,
	)
	if st.subagentGrace {
		debugLog(ctx.ProjectDir, "[wipnote] subagent grace period active for session %s — allowing write before claim",
			ctx.SessionID)
	}

	// Subagent work item guard: ensure subagents have claimed a work item.
	// Only enforced in YOLO mode — normal interactive subagents should not be
	// blocked by this guard (bug-ba6d1e1c).
	// Skipped during grace period (subagent just spawned, needs time to claim).
	st.claimedItem = inheritParentChainClaim(ctx)
	hasAgentClaim := false
	if ctx.IsSubagent {
		hasAgentClaim = st.claimedItem != ""
	} else {
		hasAgentClaim = ctx.FeatureID != ""
	}
	if ctx.IsYoloMode && !st.subagentGrace {
		return blockIf(checkSubagentWorkItemGuard(st.event.ToolName, ctx.IsSubagent, hasAgentClaim, ctx.SessionID, ctx.IsYoloMode, ctx.FeatureID, st.claimedItem))
	}
	return nil
}

// inheritParentChainClaim returns the claimed item for this agent.
//
// Parent-chain claim walk (feat-ecd82f68, canonicalised in bug-7036b94f):
// when the sub-agent has no direct claim, consult the canonical claim
// ledger for an open episode held by this session or its family root. The
// orchestrator may have run `wipnote feature start` and holds the claim
// under its session ID (GH-#87).
func inheritParentChainClaim(ctx *toolUseContext) string {
	claimedItem := ctx.ClaimedItem
	if ctx.IsSubagent && claimedItem == "" {
		inherited, parentSessID := getClaimFromParentChain(ctx.HgDir, ctx.SessionID, claimedItem)
		if inherited != "" {
			claimedItem = inherited
			if ctx.FeatureID == "" {
				ctx.FeatureID = inherited
			}
			debugLog(ctx.ProjectDir, "[wipnote] claim inherited: session=%s parent=%s feat=%s",
				ctx.SessionID, parentSessID, inherited)
		}
	}
	return claimedItem
}

// guardSubagentCommit blocks sub-agent git commit on main/master.
// Orchestrators (no parent) are allowed to commit on main intentionally.
// This runs unconditionally — it is not gated on YOLO mode.
func guardSubagentCommit(st *preToolState) *guardOutcome {
	return blockIf(checkSubagentCommitGuard(st.event, st.ctx.ParentSessionID, st.ctx.ProjectDir))
}

// guardPortDriftCommit blocks git commit when generator-input files are staged
// but the generated plugin trees are stale (not regenerated via build-ports).
// Always-on correctness gate — not YOLO-gated. Fast path: a single
// `git diff --cached --name-only` call; CheckPorts only runs when a
// generator-input file is actually staged. This replaces the expensive
// per-turn CheckPorts that was previously called from the Stop hook
// (bug-3fb22f7e).
func guardPortDriftCommit(st *preToolState) *guardOutcome {
	return blockIf(checkPortDriftCommitGuard(st.event))
}

// guardDependencyResearchCommit blocks git commit when the staged diff changes
// a dependency manifest (go.mod/package.json/…) but no web research happened
// this session. It complements the pre-edit external-tech guard by catching
// deps introduced via Bash (`go get`/`npm install`). Fast path: the same
// single `git diff --cached --name-only` shape (feat-af4ae1c3).
//
// Research guards (this one and the read-before-write and web-research
// guards below) block only in YOLO mode, where no person reviews the work.
// In attended sessions they advise instead: current models research without
// being forced, and a hard block there mostly cost a turn.
// Use the resilient yolo signal (bug-0ed4e469): a yolo subagent can report
// IsYoloMode=false, yet its parent chain is yolo and must still block.
func guardDependencyResearchCommit(st *preToolState) *guardOutcome {
	st.yoloContext = resilientYoloContext(st.ctx)
	return st.adviseOrBlockOutcome(checkDependencyResearchCommitGuard(st.event, st.ctx))
}

// guardResearchDispatch is the research-first gate on implementer dispatch.
// Advisory in warn mode, deny in block mode, for a gated item with no research
// record (docs/design/research-gate.md). File state only.
func guardResearchDispatch(st *preToolState) *guardOutcome {
	advisory, deny := checkResearchDispatchGuard(st.event, st.ctx)
	st.researchDispatchAdvisory = advisory
	if deny {
		return block(advisory)
	}
	return nil
}
