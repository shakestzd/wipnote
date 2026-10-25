package herdr

import (
	"context"
	"fmt"
	"strconv"
)

// agentGet runs `agent get <target>` (verified; agent commands take a pane id
// or a live agent name, never a terminal id).
func (c *CLI) agentGet(ctx context.Context, target string) (Agent, error) {
	var r struct {
		Agent *wireAgent `json:"agent"`
	}
	if err := c.call(ctx, &r, "agent", "get", target); err != nil {
		return Agent{}, err
	}
	if r.Agent == nil {
		return Agent{}, fmt.Errorf("%w: agent get gave no .result.agent", ErrBadReply)
	}
	return r.Agent.agent(), nil
}

// pinned looks the pinned pane's agent up and checks it is the one the pin
// names: same pane, and the same terminal when the pin recorded one. Any
// failure is ErrPin, and the caller sends nothing.
func (c *CLI) pinned(ctx context.Context, pin Pin) (Agent, error) {
	if pin.Pane == "" {
		return Agent{}, fmt.Errorf("%w: no pane pinned", ErrPin)
	}
	a, err := c.agentGet(ctx, string(pin.Pane))
	if err != nil {
		return Agent{}, fmt.Errorf("%w: herdr cannot say who is in pane %s; nothing was sent: %w", ErrPin, pin.Pane, err)
	}
	if a.Pane != pin.Pane {
		return Agent{}, fmt.Errorf("%w: agent is in pane %s, not %s; nothing was sent", ErrPin, a.Pane, pin.Pane)
	}
	if pin.TerminalID != "" && a.TerminalID != pin.TerminalID {
		return Agent{}, fmt.Errorf("%w: pane %s now holds terminal %s, not %s; nothing was sent",
			ErrPin, pin.Pane, a.TerminalID, pin.TerminalID)
	}
	return a, nil
}

// Prompt submits text and Enter to the pinned agent; success means
// submission, not that a turn started. Verified: `agent prompt <target> <text>`.
func (c *CLI) Prompt(ctx context.Context, pin Pin, text string) error {
	if _, err := c.pinned(ctx, pin); err != nil {
		return err
	}
	return c.call(ctx, nil, "agent", "prompt", string(pin.Pane), text)
}

// Read returns the pinned agent's recent screen. A hint only: delegate never
// decides a worker finished from screen text. Verified:
// `agent read <target> --source recent-unwrapped --lines N`, plain text.
func (c *CLI) Read(ctx context.Context, pin Pin, lines int) (string, error) {
	if _, err := c.pinned(ctx, pin); err != nil {
		return "", err
	}
	return c.readText(ctx, "agent", "read", string(pin.Pane), "--source", "recent-unwrapped", "--lines", strconv.Itoa(lines))
}

// State reports the pinned agent (verified: `agent get`).
func (c *CLI) State(ctx context.Context, pin Pin) (Agent, error) {
	return c.pinned(ctx, pin)
}

// AgentRename names the pinned agent. UNVERIFIED: `agent rename <target>
// <name>` is in herdr's help listing but was not run.
func (c *CLI) AgentRename(ctx context.Context, pin Pin, name string) error {
	if _, err := c.pinned(ctx, pin); err != nil {
		return err
	}
	return c.call(ctx, nil, "agent", "rename", string(pin.Pane), name)
}

// AgentList lists live agents with pane, terminal, name, kind and state.
// Verified: `agent list` -> result.agents[].
func (c *CLI) AgentList(ctx context.Context) ([]Agent, error) {
	var r struct {
		Agents []wireAgent `json:"agents"`
	}
	if err := c.call(ctx, &r, "agent", "list"); err != nil {
		return nil, err
	}
	agents := make([]Agent, 0, len(r.Agents))
	for _, a := range r.Agents {
		agents = append(agents, a.agent())
	}
	return agents, nil
}
