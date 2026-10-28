// Package procsweep finds live processes that agent sessions started —
// including `cmd &`, nohup, dev servers and watchers that hooks never see —
// and reports those whose session has ended as orphans.
//
// A process is matched to its session by the session id it inherited in its
// environment, never by parent pid: an orphan is re-parented when its shell
// exits, so its parent link is gone, but its environment is not.
//
// The package is report-only by construction. It contains no call that sends
// a signal to any process (TestPackageNeverSignals enforces this); the most it
// does is print a stop command for the owner to copy. It reads only processes
// of the current user, is bounded in time, and must never run in a hook.
package procsweep

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"
)

// ErrEnvUnreadable means the platform would not show us process environments,
// so no process could be matched to a session. Callers print it as is rather
// than reporting "no processes", which would be a silent guess.
var ErrEnvUnreadable = errors.New("process environment not readable on this platform")

// ErrTimeout means the process listing did not finish within its time bound.
var ErrTimeout = errors.New("process sweep timed out")

// Proc is one swept process: a live process attributed to a known session.
type Proc struct {
	PID       int       `json:"pid"`
	Command   string    `json:"command"`
	SessionID string    `json:"session_id"`
	Project   string    `json:"project"`
	StartedAt time.Time `json:"started_at"`
	// Orphan is true when the process's session has ended per the sessions
	// ledger while the process still runs.
	Orphan bool `json:"orphan"`
	// StopHint is a command for the owner to copy. wipnote never runs it.
	StopHint string `json:"stop_hint"`
}

// RawProc is one row of the process table as a Lister reports it. Env holds
// only the session-identifying variables (see sessionEnvKeys), never the full
// environment, so secrets in other variables are not retained.
type RawProc struct {
	PID       int
	PPID      int
	Command   string
	StartedAt time.Time
	Env       map[string]string
}

// Lister lists the current user's live processes.
type Lister interface {
	List(ctx context.Context) ([]RawProc, error)
}

// ListerFunc adapts a function to Lister (fake process tables in tests).
type ListerFunc func(ctx context.Context) ([]RawProc, error)

// List calls f.
func (f ListerFunc) List(ctx context.Context) ([]RawProc, error) { return f(ctx) }

// maxCommandRunes caps a reported command line.
const maxCommandRunes = 120

// trimCommand collapses whitespace and caps the command at maxCommandRunes.
func trimCommand(cmd string) string {
	cmd = strings.Join(strings.Fields(cmd), " ")
	r := []rune(cmd)
	if len(r) <= maxCommandRunes {
		return cmd
	}
	return string(r[:maxCommandRunes-1]) + "…"
}

// stopHint is the copyable command shown for a process.
func stopHint(pid int) string { return "kill " + strconv.Itoa(pid) }
