package runledger

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/filelock"
)

// shardPrefix namespaces per-session shard file names.
const shardPrefix = "sess-"

// OnCommit, when non-nil, is called after a successful append with the
// wipnote dir, the repo-relative shard path and the action. cmd/wipnote sets
// it to the commit-queue producer, which coalesces every write to one shard
// into one pending commit (core may not import internal/commitqueue).
var OnCommit func(wipnoteDir, relPath, action string)

// Store reads and writes the run shards of one project.
type Store struct {
	wipnoteDir string
	dir        string
}

// NewStore returns the store under wipnoteDir. Nothing is created until the
// first append, so a reader never leaves an empty directory behind.
func NewStore(wipnoteDir string) *Store {
	return &Store{wipnoteDir: wipnoteDir, dir: filepath.Join(wipnoteDir, DirName)}
}

// Dir returns the absolute runs directory.
func (s *Store) Dir() string { return s.dir }

// ShardPath returns the shard one session writes. A session id is folded to a
// safe single path component; an empty one (a wrapper run outside any
// session) writes the "no-session" shard.
func (s *Store) ShardPath(session string) string {
	return filepath.Join(s.dir, shardPrefix+slugify(session)+".html")
}

// RelPath returns path relative to the repo root, for commits.
func (s *Store) RelPath(path string) string {
	if rel, err := filepath.Rel(filepath.Dir(s.wipnoteDir), path); err == nil {
		return filepath.ToSlash(rel)
	}
	return filepath.ToSlash(filepath.Join(filepath.Base(s.wipnoteDir), DirName, filepath.Base(path)))
}

// Append validates e and appends it to its session's shard. It is O(row):
// the file is never rewritten, so a hook can call it on its hot path.
func (s *Store) Append(e Event) error {
	if err := e.Validate(); err != nil {
		return err
	}
	path := s.ShardPath(e.Session)
	release := filelock.Guard(path)
	err := ledgerFormat.AppendRow(path, marshalRow(e))
	release()
	if err != nil {
		return err
	}
	if OnCommit != nil {
		OnCommit(s.wipnoteDir, s.RelPath(path), "run "+e.Phase)
	}
	return nil
}

// ReadAll returns every event across shards, ordered by time (file order
// breaks ties). A missing directory reads as empty.
func (s *Store) ReadAll() ([]Event, error) {
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("runledger: read %s: %w", s.dir, err)
	}
	var out []Event
	for _, ent := range entries {
		name := ent.Name()
		if ent.IsDir() || !strings.HasPrefix(name, shardPrefix) || !strings.HasSuffix(name, ".html") {
			continue
		}
		events, readErr := readFile(filepath.Join(s.dir, name))
		if readErr != nil && !os.IsNotExist(readErr) {
			return nil, readErr
		}
		out = append(out, events...)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].At.Before(out[j].At) })
	return out, nil
}

// Records folds every event into runs. sessionEnded reports whether a session
// is over, which turns a run with no end into "unknown" instead of "running".
func (s *Store) Records(sessionEnded func(string) bool) ([]Record, error) {
	events, err := s.ReadAll()
	if err != nil {
		return nil, err
	}
	return Fold(events, sessionEnded), nil
}

// slugify maps an id onto a safe path component (as core/claimledger does).
func slugify(id string) string {
	id = strings.TrimSpace(id)
	if id == "" {
		return "no-session"
	}
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('-')
		}
	}
	return b.String()
}
