package hooks

import (
	"path/filepath"
	"regexp"
	"time"

	"github.com/shakestzd/wipnote/core/runledger"
)

// Background-task run records (feat-36658c22). The agent's command is never
// rewritten or wrapped: the start is read from the PostToolUse payload of the
// call that launched the task (Claude Code) or from PreToolUse (Codex), and
// the end from the transcript's task-notification (runs_transcript.go) or
// Codex's late PostToolUse. Every write is one small append to the session's
// run shard: no SQLite, no network, no transcript scan.

var (
	// bashBackgroundIDPattern is the Bash result text for a backgrounded
	// command: "Command running in background with ID: <id>. Output is …".
	bashBackgroundIDPattern = regexp.MustCompile(`running in background with ID: ([A-Za-z0-9_-]+)`)
	// monitorStartedPattern is the Monitor result text: "Monitor started (task <id>, …".
	monitorStartedPattern = regexp.MustCompile(`Monitor started \(task ([A-Za-z0-9_-]+)`)
)

// runLaunch is what a Claude tool call started, if anything.
type runLaunch struct {
	kind   string
	taskID string
	sync   bool // a foreground subagent: its PostToolUse is also its end
}

// recordRunFromPostToolUse is the PostToolUse half: a Claude launch becomes a
// start (and, for a foreground subagent, an end); a Codex shell call ends.
func recordRunFromPostToolUse(event *CloudEvent, ctx *toolUseContext) {
	switch event.Harness {
	case HarnessClaude:
		recordClaudeRunLaunch(event, ctx)
	case HarnessCodex:
		if event.ToolName == "Bash" {
			recordCodexRunEnd(event, ctx)
		}
	}
}

// recordRunFromPreToolUse is the PreToolUse half: only a Codex shell call
// starts here, since its PostToolUse arrives when the command finishes.
func recordRunFromPreToolUse(event *CloudEvent, ctx *toolUseContext) {
	if event.Harness != HarnessCodex || event.ToolName != "Bash" {
		return
	}
	appendRunEvent(ctx.ProjectDir, newRunStart(event, ctx, runledger.KindShell, "", time.Now().UTC()))
}

func recordClaudeRunLaunch(event *CloudEvent, ctx *toolUseContext) {
	launch, ok := classifyClaudeLaunch(event)
	if !ok {
		return
	}
	now := time.Now().UTC()
	start := now
	if launch.sync {
		start = now.Add(-event.DurationMS.Duration())
	}
	ev := newRunStart(event, ctx, launch.kind, launch.taskID, start)
	appendRunEvent(ctx.ProjectDir, ev)
	if launch.sync {
		end := runEndFor(ev, now, runledger.MapStatus(stringField(event.ToolResult, "status"), nil))
		appendRunEvent(ctx.ProjectDir, end)
	}
}

// classifyClaudeLaunch reports whether the call started a background task:
// a Bash command run (or moved) into the background, any subagent, or a
// monitor. A foreground Bash call or any other tool is not a run.
func classifyClaudeLaunch(event *CloudEvent) (runLaunch, bool) {
	in, res := event.ToolInput, event.ToolResult
	text := summarizeToolOutput(res)
	switch event.ToolName {
	case "Bash":
		id := firstNonEmpty(stringField(res, "backgroundTaskId"), submatch(bashBackgroundIDPattern, text))
		if id == "" && !boolField(in, "run_in_background") {
			return runLaunch{}, false
		}
		return runLaunch{kind: runledger.KindShell, taskID: id}, true
	case "Agent", "Task":
		async := boolField(res, "isAsync") || stringField(res, "status") == "async_launched" || boolField(in, "run_in_background")
		return runLaunch{kind: runledger.KindSubagent, taskID: stringField(res, "agentId"), sync: !async}, true
	case "Monitor":
		id := firstNonEmpty(stringField(res, "taskId"), submatch(monitorStartedPattern, text))
		return runLaunch{kind: runledger.KindMonitor, taskID: id}, true
	}
	return runLaunch{}, false
}

func recordCodexRunEnd(event *CloudEvent, ctx *toolUseContext) {
	start := newRunStart(event, ctx, runledger.KindShell, "", time.Now().UTC())
	status := runledger.StatusCompleted
	if !isSuccess(event.ToolResult) {
		status = runledger.StatusFailed
	}
	end := runEndFor(start, time.Now().UTC(), status)
	last, first := runledger.SummarizeText(summarizeToolOutput(event.ToolResult))
	end.LastLine, end.FirstError = runledger.Clean(last, ctx.ProjectDir), runledger.Clean(first, ctx.ProjectDir)
	appendRunEvent(ctx.ProjectDir, end)
}

// newRunStart builds the start event for the call in event.
func newRunStart(event *CloudEvent, ctx *toolUseContext, kind, taskID string, at time.Time) runledger.Event {
	root := ctx.ProjectDir
	cwd := firstNonEmpty(event.CWD, root)
	return runledger.Event{
		RunID: runIDFor(ctx.SessionID, event.ToolUseID, taskID), Phase: runledger.PhaseStart, At: at,
		TaskID: taskID, ToolUseID: event.ToolUseID, Kind: kind,
		Description: runledger.Truncate(runledger.Clean(stringField(event.ToolInput, "description"), root), 200),
		Command:     runledger.CleanCommand(stringField(event.ToolInput, "command"), root),
		Folder:      runledger.Folder(cwd, root), Commit: runledger.HeadCommit(cwd),
		Session: ctx.SessionID, WorkItem: runWorkItem(ctx), Harness: event.Harness.String(),
	}
}

// runEndFor is the end event of the run start names.
func runEndFor(start runledger.Event, at time.Time, status string) runledger.Event {
	return runledger.Event{
		RunID: start.RunID, Phase: runledger.PhaseEnd, At: at, TaskID: start.TaskID,
		ToolUseID: start.ToolUseID, Kind: start.Kind, Status: status,
		Session: start.Session, Harness: start.Harness,
	}
}

// runIDFor keys a run on its tool-use id, else its task id, so the start and
// the end, written by different hooks, name the same run.
func runIDFor(session, toolUseID, taskID string) string {
	switch {
	case toolUseID != "":
		return runledger.RunIDFor(session, toolUseID)
	case taskID != "":
		return runledger.RunIDFor(session, "task:"+taskID)
	}
	return runledger.NewRunID()
}

// runWorkItem is the active work item for the call: the agent's own claim,
// else the session's open claim.
func runWorkItem(ctx *toolUseContext) string {
	return firstNonEmpty(ctx.ClaimedItem, ctx.FeatureID, canonicalActiveWorkItem(ctx.ProjectDir, ctx.SessionID))
}

// appendRunEvent appends one event. Best-effort: a hook never fails because
// a run record could not be written.
func appendRunEvent(projectDir string, e runledger.Event) {
	if projectDir == "" || e.Session == "" {
		return
	}
	if err := runledger.NewStore(filepath.Join(projectDir, ".wipnote")).Append(e); err != nil {
		debugLog(projectDir, "run ledger append %s: %v", e.RunID, err)
	}
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func boolField(m map[string]any, key string) bool {
	v, _ := m[key].(bool)
	return v
}

func submatch(re *regexp.Regexp, s string) string {
	if m := re.FindStringSubmatch(s); m != nil {
		return m[1]
	}
	return ""
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if v != "" {
			return v
		}
	}
	return ""
}
