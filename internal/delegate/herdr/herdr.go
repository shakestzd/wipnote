// Package herdr drives the herdr terminal multiplexer through its CLI. It is
// the one place wipnote delegate runs herdr: one-shot runs and interactive
// workers place, label, prompt and close panes through the Client interface.
//
// The CLI is used, not the socket API, because the socket protocol changes
// between herdr releases and the CLI is the documented surface. The package
// is stateless; callers keep pane ids, tab ids and pins in their run folders.
//
// herdr is used only inside a herdr pane (HERDR_ENV=1). Server errors are JSON
// on stderr with exit 1; syntax errors exit 2 and are classified by exit code
// alone, so herdr's text is never scanned for a "timeout" (see errors.go).
package herdr

import (
	"context"
	"strings"
)

// PaneID and TabID are herdr's opaque public ids ("w1:p3", "w1:t2").
type (
	PaneID string
	TabID  string
)

// Direction is a pane split direction.
type Direction string

const (
	Right Direction = "right"
	Down  Direction = "down"
)

// AgentState is herdr's agent lifecycle state. It is a hint only: herdr's own
// docs say a settled state does not prove a job finished.
type AgentState string

const (
	StateIdle    AgentState = "idle"
	StateWorking AgentState = "working"
	StateBlocked AgentState = "blocked"
	StateDone    AgentState = "done"
	StateUnknown AgentState = "unknown"
)

// Pane is what `pane get` reports about a pane.
type Pane struct {
	ID          PaneID
	TerminalID  string
	WorkspaceID string
	TabID       TabID
	AgentStatus AgentState
}

// Agent is what `agent get` and `agent list` report about a pane's agent.
type Agent struct {
	Pane          PaneID
	TerminalID    string
	Name          string
	Kind          string
	State         AgentState
	CompletionSeq int
	// Cwd is the pane's cwd, "used for labels and follow-cwd behavior", and
	// ForegroundCwd the cwd of the foreground process controlling the pane,
	// present only when herdr can resolve it. Both are documented on `agent
	// list` in https://herdr.dev/docs/cli-reference/. Empty means unknown.
	Cwd           string
	ForegroundCwd string
}

// Proc is one foreground process of a pane.
type Proc struct {
	PID  int
	Name string
}

// ProcessInfo is a pane's foreground. Known is false when herdr omitted the
// shell pid or the process list: that means "unknown", never "just a shell".
type ProcessInfo struct {
	Known      bool
	ShellPID   int
	Foreground []Proc
}

// PaneRect is one pane's size in a tab layout, in terminal cells.
type PaneRect struct {
	Pane   PaneID
	Width  int
	Height int
}

// Pin names the exact agent a run started: its pane and the terminal herdr
// gave it. Agent calls check the pin first and refuse, typing nothing, when
// the pane now holds another terminal or the lookup fails.
type Pin struct {
	Pane       PaneID
	TerminalID string
}

// PinOf returns the pin for an agent as herdr reports it.
func PinOf(a Agent) Pin { return Pin{Pane: a.Pane, TerminalID: a.TerminalID} }

// Client is the herdr surface wipnote delegate uses. Split with an empty
// target splits the calling pane (--current).
type Client interface {
	TabCreate(ctx context.Context, workspace, label, cwd string) (TabID, PaneID, error)
	TabClose(ctx context.Context, tab TabID) error
	Split(ctx context.Context, target PaneID, dir Direction, cwd string) (PaneID, error)
	Run(ctx context.Context, pane PaneID, shell string) error
	Get(ctx context.Context, pane PaneID) (Pane, error)
	Close(ctx context.Context, pane PaneID) error
	Rename(ctx context.Context, pane PaneID, title string) error
	Processes(ctx context.Context, pane PaneID) (ProcessInfo, error)
	Layout(ctx context.Context, pane PaneID) ([]PaneRect, error)
	PaneRead(ctx context.Context, pane PaneID, lines int) (string, error)
	SendText(ctx context.Context, pane PaneID, text string) error
	SendKeys(ctx context.Context, pane PaneID, key string) error
	Prompt(ctx context.Context, pin Pin, text string) error
	Read(ctx context.Context, pin Pin, lines int) (string, error)
	State(ctx context.Context, pin Pin) (Agent, error)
	AgentRename(ctx context.Context, pin Pin, name string) error
	AgentList(ctx context.Context) ([]Agent, error)
	CallerWorkspace(ctx context.Context) (string, error)
}

// Env is the caller's herdr context. CallerPane is set only when InHerdr:
// HERDR_PANE_ID outside herdr names nothing we may act on.
type Env struct {
	InHerdr    bool
	CallerPane PaneID
}

// EnvFrom reads the herdr context through getenv (os.Getenv in production).
// Only HERDR_ENV=1 counts as inside herdr.
func EnvFrom(getenv func(string) string) Env {
	if getenv("HERDR_ENV") != "1" {
		return Env{}
	}
	return Env{InHerdr: true, CallerPane: PaneID(strings.TrimSpace(getenv("HERDR_PANE_ID")))}
}
