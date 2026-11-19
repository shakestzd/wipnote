package hooks

import (
	"os"
	"path/filepath"
	"strings"
)

// activeAnnouncedFile holds the item id the UserPrompt hook last announced in
// a session. claimSessionOnce is not reused: it keeps no value, and the line
// must be sent again when the item changes (feat-61944b76).
const activeAnnouncedFile = ".active-announced"

func activeAnnouncedPath(projectDir, sessionID string) string {
	return filepath.Join(sessionStateDir(projectDir, sessionID), activeAnnouncedFile)
}

// lastAnnouncedItem returns the last announced item id, or "" when none.
func lastAnnouncedItem(projectDir, sessionID string) string {
	data, err := os.ReadFile(activeAnnouncedPath(projectDir, sessionID))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// rememberAnnouncedItem records the announced item. A write failure is
// ignored: the worst case is the line is sent again.
func rememberAnnouncedItem(projectDir, sessionID, featureID string) {
	path := activeAnnouncedPath(projectDir, sessionID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return
	}
	_ = os.WriteFile(path, []byte(featureID+"\n"), 0o644)
}

// clearAnnouncedItem forgets the announced item. SessionStart calls it for
// every source (startup, resume, compact, clear) because each can drop the
// earlier line from the model's context.
func clearAnnouncedItem(projectDir, sessionID string) {
	_ = os.Remove(activeAnnouncedPath(projectDir, sessionID))
}
