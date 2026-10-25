package main

import (
	"time"
)

// normalizeTimes returns sensible defaults for zero-value timestamps.
func normalizeTimes(createdAt, updatedAt time.Time) (time.Time, time.Time) {
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	if updatedAt.IsZero() {
		updatedAt = createdAt
	}
	return createdAt, updatedAt
}

// normalizeStatus maps HTML statuses to the features table CHECK constraint values.
// features table allows: todo, in-progress, blocked, done, active, ended, stale
func normalizeStatus(status string) string {
	switch status {
	case "todo", "in-progress", "blocked", "done", "active", "ended", "stale":
		return status
	case "completed":
		return "done"
	case "in_progress":
		return "in-progress"
	case "archived", "cancelled":
		return "ended"
	case "pending", "identified":
		return "todo"
	default:
		return "todo"
	}
}

// mapNodeType converts HTML node types to the features table CHECK constraint values.
// features table allows: feature, bug, spike, chore, epic, task
func mapNodeType(nodeType string) string {
	switch nodeType {
	case "feature":
		return "feature"
	case "bug":
		return "bug"
	case "spike":
		return "spike"
	case "track":
		return "epic"
	case "chore":
		return "chore"
	case "plan", "spec":
		return "task"
	default:
		return "feature"
	}
}
