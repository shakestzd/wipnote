package session

import (
	"errors"
	"fmt"
)

// State is a worker's readiness. One predicate (Ready) decides it, and
// probe, send and wait all read it from there (D2: orch's probe and send
// disagreed, and its state stuck at working on an idle pane).
type State string

const (
	StateStarting     State = "starting"       // spawned; no session bound yet
	StateReady        State = "ready"          // bound, no turn running, nothing owed
	StateWorking      State = "working"        // a turn is running (hooks say so)
	StateQuestion     State = "question"       // the worker asks something
	StateDone         State = "done"           // the turn ended and the task's result file exists
	StateIdleNoResult State = "idle_no_result" // the turn ended with no result file
	StateIdleShell    State = "idle_shell"     // the turn ended, a background shell still runs
	StateBlocked      State = "blocked"        // a folder-trust dialog holds the pane
	StateGone         State = "gone"           // the pane or its agent is gone
)

// AcceptsInput reports whether a new task may be typed or sent now.
func (s State) AcceptsInput() bool {
	switch s {
	case StateReady, StateDone, StateIdleNoResult, StateIdleShell:
		return true
	}
	return false
}

// Turn is the current session's turn, from hook events only.
type Turn int

const (
	TurnNone    Turn = iota // bound, no prompt yet
	TurnRunning             // a prompt event with no stop after it
	TurnEnded               // a stop event after the last prompt
)

// Observation is everything Ready looks at. It holds no input-line text: a
// grey next-prompt suggestion on the line can never make a worker working
// (D8). The send and stop guards read the line separately (Input).
type Observation struct {
	PaneGone     bool
	GoneWhy      string
	Dialog       string // a folder-trust dialog named by its labels, or ""
	Bound        bool   // a session_start event from the worker's hook
	Turn         Turn
	Question     bool // a question, permission request or outbox q-file is pending
	TaskPending  bool // the newest task has no result file
	TaskAnswered bool // the newest task has its result file
	ShellRunning bool // a shell tool started and has not ended
}

// Readiness is a state and, for gone and blocked, why.
type Readiness struct {
	State  State  `json:"state"`
	Detail string `json:"detail,omitempty"`
}

// Ready is the one readiness predicate.
func Ready(o Observation) Readiness {
	switch {
	case o.PaneGone:
		return Readiness{StateGone, o.GoneWhy}
	case o.Dialog != "":
		return Readiness{StateBlocked, o.Dialog + "; delegate presses no key on it: answer it in the pane"}
	case o.Question: // an outbox q-file counts before any hook has fired (D7)
		return Readiness{State: StateQuestion}
	case !o.Bound:
		return Readiness{State: StateStarting}
	case o.Turn == TurnRunning:
		return Readiness{State: StateWorking}
	case o.Turn == TurnEnded && o.TaskAnswered:
		return Readiness{State: StateDone}
	case o.Turn == TurnEnded && o.ShellRunning:
		return Readiness{State: StateIdleShell}
	case o.Turn == TurnEnded && o.TaskPending:
		return Readiness{State: StateIdleNoResult}
	}
	return Readiness{State: StateReady}
}

// Probe is the state a probe reports.
func Probe(o Observation) State { return Ready(o).State }

// WaitOutcome is what a wait settles on: every state but starting and
// working ends a wait, with that state as its outcome.
func WaitOutcome(o Observation) (State, bool) {
	s := Ready(o).State
	return s, s != StateStarting && s != StateWorking
}

// Send refusals.
var (
	ErrDraft        = errors.New("delegate: a draft is on the worker's input line")
	ErrNotAccepting = errors.New("delegate: the worker does not accept input now")
)

// SendCheck is the send guard: the state must accept input, and the input
// line must hold no draft. A failed styled read counts as a draft here, and
// only here.
func SendCheck(o Observation, in Input) error {
	r := Ready(o)
	if !r.State.AcceptsInput() {
		return fmt.Errorf("%w (%s)", ErrNotAccepting, r.State)
	}
	if in.Draft() {
		return fmt.Errorf("%w: %s", ErrDraft, in.why())
	}
	return nil
}
