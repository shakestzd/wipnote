package hooks

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/models"
)

// TeammateIdle handles the TeammateIdle Claude Code hook event.
// Records a teammate_idle event when a teammate agent goes idle.
//
// CONTROLLING CAPABILITY (AdditiveControlling, per hookEventContractSpecs): CC
// reads a JSON HookResult here, so this handler COULD return
// {decision:"block", reason:"…"} to keep a teammate busy. It is kept
// observe-only by design — there is no current use-case to override CC's idle
// handling — but the HookResult return path is already in place, so adding a
// future block condition is a one-line change.
func TeammateIdle(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	summary := "Teammate agent went idle"
	if event.TeammateName != "" {
		summary = fmt.Sprintf("Teammate %s went idle", event.TeammateName)
	}
	if event.IdleReason != "" {
		summary += fmt.Sprintf(" (reason: %s)", event.IdleReason)
	}
	return recordSimpleEvent(models.EventTeammateIdle, "TeammateIdle", summary, "recorded", event, database)
}

// TaskCreated handles the TaskCreated Claude Code hook event.
// Mirrors Claude Code tasks into wipnote as steps on the active feature,
// making wipnote the durable task tracking surface that survives session
// termination and is visible to other sessions.
func TaskCreated(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	featureID := cachedGetActiveFeatureID(database, sessionID)
	subject := event.TaskSubject
	if subject == "" {
		subject, _ = event.TaskData["subject"].(string)
	}
	description := event.TaskDescription
	if description == "" {
		description, _ = event.TaskData["description"].(string)
	}
	taskID := event.TaskID

	summary := "Task created"
	if subject != "" {
		summary = fmt.Sprintf("Task created: %s", subject)
	} else if taskID != "" {
		summary = fmt.Sprintf("Task created: task_id=%s", taskID)
	}

	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:       uuid.New().String(),
		AgentID:       resolveEventAgentID(event),
		EventType:     models.EventTaskCreated,
		Timestamp:     now,
		ToolName:      "TaskCreate",
		InputSummary:  summary,
		OutputSummary: description,
		SessionID:     sessionID,
		FeatureID:     featureID,
		Status:        "recorded",
		Source:        "hook",
		ClaudeTaskID:  taskID,
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	if err := db.InsertEvent(database, ev); err != nil {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		debugLog(projectDir, "[error] handler=TaskCreated session=%s: insert event: %v", sessionID[:minSessionLen(sessionID)], err)
	}

	// Mirror as a step on the active feature so the task survives session end
	// — but only when the task names a work-item ID (bug-XXXXXXXX, feat-…, …),
	// so orchestrator-internal process tasks ("Draft the plan YAML") aren't
	// misattributed to whatever item happens to be active (GH-#169,
	// bug-664276df). WIPNOTE_TASK_STEPS=off disables step-mirroring entirely;
	// the agent_event above is still recorded either way.
	if featureID != "" && taskID != "" && !taskStepsDisabled() && taskNamesWorkItem(subject, description) {
		addTaskStepFn(database, sessionID, featureID, taskID, subject, event.TeammateName)
	}

	return &HookResult{Continue: true}, nil
}

// TaskCompleted handles the TaskCompleted Claude Code hook event.
// Marks the corresponding wipnote step as completed and records a
// task_completed agent_event for the timeline.
func TaskCompleted(event *CloudEvent, database *sql.DB) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}

	featureID := cachedGetActiveFeatureID(database, sessionID)
	taskID := event.TaskID
	subject := event.TaskSubject
	if subject == "" {
		subject, _ = event.TaskData["subject"].(string)
	}
	description := event.TaskDescription
	if description == "" {
		description, _ = event.TaskData["description"].(string)
	}

	summary := "Task completed"
	if subject != "" {
		summary = fmt.Sprintf("Task completed: %s", subject)
	} else if taskID != "" {
		summary = fmt.Sprintf("Task completed: task_id=%s", taskID)
	}

	now := time.Now().UTC()
	ev := &models.AgentEvent{
		EventID:      uuid.New().String(),
		AgentID:      resolveEventAgentID(event),
		EventType:    models.EventTaskCompleted,
		Timestamp:    now,
		ToolName:     "TaskComplete",
		InputSummary: summary,
		SessionID:    sessionID,
		FeatureID:    featureID,
		Status:       "completed",
		Source:       "hook",
		ClaudeTaskID: taskID,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	if err := db.InsertEvent(database, ev); err != nil {
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		debugLog(projectDir, "[error] handler=TaskCompleted session=%s: insert event: %v", sessionID[:minSessionLen(sessionID)], err)
	}

	// quality gate only runs when a feature is actively claimed
	if featureID != "" {
		// Opt-in quality gate: run build/test before allowing task completion.
		projectDir := ResolveProjectDir(event.CWD, event.SessionID)
		blockOnFailure := readTaskCompletionConfig(projectDir)
		gate := runTaskCompletionGate(projectDir)
		if !gate.Passed {
			// Record the failure as an event regardless of blocking mode.
			recordSimpleEvent(models.EventQualityGate, "TaskCompletionGate",
				fmt.Sprintf("Quality gate failed: %s", gate.GateName), "failed", event, database)

			if blockOnFailure {
				msg := fmt.Sprintf("Quality gate %q failed. "+
					"To complete this task manually after fixing: wipnote feature complete %s",
					gate.GateName, featureID)
				return nil, &BlockExit2Error{Message: msg}
			}
		}
	}

	// Mark the step as completed on the feature HTML. Mirrors the same
	// work-item-id resolution rule as TaskCreated's addTaskStep gate (GH-#169,
	// bug-664276df): a step is only ticked if it could have been created,
	// i.e. the task names a work-item ID and step-mirroring isn't disabled.
	if featureID != "" && taskID != "" && !taskStepsDisabled() && taskNamesWorkItem(subject, description) {
		completeTaskStepFn(database, sessionID, featureID, taskID, event.TeammateName)
	}

	return &HookResult{Continue: true}, nil
}
