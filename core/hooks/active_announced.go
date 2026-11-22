package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// activeAnnouncedPrefix starts the name of each per-item marker file. The
// marker for an item exists once the UserPrompt hook announced that item in
// the session. The hook creates it with O_EXCL, so when two hook runs race
// for one session exactly one wins and announces (bug-cb72df80).
const activeAnnouncedPrefix = ".active-announced."

func activeMarkerPath(projectDir, sessionID, itemID string) string {
	safe := strings.NewReplacer("/", "_", "\\", "_").Replace(itemID)
	return filepath.Join(sessionStateDir(projectDir, sessionID), activeAnnouncedPrefix+safe)
}

// claimAnnouncedItem reports whether the caller won the right to announce the
// item. It creates the item's marker exclusively. On a win it removes the
// markers of other items, so switching back to an earlier item announces
// again. A marker error other than "already exists" returns true: the worst
// case is a repeated line, and the hook never fails a prompt.
func claimAnnouncedItem(projectDir, sessionID, itemID string) bool {
	path := activeMarkerPath(projectDir, sessionID, itemID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return true
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err != nil {
		return !errors.Is(err, os.ErrExist)
	}
	_ = f.Close()
	removeOtherMarkers(filepath.Dir(path), filepath.Base(path))
	return true
}

func removeOtherMarkers(dir, keep string) {
	matches, _ := filepath.Glob(filepath.Join(dir, activeAnnouncedPrefix+"*"))
	for _, m := range matches {
		if filepath.Base(m) != keep {
			_ = os.Remove(m)
		}
	}
}

// clearAnnouncedItem forgets every announced item. SessionStart calls it for
// every source (startup, resume, compact, clear) because each can drop the
// earlier line from the model's context.
func clearAnnouncedItem(projectDir, sessionID string) {
	removeOtherMarkers(sessionStateDir(projectDir, sessionID), "")
}
