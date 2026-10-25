// Package runstore is the live run store of wipnote delegate (plan-28b4a7f6
// slice 5): one folder per run, per-writer event files and a file mailbox,
// under <project>/.wipnote/delegate/.
//
// Layout (<root> = .wipnote/delegate):
//
//	<root>/runs/<run-id>/            run folder (the feat-a1df60c2 contract,
//	                                 runfolder.go) plus events-<writer>.ndjson
//	<root>/mail/<name>/inbox/        caller -> worker, <seq>-<msg-id>.json
//	<root>/mail/<name>/outbox/       worker -> caller, <seq>-<msg-id>.json and
//	                                 <task-id>-result.json
//	<root>/mail/<name>/ack/<msg-id>  worker touches it when it starts a task
//
// The store is runtime state, gitignored by the managed .wipnote/.gitignore
// (core/worktree); only delegations-ledger rows are committed.
//
// One writer per file: every message is its own file written by temp file,
// fsync and rename (core/filelock.WriteFileAtomic), and each process appends
// only to its own events file. There is no lock and no shared append, because
// O_APPEND is not atomic on APFS and flock across a host-container bind mount
// is unverified. Readers skip temp files, misnamed files, symlinks and partial
// content instead of failing.
package runstore

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// DirName is the store's folder inside .wipnote/.
const DirName = "delegate"

const (
	runsDir = "runs"
	mailDir = "mail"
	dirPerm = 0o700
	// filePerm is the mode of every file the store writes.
	filePerm = 0o600
)

var (
	// runIDPattern: the delegations-ledger row id (slice 4), 12 or more hex
	// so ids do not collide (orch finding P-6).
	runIDPattern = regexp.MustCompile(`^dlg-[0-9a-f]{12,64}$`)
	msgIDPattern = regexp.MustCompile(`^msg-[0-9a-f]{12,64}$`)
	// namePattern: a mailbox (interactive run) name, as orch's session names.
	namePattern = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	// writerPattern: an events writer, usually a session id or "caller".
	writerPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,127}$`)
)

// Store is the live store of one project.
type Store struct {
	root string
}

// New returns the store of the project at projectDir. It creates nothing:
// folders are made when a run or mailbox is first written.
func New(projectDir string) (*Store, error) {
	if projectDir == "" {
		return nil, errors.New("runstore: empty project dir")
	}
	return &Store{root: filepath.Join(projectDir, ".wipnote", DirName)}, nil
}

// Root is <project>/.wipnote/delegate.
func (s *Store) Root() string { return s.root }

// ValidRunID reports whether id is a run id (dlg- plus 12 to 64 hex).
func ValidRunID(id string) bool { return runIDPattern.MatchString(id) }

// ValidMsgID reports whether id is a message id (msg- plus 12 to 64 hex).
func ValidMsgID(id string) bool { return msgIDPattern.MatchString(id) }

// ValidName reports whether name is a mailbox name.
func ValidName(name string) bool { return namePattern.MatchString(name) }

// NewMsgID returns msg- plus 16 random hex (64 bits).
func NewMsgID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic(fmt.Sprintf("runstore: crypto/rand: %v", err))
	}
	return "msg-" + hex.EncodeToString(b[:])
}

// privateMkdir creates dir and its parents with mode 0700 and refuses a
// symlink or non-folder at any level from the store root down.
func (s *Store) privateMkdir(dir string) error {
	rel, err := filepath.Rel(s.root, dir)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("runstore: %s is outside the store", dir)
	}
	if err := os.MkdirAll(dir, dirPerm); err != nil {
		return fmt.Errorf("runstore: mkdir: %w", err)
	}
	for p := dir; ; p = filepath.Dir(p) {
		fi, err := os.Lstat(p)
		if err != nil {
			return fmt.Errorf("runstore: %w", err)
		}
		if !fi.IsDir() {
			return fmt.Errorf("runstore: %s is not a plain folder", p)
		}
		if p == s.root {
			return nil
		}
	}
}

// isPlainFile reports whether path is a regular file, not a symlink.
func isPlainFile(path string) bool {
	fi, err := os.Lstat(path)
	return err == nil && fi.Mode().IsRegular()
}
