package herdr

// Core is the Client over herdr-core's `hc`, the runtime under the Herdr app.
// Upstream's manual calls the `herdr` CLI "the older terminal multiplexer", and
// neither hc nor the app sets HERDR_ENV, so without this client delegate runs
// headless and spawn refuses inside the app (feat-4d4ca0a1).
//
// hc has terminals, not panes, so a "pane" here is a terminal address ("t7"):
// fixed for the terminal's life, so a pin cannot go stale. What the window
// shows is the app's business: a terminal labelled herdr.space and herdr.tab
// shows up in that tab, and the app chooses how to arrange it.
//
// Verified on herdr-core 0.1.16 by scripts/delegate-core-probe.sh and by real
// Claude workers through spawn, send, wait, stop and run. UNVERIFIED: that the
// app shows a labelled terminal in the tab it names, and exit 11, which neither
// reaches. Both come from upstream's docs.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// Labels on terminals delegate starts. The Herdr app mod reads the last two.
const (
	labelSpace  = "herdr.space"
	labelTab    = "herdr.tab"
	LabelTitle  = "wipnote.title"
	LabelWorker = "wipnote.worker"
)

// coreBins are the names herdr-core installs under. The installer adds hc only
// when no other hc exists, because some window managers ship one, so
// herdr-core is tried first.
var coreBins = []string{"herdr-core", "hc"}

// Core is the Client that runs the herdr-core binary.
type Core struct {
	Env     Env
	Timeout time.Duration // per call; DefaultTimeout when zero
	Bin     string        // binary name; coreBins when empty
}

var _ Client = (*Core)(nil)

// NewClient returns the Client for env: Core for an hc terminal, else the
// herdr CLI. A timeout of zero means DefaultTimeout.
func NewClient(env Env, timeout time.Duration) Client {
	if env.Core {
		return &Core{Env: env, Timeout: timeout}
	}
	return &CLI{Env: env, Timeout: timeout}
}

// CoreBinary returns the path of the herdr-core binary on PATH.
func CoreBinary() (string, error) {
	for _, bin := range coreBins {
		if path, err := exec.LookPath(bin); err == nil {
			return path, nil
		}
	}
	return "", fmt.Errorf("%w: neither %s", ErrAbsent, strings.Join(coreBins, " nor "))
}

func (c *Core) binary() (string, error) {
	if c.Bin == "" {
		return CoreBinary()
	}
	path, err := exec.LookPath(c.Bin)
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrAbsent, err)
	}
	return path, nil
}

// run executes one hc call under the call timeout and returns stdout.
func (c *Core) run(ctx context.Context, args ...string) ([]byte, error) {
	if !c.Env.InHerdr {
		return nil, ErrNotInHerdr
	}
	path, err := c.binary()
	if err != nil {
		return nil, err
	}
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return nil, fmt.Errorf("%w: hc %s after %s", ErrTimeout, strings.Join(args, " "), timeout)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, classifyCore(args, exitErr.ExitCode(), stderr.Bytes())
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// call runs hc with --json and decodes the reply, which hc prints bare rather
// than in the herdr CLI's {"result":...} envelope.
func (c *Core) call(ctx context.Context, result any, args ...string) error {
	out, err := c.run(ctx, append(args, "--json")...)
	if err != nil || result == nil {
		return err
	}
	if err := json.Unmarshal(out, result); err != nil {
		return fmt.Errorf("%w: hc %s: %v", ErrBadReply, strings.Join(args, " "), err)
	}
	return nil
}

func (c *Core) get(ctx context.Context, t string) (wireTerminal, error) {
	var w wireTerminal
	if err := c.call(ctx, &w, "get", t); err != nil {
		return w, err
	}
	if w.Address == "" {
		return w, fmt.Errorf("%w: hc get %s gave no address", ErrBadReply, t)
	}
	if w.State == "finished" {
		// A finished terminal stays listed until removed, but nothing runs in
		// it and nothing can be typed there: to delegate its pane is gone.
		return w, &Error{Args: "get " + t, Exit: 1, Code: "pane_not_found", Message: "terminal finished"}
	}
	return w, nil
}

// startShell starts a shell terminal for delegate to type its one line into.
// --outlive-parent keeps the herdr rule that a worker survives its caller, so
// `delegate spawn --resume` and a person can still reach it. --no-notify stops
// hc typing notes into the caller, which would start a turn there; the mailbox
// and `delegate wait` are how a caller hears from a worker.
func (c *Core) startShell(ctx context.Context, cwd, space, tab string) (PaneID, error) {
	args := []string{"run", "--bg", "--outlive-parent", "--no-notify", "--cwd", cwd}
	if space != "" {
		args = append(args, "--label", labelSpace+"="+space)
		if tab != "" {
			args = append(args, "--label", labelTab+"="+tab)
		}
	}
	var w wireTerminal
	if err := c.call(ctx, &w, args...); err != nil {
		return "", err
	}
	if w.Address == "" {
		return "", fmt.Errorf("%w: hc run gave no address", ErrBadReply)
	}
	return PaneID(w.Address), nil
}

// TabCreate starts the first terminal of a workers tab. An id the app does not
// know becomes a new tab in that space, which gives one workers tab per caller
// as in the TUI. Without a space (bare hc, no app) there is no tab to make: the
// terminal is simply the caller's child, and the returned id only marks the
// record.
func (c *Core) TabCreate(ctx context.Context, workspace, label, cwd string) (TabID, PaneID, error) {
	tab := "wipnote-" + label + "-" + string(c.Env.CallerPane)
	pane, err := c.startShell(ctx, cwd, workspace, tab)
	if err != nil {
		return "", "", err
	}
	return TabID(tab), pane, nil
}

// TabClose does nothing: a tab in the app ends with its last terminal, and
// each worker's terminal is closed before the tab is released.
func (c *Core) TabClose(context.Context, TabID) error { return nil }

// Split starts a terminal in the same place as target (the caller when empty).
// The direction is ignored: the app arranges labelled terminals itself.
func (c *Core) Split(ctx context.Context, target PaneID, _ Direction, cwd string) (PaneID, error) {
	if target == "" {
		target = c.Env.CallerPane
	}
	if target == "" {
		return "", ErrNoCallerPane
	}
	t, err := c.get(ctx, string(target))
	if err != nil {
		return "", err
	}
	return c.startShell(ctx, cwd, t.Labels[labelSpace], t.Labels[labelTab])
}

// Run pastes a shell line and presses Enter: `hc send`.
func (c *Core) Run(ctx context.Context, pane PaneID, shell string) error {
	return c.call(ctx, nil, "send", string(pane), shell)
}

// Get reports a terminal; a finished one is absent.
func (c *Core) Get(ctx context.Context, pane PaneID) (Pane, error) {
	t, err := c.get(ctx, string(pane))
	if err != nil {
		return Pane{}, err
	}
	return t.pane(), nil
}

// Close ends a terminal and removes it. Callers reach this only at a bare
// shell. A terminal that has not ended yet stays until it exits, so rm is best
// effort, and a terminal already gone is reported as such.
func (c *Core) Close(ctx context.Context, pane PaneID) error {
	err := c.call(ctx, nil, "kill", string(pane))
	if err != nil && !IsAbsent(err) {
		return err
	}
	_ = c.call(ctx, nil, "rm", string(pane))
	return err
}

// Rename records a title as a label: hc names are fixed at start and must be
// short slugs, while a title holds spaces. The wipnote board mod draws it.
func (c *Core) Rename(ctx context.Context, pane PaneID, title string) error {
	return c.call(ctx, nil, "label", string(pane), LabelTitle+"="+title)
}

// Processes reports the terminal's program and foreground from `hc get`.
func (c *Core) Processes(ctx context.Context, pane PaneID) (ProcessInfo, error) {
	t, err := c.get(ctx, string(pane))
	if err != nil {
		return ProcessInfo{}, err
	}
	if t.PID == nil || t.Foreground == nil {
		return ProcessInfo{}, nil
	}
	return ProcessInfo{Known: true, ShellPID: *t.PID,
		Foreground: []Proc{{PID: t.Foreground.PID, Name: t.Foreground.Name}}}, nil
}

// Layout reports nothing: hc does not know how the window arranges terminals.
func (c *Core) Layout(context.Context, PaneID) ([]PaneRect, error) { return nil, nil }

func (c *Core) read(ctx context.Context, t string, lines int) (string, error) {
	out, err := c.run(ctx, "read", t, "--lines", strconv.Itoa(lines))
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// PaneRead reads the screen and lines of history.
func (c *Core) PaneRead(ctx context.Context, pane PaneID, lines int) (string, error) {
	return c.read(ctx, string(pane), lines)
}

// observePaint is how long a styled read watches a terminal. A new viewer gets
// the whole screen at once, so this only has to outlast that first paint.
const observePaint = 800 * time.Millisecond

// PaneReadStyled reads the screen with its styling, so a harness's grey
// next-prompt suggestion can be told from a person's draft (D8). hc read has no
// styled form, but a viewer's first paint does, and --observe never types or
// resizes. The lines argument does not apply: a paint is the whole screen.
func (c *Core) PaneReadStyled(ctx context.Context, pane PaneID, _ int) (string, error) {
	if !c.Env.InHerdr {
		return "", ErrNotInHerdr
	}
	path, err := c.binary()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, observePaint)
	defer cancel()
	args := []string{"attach", "--observe", string(pane)}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err = cmd.Run()
	var exitErr *exec.ExitError
	if ctx.Err() == nil && errors.As(err, &exitErr) {
		return "", classifyCore(args, exitErr.ExitCode(), stderr.Bytes())
	}
	if stdout.Len() == 0 {
		return "", fmt.Errorf("%w: hc attach --observe %s painted nothing", ErrBadReply, pane)
	}
	return stdout.String(), nil
}

// keyNames maps literal text to hc key names: hc send always presses Enter, so
// typing without it goes key by key. It refuses a character hc has no name for
// rather than dropping it from the line.
func keyNames(text string) ([]string, error) {
	keys := make([]string, 0, len(text))
	for _, r := range text {
		switch {
		case r == ' ':
			keys = append(keys, "space")
		case r < unicode.MaxASCII && unicode.IsUpper(r):
			keys = append(keys, "shift+"+string(unicode.ToLower(r)))
		case r < unicode.MaxASCII && (unicode.IsLower(r) || unicode.IsDigit(r) || strings.ContainsRune("`[],=-.';/", r)):
			keys = append(keys, string(r))
		default:
			return nil, fmt.Errorf("hc has no key for %q: nothing was typed", r)
		}
	}
	return keys, nil
}

// SendText types literal text with no Enter.
func (c *Core) SendText(ctx context.Context, pane PaneID, text string) error {
	keys, err := keyNames(text)
	if err != nil {
		return err
	}
	return c.call(ctx, nil, append([]string{"keys", string(pane)}, keys...)...)
}

// SendKeys presses one key. hc's names match herdr's: enter, esc, ctrl+c.
func (c *Core) SendKeys(ctx context.Context, pane PaneID, key string) error {
	return c.call(ctx, nil, "keys", string(pane), key)
}

// pinned checks the pinned terminal lives. An address never moves to another
// terminal, so a live terminal is the pinned one when the pin names it.
func (c *Core) pinned(ctx context.Context, pin Pin) (wireTerminal, error) {
	if pin.Pane == "" {
		return wireTerminal{}, fmt.Errorf("%w: no terminal pinned", ErrPin)
	}
	t, err := c.get(ctx, string(pin.Pane))
	if err != nil {
		return t, fmt.Errorf("%w: hc cannot say who is in %s; nothing was sent: %w", ErrPin, pin.Pane, err)
	}
	if pin.TerminalID != "" && t.Address != pin.TerminalID {
		return t, fmt.Errorf("%w: %s is terminal %s, not %s; nothing was sent", ErrPin, pin.Pane, t.Address, pin.TerminalID)
	}
	return t, nil
}

// Prompt submits text and Enter. hc refuses while the program is blocked, so a
// person's permission decision is never answered by accident.
func (c *Core) Prompt(ctx context.Context, pin Pin, text string) error {
	if _, err := c.pinned(ctx, pin); err != nil {
		return err
	}
	return c.call(ctx, nil, "send", string(pin.Pane), text)
}

// Read returns the pinned terminal's recent screen.
func (c *Core) Read(ctx context.Context, pin Pin, lines int) (string, error) {
	if _, err := c.pinned(ctx, pin); err != nil {
		return "", err
	}
	return c.read(ctx, string(pin.Pane), lines)
}

// State reports the pinned terminal as an agent.
func (c *Core) State(ctx context.Context, pin Pin) (Agent, error) {
	t, err := c.pinned(ctx, pin)
	if err != nil {
		return Agent{}, err
	}
	return t.agent(), nil
}

// AgentRename records the worker's name as a label; see Rename.
func (c *Core) AgentRename(ctx context.Context, pin Pin, name string) error {
	if _, err := c.pinned(ctx, pin); err != nil {
		return err
	}
	return c.call(ctx, nil, "label", string(pin.Pane), LabelWorker+"="+name)
}

// AgentList lists the live terminals that are agents. `hc ls` is a compact view:
// its status is a plain word and it has no app, labels or foreground, so it gives
// the addresses and `hc get` the facts. A terminal that ended between the two is
// skipped.
func (c *Core) AgentList(ctx context.Context) ([]Agent, error) {
	var r struct {
		Terminals []struct {
			Address string `json:"address"`
		} `json:"terminals"`
	}
	if err := c.call(ctx, &r, "ls"); err != nil {
		return nil, err
	}
	agents := make([]Agent, 0, len(r.Terminals))
	for _, listed := range r.Terminals {
		t, err := c.get(ctx, listed.Address)
		if IsAbsent(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		if t.isAgent() {
			agents = append(agents, t.agent())
		}
	}
	return agents, nil
}

// CallerWorkspace returns the caller's app space, or "" under bare hc. Unlike
// the TUI, an empty answer is not an error: the terminal is then a plain child.
func (c *Core) CallerWorkspace(ctx context.Context) (string, error) {
	if !c.Env.InHerdr {
		return "", ErrNotInHerdr
	}
	if c.Env.CallerPane == "" {
		return "", ErrNoCallerPane
	}
	t, err := c.get(ctx, string(c.Env.CallerPane))
	if err != nil {
		return "", err
	}
	return t.Labels[labelSpace], nil
}
