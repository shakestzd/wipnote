package main

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// sessionsRestoreSentinel marks the one-time sessions restore as done. It sits
// beside the other migration sentinels; sync never commits *.done files.
const sessionsRestoreSentinel = "sessions-restore.done"

// runSessionsRestore runs the restore pass and logs the outcome. The writer
// daemon calls it in a goroutine; it is never on the launch path.
func runSessionsRestore(wipnoteDir string) {
	n, err := restoreUntrackedSessions(wipnoteDir)
	if err != nil {
		log.Printf("sessions-restore: %v", err)
	} else if n > 0 {
		log.Printf("sessions-restore: restored %d file(s)", n)
	}
}

// restoreUntrackedSessions is a one-time pass (feat-9457496e). When a pull
// brings in the commit that untracked .wipnote/sessions, git deletes the
// tracked session files from the working tree. Session telemetry is
// per-machine data, so this pass restores those files from the parent of the
// untrack commit, then writes the sentinel. It returns the number of files
// restored. With no untrack commit in history it writes no sentinel, so a
// later pull still gets handled.
func restoreUntrackedSessions(wipnoteDir string) (int, error) {
	migDir := filepath.Join(wipnoteDir, "migrations")
	sentinel := filepath.Join(migDir, sessionsRestoreSentinel)
	if _, err := os.Stat(sentinel); err == nil {
		return 0, nil
	}
	root := filepath.Dir(wipnoteDir)
	commit, err := sessionsUntrackCommit(root)
	if err != nil || commit == "" {
		return 0, err
	}
	restored, err := restoreSessionFiles(root, commit)
	if err != nil {
		return restored, err
	}
	if err := os.MkdirAll(migDir, 0o755); err != nil {
		return restored, err
	}
	return restored, os.WriteFile(sentinel, []byte("restored from "+commit+"^\n"), 0o644)
}

// restoreSessionFiles writes back every session file that commit itself removed,
// taken from its parent, when missing on disk. Existing files are never
// overwritten.
func restoreSessionFiles(root, commit string) (int, error) {
	out, err := gitOut(root, "show", "--diff-filter=D", "--name-only", "--format=", commit, "--", ".wipnote/sessions")
	if err != nil {
		return 0, err
	}
	restored := 0
	for _, rel := range strings.Split(out, "\n") {
		rel = strings.TrimSpace(rel)
		if rel == "" {
			continue
		}
		dst := filepath.Join(root, rel)
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		data, err := exec.Command("git", "-C", root, "show", commit+"^:"+rel).Output()
		if err != nil {
			return restored, fmt.Errorf("git show %s: %w", rel, err)
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return restored, err
		}
		if err := os.WriteFile(dst, data, 0o644); err != nil {
			return restored, err
		}
		restored++
	}
	return restored, nil
}

// sessionsUntrackCommit returns the commit that untracked .wipnote/sessions,
// or "" when there is none. The commit is identified by an explicit trailer
// line in its message, never by "any commit that deleted a session file":
//
//	Wipnote-Untrack: sessions
//
// The human who makes the `git rm -r --cached .wipnote/sessions` commit must
// add that exact line to the message.
func sessionsUntrackCommit(root string) (string, error) {
	out, err := gitOut(root, "log", "--grep=^Wipnote-Untrack: sessions$", "--format=%H", "-1")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

func gitOut(root string, args ...string) (string, error) {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}
