package herdr

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// classifyCore turns hc's failure into an *Error: {"error":{"kind","message"}}
// on stderr with exit 3 for not_found and 11 for finished. Both mean the
// terminal is gone to delegate, so they take the code and exit IsAbsent reads.
func classifyCore(args []string, exit int, stderr []byte) *Error {
	e := &Error{Args: strings.Join(args, " "), Exit: exit, Message: firstLine(stderr)}
	var body struct {
		Error *struct {
			Kind    string `json:"kind"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(bytes.TrimSpace(stderr), &body) == nil && body.Error != nil {
		e.Code, e.Message = body.Error.Kind, body.Error.Message
	}
	switch {
	case exit == 2:
		e.Code = CodeUsage
	case exit == 3 || exit == 11:
		e.Exit, e.Code = 1, "pane_not_found"
	case e.Code == "":
		e.Code = fmt.Sprintf("exit_%d", exit)
	}
	return e
}

// wireTerminal is the part of hc's Terminal that delegate reads.
type wireTerminal struct {
	Address  string            `json:"address"`
	Name     *string           `json:"name"`
	State    string            `json:"state"`
	Cwd      string            `json:"cwd"`
	PID      *int              `json:"pid"`
	Turn     int               `json:"turn"`
	Reported bool              `json:"reported"`
	Labels   map[string]string `json:"labels"`
	Status   *struct {
		State string `json:"state"`
		App   string `json:"app"`
	} `json:"status"`
	Foreground *struct {
		PID  int    `json:"pid"`
		Name string `json:"name"`
		Cwd  string `json:"cwd"`
	} `json:"foreground"`
}

func (t wireTerminal) status() AgentState {
	if t.Status == nil {
		return ""
	}
	return AgentState(t.Status.State)
}

func (t wireTerminal) pane() Pane {
	return Pane{ID: PaneID(t.Address), TerminalID: t.Address, WorkspaceID: t.Labels[labelSpace],
		TabID: TabID(t.Labels[labelTab]), AgentStatus: t.status()}
}

func (t wireTerminal) agent() Agent {
	a := Agent{Pane: PaneID(t.Address), TerminalID: t.Address, State: t.status(), CompletionSeq: t.Turn, Cwd: t.Cwd}
	if a.State == "" {
		a.State = StateUnknown
	}
	if t.Name != nil {
		a.Name = *t.Name
	}
	if t.Status != nil {
		a.Kind = t.Status.App
	}
	if t.Foreground != nil {
		a.ForegroundCwd = t.Foreground.Cwd
		if a.Kind == "" {
			a.Kind = t.Foreground.Name
		}
	}
	return a
}

// isAgent is a terminal that reported OSC 7501 status, or that runs a harness
// delegate starts: core adds no per-agent integration, so a harness may report
// nothing, and it must still be found.
func (t wireTerminal) isAgent() bool {
	if t.Reported {
		return true
	}
	return t.Foreground != nil && (t.Foreground.Name == "claude" || t.Foreground.Name == "codex")
}
