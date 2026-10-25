package claimledger

import (
	"os"
	"path/filepath"
	"strings"
	"time"
)

// staleTempAge is how old a ".claims-*.tmp" file must be before a later write
// removes it. A writer that is cancelled between CreateTemp and Rename (a
// SessionEnd hook cut off at its time limit) cannot run its deferred Remove, so
// the file would otherwise stay in the git-tracked claims directory forever.
// The age floor keeps a concurrent writer's live temp file safe: a write takes
// milliseconds, never minutes.
const staleTempAge = 5 * time.Minute

// removeStaleTemps deletes ".claims-*.tmp" files in dir older than staleTempAge.
// Best-effort: the claim write that called it must not fail because cleanup did.
func removeStaleTemps(dir string, now time.Time) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasPrefix(name, ".claims-") || !strings.HasSuffix(name, ".tmp") {
			continue
		}
		info, err := e.Info()
		if err != nil || now.Sub(info.ModTime()) < staleTempAge {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}
