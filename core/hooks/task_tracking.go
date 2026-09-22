package hooks

import (
	"database/sql"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// workItemIDInTextPattern matches a wipnote work-item ID (e.g. "bug-9972133d")
// anywhere in free text. The prefix list and "8 lowercase-hex-chars" shape
// mirror the ID grammar cmd/wipnote/reindex_trailers.go uses to parse commit
// trailers (parenWorkItemRe / isWorkItemID) — copied here, rather than
// imported, because core/hooks lives in the core module and cannot import
// the cmd/wipnote main package (see .claude/rules/code-hygiene.md's
// cmd -> internal import-direction rule). Unlike parenWorkItemRe this does
// NOT require surrounding parentheses: task subjects/descriptions reference
// work items inline (e.g. "Fix bug-9972133d's redirect regex"), not in the
// parenthesized commit-trailer convention.
var workItemIDInTextPattern = regexp.MustCompile(`\b(?:feat|bug|spk|trk|pln|spc|plan|spec)-[0-9a-f]{8}\b`)

// taskNamesWorkItem reports whether subject or description names a wipnote
// work-item ID, i.e. the task describes specific tracked work rather than an
// orchestrator-internal process step (e.g. "Draft the plan YAML") that
// happens to run while some unrelated item is active (GH-#169, bug-664276df).
func taskNamesWorkItem(subject, description string) bool {
	return workItemIDInTextPattern.MatchString(subject) || workItemIDInTextPattern.MatchString(description)
}

// taskStepsDisabled reports whether step-mirroring for TaskCreated/TaskCompleted
// has been opted out of via WIPNOTE_TASK_STEPS=off. The agent_event recording
// in both handlers is unaffected — this only gates the addTaskStep/completeTaskStep
// side effect.
func taskStepsDisabled() bool {
	return strings.EqualFold(strings.TrimSpace(os.Getenv("WIPNOTE_TASK_STEPS")), "off")
}

// selfBinary returns the path to the wipnote binary for self-invocation.
//
// Resolution order:
//  1. os.Executable() — the binary currently running (which is whatever
//     resolved on PATH when Claude Code invoked the hook command). This is
//     the canonical answer: if `wipnote hook X` is running, self-invoking
//     `wipnote Y` should use the same binary.
//  2. "wipnote" on PATH (fallback when os.Executable() fails, rare).
//
// Note: previous versions checked `$CLAUDE_PLUGIN_ROOT/hooks/bin/wipnote`
// first. That fallback was removed because (a) hooks.json invokes the PATH
// `wipnote` directly, so the hook process already IS the PATH binary;
// (b) a stale binary lingering under plugin/hooks/bin/ could silently
// shadow the current install. Trust PATH — single source of truth.
func selfBinary() string {
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "wipnote"
}

// CROSS-HARNESS STEP CONTRACT (feat-885ec940, Tier 4).
//
// Hook-driven step tracking (addTaskStep / completeTaskStep) is reachable ONLY
// from the Claude TaskCreated / TaskCompleted handlers in missing_events.go.
// This is intentional and load-bearing:
//
//   - Claude:  TaskCreated → addTaskStep, TaskCompleted → completeTaskStep.
//     A create/complete pair exists, so steps are honestly LIVE.
//   - Codex:   no task lifecycle hooks exist at all. Codex 0.147.0 dispatches
//     exactly 11 events (SessionStart, SessionEnd, UserPromptSubmit,
//     PreToolUse, PostToolUse, PermissionRequest, PreCompact, PostCompact,
//     SubagentStart, SubagentStop, Stop) — none of them per-task. wipnote
//     once registered TaskStarted/TaskComplete for Codex; those names are
//     phantom and never fired (bug-e39d408f). Session termination now binds
//     to the real SessionEnd and Stop events, which are SESSION lifecycle
//     events, NOT per-task completions: mapping either to completeTaskStep
//     would tick steps that were never created as steps — a dishonest "live
//     steps" state. Deliberately NOT mapped.
//   - Gemini:  emits no task lifecycle hooks at all (manifest.json declares
//     none with targets:[gemini]). Nothing to map.
//
// The honest per-harness truth is centralized in resolveTaskTrackingInfo
// (cmd/wipnote/who.go): codex-cli / gemini-cli report Supported=false with an
// UNSUPPORTED detail string. Tier 4 surfaces that exact signal into
// /api/features as step_tracking_supported / step_tracking_detail so the
// Kanban board renders a visible "steps not live for this harness" state and
// NEVER implies live step tracking for a harness that cannot emit step events.
// Because no harness→step mapping changed, manifest.json is unchanged and no
// `wipnote plugin build-ports` regeneration is required.
//
// addTaskStep shells out to the wipnote CLI to add a task-associated step to
// the active feature. The CLI sets StepID="task-<taskID>" so completeTaskStep
// can find and tick it. Shells out rather than importing workitem directly
// (architectural constraint: hooks must not import workitem).
//
// Indirected through addTaskStepFn (default: this function) so tests can
// observe whether a step-mirroring call happened without actually shelling
// out to a wipnote binary.
var addTaskStepFn = addTaskStep

func addTaskStep(_ *sql.DB, _ string, featureID, taskID, subject, teammateName string) {
	if subject == "" {
		subject = "Task " + taskID
	}
	stepDesc := subject
	if teammateName != "" {
		stepDesc = "[" + teammateName + "] " + stepDesc
	}
	typeName := inferTypeName(featureID)

	// wipnote <type> add-task-step <id> <task-id> "<description>"
	cmd := exec.Command(selfBinary(), typeName, "add-task-step", featureID, taskID, stepDesc)
	_ = cmd.Run()
}

// completeTaskStep flips data-completed=true on the step with
// StepID="task-<taskID>" via the CLI. The CLI call (which uses
// workitem.Collection.CompleteTaskStep) is the canonical update — it mutates
// HTML and updates SQLite counters in one transaction.
//
// Indirected through completeTaskStepFn (default: this function) — see
// addTaskStepFn for why.
var completeTaskStepFn = completeTaskStep

func completeTaskStep(database *sql.DB, _ string, featureID, taskID, _ string) {
	typeName := inferTypeName(featureID)
	cmd := exec.Command(selfBinary(), typeName, "complete-task-step", featureID, taskID)
	_ = cmd.Run()

	if database == nil {
		return
	}
	// Bump updated_at so query consumers see freshness (CLI also updates this,
	// but the hook may run before/after the CLI completes — this is a no-op
	// when the CLI already touched the row).
	_, _ = database.Exec(`
		UPDATE features
		SET updated_at = ?
		WHERE id = ?`,
		time.Now().UTC().Format(time.RFC3339), featureID)
}

// inferTypeName returns the CLI type name (feature, bug, spike) from an ID prefix.
func inferTypeName(id string) string {
	switch {
	case strings.HasPrefix(id, "bug-"):
		return "bug"
	case strings.HasPrefix(id, "spk-"):
		return "spike"
	default:
		return "feature"
	}
}
