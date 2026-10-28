package hooks

// Session titles by work item (plan-28b4a7f6 slice 15). A claim happens in a
// Bash call, where no hook output can rename the session, so `<kind> start`
// leaves a mark with the session and the next UserPromptSubmit turns it into
// Claude Code's hookSpecificOutput.sessionTitle (same effect as /rename;
// https://code.claude.com/docs/en/hooks). The mark is read once and removed.
// Codex documents no title output, so its emitter never carries the field.

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/slug"
)

const (
	pendingTitleFile = ".pending-session-title"
	// sessionTitleWidth matches the herdr pane label width.
	sessionTitleWidth = 48
)

// MarkSessionTitlePending records that sessionID claimed itemID, so its next
// prompt renames it. With no session or item it does nothing.
func MarkSessionTitlePending(projectDir, sessionID, itemID string) error {
	if projectDir == "" || sessionID == "" || strings.TrimSpace(itemID) == "" {
		return nil
	}
	dir := filepath.Join(projectDir, ".wipnote", "sessions", sessionID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, pendingTitleFile), []byte(strings.TrimSpace(itemID)+"\n"), 0o644)
}

// takePendingSessionTitle returns the marked item id and removes the mark.
// A mark another hook removed first is not returned, so one claim renames
// once.
func takePendingSessionTitle(wipnoteDir, sessionID string) string {
	if wipnoteDir == "" || sessionID == "" {
		return ""
	}
	path := filepath.Join(wipnoteDir, "sessions", sessionID, pendingTitleFile)
	data, err := os.ReadFile(path)
	if err != nil || os.Remove(path) != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// pendingSessionTitle is the title for a marked claim: "<item-id> <title>",
// or "" when nothing is marked or the session no longer holds that item.
func pendingSessionTitle(projectDir, sessionID, activeItem string) string {
	id := takePendingSessionTitle(filepath.Join(projectDir, ".wipnote"), sessionID)
	if id == "" || id != activeItem {
		return ""
	}
	return slug.WorkTitle(id, " ", canonicalWorkItemTitle(projectDir, id), sessionTitleWidth)
}
