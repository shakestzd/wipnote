package runstore

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// DefaultSweepAge is how long a finished run folder is kept, as the
// retired herdr pane mod's 7-day sweep.
const DefaultSweepAge = 7 * 24 * time.Hour

// Sweep removes the run folders of finished runs whose end is older than
// maxAge at now, and returns their ids. A run with no exit file is live and
// always kept, whatever its age: closing a lost run is the worker-loss path's
// job (slices 10, 16), not the sweep's. A run whose end file is missing or
// unreadable ages from the exit file's mtime. Symlinks and misnamed entries
// are left alone. Mailboxes are not swept. It never blocks on a lock, so a
// caller may run it in the background.
func (s *Store) Sweep(now time.Time, maxAge time.Duration) ([]string, error) {
	return s.SweepExcept(now, maxAge, nil)
}

// SweepExcept is Sweep that also keeps every folder keep names, such as a
// container run whose own files were written after its last fold (they must
// reach the host's ledgers before the folder may go).
func (s *Store) SweepExcept(now time.Time, maxAge time.Duration, keep func(runDir string) bool) ([]string, error) {
	if maxAge <= 0 {
		return nil, fmt.Errorf("runstore: sweep age must be positive, got %v", maxAge)
	}
	entries, err := os.ReadDir(filepath.Join(s.root, runsDir))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("runstore: sweep: %w", err)
	}
	var removed []string
	for _, e := range entries {
		if !e.IsDir() || !ValidRunID(e.Name()) {
			continue
		}
		rf := &RunFolder{id: e.Name(), dir: filepath.Join(s.root, runsDir, e.Name())}
		ended, ok := rf.endedAt()
		if !ok || now.Sub(ended) < maxAge || (keep != nil && keep(rf.dir)) {
			continue
		}
		if err := os.RemoveAll(rf.dir); err != nil {
			return removed, fmt.Errorf("runstore: sweep %s: %w", rf.id, err)
		}
		removed = append(removed, rf.id)
	}
	return removed, nil
}

// endedAt is when a finished run ended; ok is false for a live run.
func (r *RunFolder) endedAt() (time.Time, bool) {
	fi, err := os.Lstat(filepath.Join(r.dir, FileExit))
	if err != nil || !fi.Mode().IsRegular() {
		return time.Time{}, false
	}
	if end := unixOf(r.text(FileEnd)); !end.IsZero() {
		return end, true
	}
	return fi.ModTime(), true
}
