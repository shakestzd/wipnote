package hooks

import (
	"fmt"
	"strings"
)

// PreToolUse handles the PreToolUse Claude Code hook event.
// It inserts a tool_call agent_event row and allows the tool to proceed.
//
// After the kill switch and context resolution, the guards run as an ordered
// pipeline (preToolUseGuards); the first guard that returns an outcome decides
// the call. When none does, the event is recorded and the call is allowed with
// the advisories the guards collected.
func PreToolUse(event *CloudEvent) (*HookResult, error) {
	// Kill switch: WIPNOTE_GUARDS_OFF=1 disables ALL guards for emergency use.
	// It is operator-only and never advertised in block messages; every time it
	// is honoured it is recorded loudly (stderr, debug log, GuardOverride
	// agent_event) so the bypass is part of the lineage (GH-#164).
	if guardOverrideEnabled() {
		recordGuardOverride(event)
		return &HookResult{}, nil
	}

	ctx := resolveToolUseContext(event)
	if ctx == nil {
		return &HookResult{}, nil
	}

	st := &preToolState{event: event, ctx: ctx}
	for _, g := range preToolUseGuards() {
		if out := g.run(st); out != nil {
			if out.err == nil && delegateToolAllowed(out.result) {
				recordDelegateToolStart(event, ctx.ProjectDir, ctx.SessionID)
			}
			return out.result, out.err
		}
	}
	// A denied call never runs, so only an allowed one opens a tool_start.
	recordDelegateToolStart(event, ctx.ProjectDir, ctx.SessionID)
	return st.recordAndAllow()
}

// preToolState is the state the PreToolUse guard pipeline shares: the event,
// its resolved context, and what earlier guards derived or advised.
type preToolState struct {
	event *CloudEvent
	ctx   *toolUseContext

	orchestrationResearchAdvisory string
	orchestratorAdvice            string
	researchDispatchAdvisory      string
	researchAdvice                []string

	subagentGrace bool
	claimedItem   string
	yoloContext   bool
	targetFile    string
	hasResearch   bool
}

// guardOutcome is a guard's decision. A nil *guardOutcome lets the pipeline
// continue to the next guard.
type guardOutcome struct {
	result *HookResult
	err    error
}

// preToolGuard is one named step of the PreToolUse guard pipeline.
type preToolGuard struct {
	name string
	run  func(*preToolState) *guardOutcome
}

// preToolUseGuards returns the PreToolUse guards in evaluation order. The
// order is load-bearing and pinned by TestPreToolUse_GuardOrder.
func preToolUseGuards() []preToolGuard {
	return []preToolGuard{
		{"wipnote-write", guardWipnoteWrite},
		{"bash-cwd", guardBashCwd},
		{"delegate-redirect", guardDelegateRedirect},
		{"plan-mode-bypass", guardPlanModeBypass},
		{"orchestrator-research-advisory", guardOrchestratorResearchAdvisory},
		{"orchestrator-strict", guardOrchestratorStrict},
		{"subagent-work-item", guardSubagentWorkItem},
		{"subagent-commit", guardSubagentCommit},
		{"port-drift-commit", guardPortDriftCommit},
		{"dependency-research-commit", guardDependencyResearchCommit},
		{"research-dispatch", guardResearchDispatch},
		{"yolo-work-item", guardYoloWorkItem},
		{"read-before-write", guardReadBeforeWrite},
		{"external-tech-research", guardExternalTechResearch},
		{"yolo-bash-research", guardYoloBashResearch},
		{"yolo-worktree", guardYoloWorktree},
		{"yolo-session", guardYoloSession},
	}
}

// block is the outcome of a guard that refuses the call with reason.
func block(reason string) *guardOutcome {
	return &guardOutcome{result: &HookResult{Decision: "block", Reason: reason}}
}

// blockIf blocks with warn when it is non-empty.
func blockIf(warn string) *guardOutcome {
	if warn != "" {
		return block(warn)
	}
	return nil
}

// adviseOrBlock applies the research-guard posture: research guards block
// only in YOLO mode, where no person reviews the work, and advise otherwise.
func (st *preToolState) adviseOrBlock(warn string) bool {
	blocked, advice := researchGuardVerdict(warn, st.yoloContext)
	if advice != "" {
		st.researchAdvice = append(st.researchAdvice, advice)
	}
	return blocked
}

// adviseOrBlockOutcome blocks with warn when adviseOrBlock says so.
func (st *preToolState) adviseOrBlockOutcome(warn string) *guardOutcome {
	if st.adviseOrBlock(warn) {
		return block(warn)
	}
	return nil
}

// recordAndAllow records the event, allows the tool to proceed, and attaches
// the advisories the guards collected, in their established order.
func (st *preToolState) recordAndAllow() (*HookResult, error) {
	result, err := recordEventAndAllow(st.event, st.ctx)
	if err == nil && result != nil {
		appendAdditionalContext(result, st.orchestrationResearchAdvisory)
		appendAdditionalContext(result, st.researchDispatchAdvisory)
		appendAdditionalContext(result, st.orchestratorAdvice)
		appendAdditionalContext(result, strings.Join(st.researchAdvice, "\n"))
	}
	return result, err
}

func appendAdditionalContext(result *HookResult, context string) {
	if result == nil || context == "" {
		return
	}
	if result.AdditionalContext != "" {
		result.AdditionalContext += "\n" + context
	} else {
		result.AdditionalContext = context
	}
}

// checkSubagentWorkItemGuard blocks Write/Edit/MultiEdit from subagents when
// no active work item is registered for THIS session. Returns a non-empty
// reason to block, or "" to allow.
//
// hasWorkItem must be derived from ctx.FeatureID (session-scoped), not from a
// global DB scan — a global check always passes on projects that have any
// in-progress item, defeating the guard entirely.
//
// Subagents ignore prompt-based instructions to register work items before
// writing code. Enforcing at the hook layer is the reliable alternative.
func checkSubagentWorkItemGuard(toolName string, isSubagent, hasWorkItem bool, sessionID string, isYoloMode bool, featureID, claimedItem string) string {
	if !isSubagent {
		return ""
	}
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch":
	default:
		return ""
	}
	if hasWorkItem {
		return ""
	}

	sess := sessionID
	if len(sess) > 8 {
		sess = sess[:8]
	}
	feat := featureID
	if feat == "" {
		feat = "none"
	}
	claim := claimedItem
	if claim == "" {
		claim = "none"
	}
	return fmt.Sprintf(
		"Write blocked: no claimed work item.\n"+
			"  session=%s yolo=%v subagent=%v\n"+
			"  feature=%s  claim=%s\n"+
			"To unblock: wipnote feature start <id>  (or: wipnote feature create \"...\" --track <trk-id>)",
		sess, isYoloMode, isSubagent,
		feat, claim,
	)
}

// isWriteTool returns true for tools that can modify the filesystem or execute
// arbitrary code. These are blocked when the CWD drifts to a different project.
func isWriteTool(toolName string) bool {
	switch toolName {
	case "Write", "Edit", "MultiEdit", "apply_patch", "Bash", "exec_command", "functions.exec_command", "NotebookEdit", "Agent":
		return true
	}
	return false
}
