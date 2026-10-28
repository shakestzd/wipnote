// Package containerwrite is how a delegate worker running inside the
// project's devcontainer writes wipnote state (plan-28b4a7f6 slice 21).
//
// Across the host-to-container bind mount, flock is not exclusive (each side
// gets a lock the other holds) and appends from both sides lose lines; rename
// stays atomic (Phase B B7). So a container worker writes only files it alone
// owns, in its own run folder, and replaces each by rename. It never writes a
// shared .wipnote/ file such as the sessions or gate ledger: wipnote on the
// host folds the worker's own files into those (internal/delegate/container).
//
// A process is a container worker when `devcontainer exec` gave it both
// PlaceEnv=container and a delegate run id in RunEnv. A person's own session
// in the devcontainer carries neither and writes as before.
package containerwrite

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/filelock"
)

// Environment a container worker carries (set with devcontainer exec
// --remote-env).
const (
	PlaceEnv       = "WIPNOTE_DELEGATE_PLACE"
	PlaceContainer = "container"
	RunEnv         = "WIPNOTE_DELEGATE_RUN"
)

// ErrNotWorker is returned by every writer outside a container worker, so a
// host session is never routed away from the shared ledgers by mistake.
var ErrNotWorker = errors.New("containerwrite: not a container worker")

var (
	runRe  = regexp.MustCompile(`^dlg-[0-9a-f]{12,64}$`)
	nameRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,159}$`)
)

// Active reports whether this process is a container worker.
func Active() bool {
	return os.Getenv(PlaceEnv) == PlaceContainer && runRe.MatchString(strings.TrimSpace(os.Getenv(RunEnv)))
}

// RunDir is the worker's own run folder under wipnoteDir. It must exist and
// be a real folder, not a symlink.
func RunDir(wipnoteDir string) (string, error) {
	if !Active() {
		return "", ErrNotWorker
	}
	dir := filepath.Join(wipnoteDir, "delegate", "runs", strings.TrimSpace(os.Getenv(RunEnv)))
	fi, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("containerwrite: run folder: %w", err)
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("containerwrite: %s is not a folder", dir)
	}
	return dir, nil
}

// WriteJSON replaces the worker's own file name in its run folder with v,
// by temp file, fsync and rename.
func WriteJSON(wipnoteDir, name string, v any) error {
	dir, err := RunDir(wipnoteDir)
	if err != nil {
		return err
	}
	if !nameRe.MatchString(name) {
		return fmt.Errorf("containerwrite: bad file name %q", name)
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	return filelock.WriteFileAtomic(filepath.Join(dir, name), append(data, '\n'), 0o600)
}

// AppendLine adds line to the worker's own file at path by rewriting it
// whole and renaming it into place. The lock that serialises this side's
// writers lives in the container's own temp folder, never on the mount.
func AppendLine(path string, line []byte) error {
	release := filelock.Guard(localLock(path))
	defer release()
	old, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	if n := len(old); n > 0 && old[n-1] != '\n' {
		old = append(old, '\n')
	}
	return filelock.WriteFileAtomic(path, append(old, line...), 0o600)
}

// localLock names a lock file in the local temp folder for path.
func localLock(path string) string {
	key := strings.NewReplacer("/", "_", "\\", "_", ":", "_").Replace(filepath.Clean(path))
	if len(key) > 180 {
		key = key[len(key)-180:]
	}
	return filepath.Join(os.TempDir(), "wipnote-own-"+key)
}

// withOwnFile decodes the own file name into v under this side's lock, runs
// fn, and writes v back by rename when fn reports a change.
func withOwnFile(wipnoteDir, name string, v any, fn func() bool) error {
	dir, err := RunDir(wipnoteDir)
	if err != nil {
		return err
	}
	path := filepath.Join(dir, name)
	release := filelock.Guard(localLock(path))
	defer release()
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, v); err != nil {
			return fmt.Errorf("containerwrite: unreadable %s: %w", name, err)
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	if !fn() {
		return nil
	}
	return WriteJSON(wipnoteDir, name, v)
}
