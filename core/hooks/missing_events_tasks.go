package hooks

import (
	"database/sql"
	"fmt"
)

// taskCompletionGateEnabled holds the TaskCompleted quality gate off
// (feat-174dc958). The gate runs the project's test command (or its guard
// profile) synchronously inside the hook for up to 60s and, when
// block_task_completion_on_quality_failure is set, blocks the task with exit
// 2. It was unreachable while the active work item came from the dead hook
// database; reviving the step mirror must not silently revive it. Turning it
// on is the owner's call.
const taskCompletionGateEnabled = false

// taskFields is the subject and description a task event carries, typed
// field first, then the task_data map.
func taskFields(event *CloudEvent) (subject, description string) {
	subject = event.TaskSubject
	if subject == "" {
		subject, _ = event.TaskData["subject"].(string)
	}
	description = event.TaskDescription
	if description == "" {
		description, _ = event.TaskData["description"].(string)
	}
	return subject, description
}

// taskStepTarget is the work item a task's step belongs to: the session's
// open claim in the canonical claim ledger, and only when the task names a
// work-item ID (GH-#169, bug-664276df) and step mirroring is on. "" means no
// step is mirrored.
func taskStepTarget(event *CloudEvent, sessionID, subject, description string) string {
	if event.TaskID == "" || taskStepsDisabled() || !taskNamesWorkItem(subject, description) {
		return ""
	}
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	return canonicalActiveWorkItem(projectDir, sessionID)
}

// TaskCreated handles the TaskCreated Claude Code hook event. It mirrors a
// Claude Code task as a step on the session's claimed work item, so the task
// survives the session and is visible to other sessions. database is unused.
func TaskCreated(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	_ = database
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	subject, description := taskFields(event)
	if featureID := taskStepTarget(event, sessionID, subject, description); featureID != "" {
		addTaskStepFn(featureID, event.TaskID, subject, event.TeammateName)
	}
	return &HookResult{Continue: true}, nil
}

// TaskCompleted handles the TaskCompleted Claude Code hook event. It ticks the
// step TaskCreated mirrored. The quality gate is held off; see
// taskCompletionGateEnabled. database is unused.
func TaskCompleted(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	_ = database
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	subject, description := taskFields(event)
	featureID := taskStepTarget(event, sessionID, subject, description)
	if taskCompletionGateEnabled && featureID != "" {
		if err := taskCompletionGateBlock(event, featureID); err != nil {
			return nil, err
		}
	}
	if featureID != "" {
		completeTaskStepFn(featureID, event.TaskID)
	}
	return &HookResult{Continue: true}, nil
}

// taskCompletionGateBlock runs the opt-in quality gate and returns the block
// error when it fails and blocking is configured.
func taskCompletionGateBlock(event *CloudEvent, featureID string) error {
	projectDir := ResolveProjectDir(event.CWD, event.SessionID)
	gate := runTaskCompletionGateFn(projectDir)
	if gate.Passed || !readTaskCompletionConfig(projectDir) {
		return nil
	}
	return &BlockExit2Error{Message: fmt.Sprintf("Quality gate %q failed. "+
		"To complete this task manually after fixing: wipnote feature complete %s",
		gate.GateName, featureID)}
}
