package hooks

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
// survives the session and is visible to other sessions.
func TaskCreated(event *CloudEvent) (*HookResult, error) {
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
// step TaskCreated mirrored and never blocks: hooks do not gate on quality,
// `wipnote check --gate` does that on demand (feat-726637b8).
func TaskCompleted(event *CloudEvent) (*HookResult, error) {
	sessionID := EnvSessionID(event.SessionID)
	if sessionID == "" {
		return &HookResult{Continue: true}, nil
	}
	subject, description := taskFields(event)
	if featureID := taskStepTarget(event, sessionID, subject, description); featureID != "" {
		completeTaskStepFn(featureID, event.TaskID)
	}
	return &HookResult{Continue: true}, nil
}
