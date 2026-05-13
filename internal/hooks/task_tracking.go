package hooks

import (
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// selfBinary returns the path to the wipnote binary for self-invocation.
// Resolution order:
//  1. CLAUDE_PLUGIN_ROOT env var (always correct in hook context)
//  2. os.Executable() (correct when called from the binary itself)
//  3. "wipnote" on PATH (fallback)
func selfBinary() string {
	if root := os.Getenv("CLAUDE_PLUGIN_ROOT"); root != "" {
		candidate := filepath.Join(root, "hooks", "bin", "wipnote")
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	if exe, err := os.Executable(); err == nil {
		return exe
	}
	return "wipnote"
}

// addTaskStep shells out to the wipnote CLI to add a task-associated step to
// the active feature. The CLI sets StepID="task-<taskID>" so completeTaskStep
// can find and tick it. Shells out rather than importing workitem directly
// (architectural constraint: hooks must not import workitem).
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
