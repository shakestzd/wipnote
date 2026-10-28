package herdr

// The herdr command forms. Each method names its source. "Verified" means the
// reference research ran the form on a real herdr 0.9.5. "Documented" means it
// is shown in herdr's skill text but was not run there. "UNVERIFIED" forms come
// from herdr's help listing or API schema only. Plan slice 2 checks the
// documented and unverified forms on a real herdr.

import (
	"context"
	"fmt"
	"strconv"
)

type wirePane struct {
	PaneID      string `json:"pane_id"`
	TerminalID  string `json:"terminal_id"`
	WorkspaceID string `json:"workspace_id"`
	TabID       string `json:"tab_id"`
	AgentStatus string `json:"agent_status"`
}

func (w wirePane) pane() Pane {
	return Pane{ID: PaneID(w.PaneID), TerminalID: w.TerminalID, WorkspaceID: w.WorkspaceID,
		TabID: TabID(w.TabID), AgentStatus: AgentState(w.AgentStatus)}
}

type wireAgent struct {
	PaneID        string `json:"pane_id"`
	TerminalID    string `json:"terminal_id"`
	Name          string `json:"name"`
	Agent         string `json:"agent"`
	AgentStatus   string `json:"agent_status"`
	CompletionSeq int    `json:"completion_seq"`
	Cwd           string `json:"cwd"`
	ForegroundCwd string `json:"foreground_cwd"`
}

func (w wireAgent) agent() Agent {
	state := AgentState(w.AgentStatus)
	if state == "" {
		state = StateUnknown
	}
	return Agent{Pane: PaneID(w.PaneID), TerminalID: w.TerminalID, Name: w.Name, Kind: w.Agent,
		State: state, CompletionSeq: w.CompletionSeq, Cwd: w.Cwd, ForegroundCwd: w.ForegroundCwd}
}

// TabCreate makes a background tab in workspace and returns it and its root
// pane. Always pass the workspace: without it a tab can land in another one.
// Verified: `tab create --workspace W --label L --cwd D --no-focus`.
func (c *CLI) TabCreate(ctx context.Context, workspace, label, cwd string) (TabID, PaneID, error) {
	var r struct {
		Tab struct {
			TabID string `json:"tab_id"`
		} `json:"tab"`
		RootPane wirePane `json:"root_pane"`
	}
	args := []string{"tab", "create", "--workspace", workspace, "--label", label, "--cwd", cwd, "--no-focus"}
	if err := c.call(ctx, &r, args...); err != nil {
		return "", "", err
	}
	if r.Tab.TabID == "" || r.RootPane.PaneID == "" {
		return "", "", fmt.Errorf("%w: tab create gave no tab or root pane id", ErrBadReply)
	}
	return TabID(r.Tab.TabID), PaneID(r.RootPane.PaneID), nil
}

// TabClose closes a tab. UNVERIFIED: `tab close <tab>` is in herdr's help
// listing but was not run on a real herdr.
func (c *CLI) TabClose(ctx context.Context, tab TabID) error {
	return c.call(ctx, nil, "tab", "close", string(tab))
}

// Split splits target (the calling pane when empty) and returns the new pane.
// Verified: `pane split --current --direction D --cwd C --no-focus`.
// UNVERIFIED: a positional pane id in place of --current.
func (c *CLI) Split(ctx context.Context, target PaneID, dir Direction, cwd string) (PaneID, error) {
	t := string(target)
	if t == "" {
		t = "--current"
	}
	var r struct {
		Pane wirePane `json:"pane"`
	}
	if err := c.call(ctx, &r, "pane", "split", t, "--direction", string(dir), "--cwd", cwd, "--no-focus"); err != nil {
		return "", err
	}
	if r.Pane.PaneID == "" {
		return "", fmt.Errorf("%w: pane split gave no pane id", ErrBadReply)
	}
	return PaneID(r.Pane.PaneID), nil
}

// Run queues a shell command line (one string) and Enter in pane; success
// means admission, not execution. Verified: `pane run <pane> <text>`.
func (c *CLI) Run(ctx context.Context, pane PaneID, shell string) error {
	return c.call(ctx, nil, "pane", "run", string(pane), shell)
}

// Get reports a pane. Verified: `pane get <pane>`. Whether a gone pane yields
// pane_not_found or not_found is UNVERIFIED; IsAbsent accepts both.
func (c *CLI) Get(ctx context.Context, pane PaneID) (Pane, error) {
	var r struct {
		Pane wirePane `json:"pane"`
	}
	if err := c.call(ctx, &r, "pane", "get", string(pane)); err != nil {
		return Pane{}, err
	}
	return r.Pane.pane(), nil
}

// Close closes a pane. Documented: `pane close <pane>`.
func (c *CLI) Close(ctx context.Context, pane PaneID) error {
	return c.call(ctx, nil, "pane", "close", string(pane))
}

// Rename sets a pane's title. Documented: herdr's CLI reference (0.9.5)
// lists `pane rename <pane_id> <label>|--clear`; not run on a real herdr.
func (c *CLI) Rename(ctx context.Context, pane PaneID, title string) error {
	return c.call(ctx, nil, "pane", "rename", string(pane), title)
}

// Processes reports a pane's foreground. Verified: the pane is an option,
// `pane process-info --pane <id>`; the positional form fails.
func (c *CLI) Processes(ctx context.Context, pane PaneID) (ProcessInfo, error) {
	var r struct {
		Info struct {
			ShellPID *int `json:"shell_pid"`
			Procs    []struct {
				PID  int    `json:"pid"`
				Name string `json:"name"`
			} `json:"foreground_processes"`
		} `json:"process_info"`
	}
	if err := c.call(ctx, &r, "pane", "process-info", "--pane", string(pane)); err != nil {
		return ProcessInfo{}, err
	}
	info := ProcessInfo{Known: r.Info.ShellPID != nil && len(r.Info.Procs) > 0}
	if r.Info.ShellPID != nil {
		info.ShellPID = *r.Info.ShellPID
	}
	for _, p := range r.Info.Procs {
		info.Foreground = append(info.Foreground, Proc{PID: p.PID, Name: p.Name})
	}
	return info, nil
}

// Layout reports the sizes of the panes in pane's tab. UNVERIFIED: the reply
// shape of `pane layout --pane <id>` is read from the API schema
// (result.layout.panes[].pane_id, .rect.width, .rect.height).
func (c *CLI) Layout(ctx context.Context, pane PaneID) ([]PaneRect, error) {
	var r struct {
		Layout struct {
			Panes []struct {
				PaneID string `json:"pane_id"`
				Rect   struct {
					Width  int `json:"width"`
					Height int `json:"height"`
				} `json:"rect"`
			} `json:"panes"`
		} `json:"layout"`
	}
	if err := c.call(ctx, &r, "pane", "layout", "--pane", string(pane)); err != nil {
		return nil, err
	}
	var rects []PaneRect
	for _, p := range r.Layout.Panes {
		rects = append(rects, PaneRect{Pane: PaneID(p.PaneID), Width: p.Rect.Width, Height: p.Rect.Height})
	}
	return rects, nil
}

// PaneRead reads a pane's recent output. UNVERIFIED: `pane read <pane>
// --source recent-unwrapped --lines N` is from herdr's skill text, not run.
func (c *CLI) PaneRead(ctx context.Context, pane PaneID, lines int) (string, error) {
	return c.readText(ctx, "pane", "read", string(pane), "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines))
}

// SendText types literal text with no Enter. Verified: `pane send-text`.
func (c *CLI) SendText(ctx context.Context, pane PaneID, text string) error {
	return c.call(ctx, nil, "pane", "send-text", string(pane), text)
}

// SendKeys sends one logical key (enter, esc, ctrl+c). Verified for one key
// per call: `pane send-keys <pane> <key>`.
func (c *CLI) SendKeys(ctx context.Context, pane PaneID, key string) error {
	return c.call(ctx, nil, "pane", "send-keys", string(pane), key)
}

// PaneReadStyled reads a pane's recent output with its SGR styling, so a
// dim or grey next-prompt suggestion can be told from a typed draft (D8).
// UNVERIFIED: `pane read <pane> --source recent --lines N --format ansi` is
// Phase B check D8a/D8b; a usage error (exit 2) means the form is wrong, and
// callers then treat the input line as a possible draft.
func (c *CLI) PaneReadStyled(ctx context.Context, pane PaneID, lines int) (string, error) {
	return c.readText(ctx, "pane", "read", string(pane), "--source", "recent", "--lines", strconv.Itoa(lines), "--format", "ansi")
}
