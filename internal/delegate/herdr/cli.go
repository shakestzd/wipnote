package herdr

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds every herdr call when CLI.Timeout is zero.
const DefaultTimeout = 15 * time.Second

// CLI is the Client that runs the herdr binary found on PATH.
type CLI struct {
	Env     Env
	Timeout time.Duration // per call; DefaultTimeout when zero
	Bin     string        // binary name; "herdr" when empty
}

var _ Client = (*CLI)(nil)

// New returns a CLI client for env (see EnvFrom).
func New(env Env) *CLI { return &CLI{Env: env} }

// run executes one herdr call under the call timeout and returns stdout. It
// refuses before any exec outside herdr, and maps failures onto the error
// types in errors.go: the deadline first, then the exit status.
func (c *CLI) run(ctx context.Context, args ...string) ([]byte, error) {
	if !c.Env.InHerdr {
		return nil, ErrNotInHerdr
	}
	bin := c.Bin
	if bin == "" {
		bin = "herdr"
	}
	path, err := exec.LookPath(bin)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrAbsent, err)
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
		return nil, fmt.Errorf("%w: herdr %s after %s", ErrTimeout, strings.Join(args, " "), timeout)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return nil, classify(args, exitErr.ExitCode(), stderr.Bytes())
	}
	if err != nil {
		return nil, err
	}
	return stdout.Bytes(), nil
}

// call runs herdr and decodes the {"id":..,"result":{...}} envelope into
// result. A nil result accepts any output: send-text, send-keys and run print
// nothing on success.
func (c *CLI) call(ctx context.Context, result any, args ...string) error {
	out, err := c.run(ctx, args...)
	if err != nil || result == nil {
		return err
	}
	env := struct {
		Result json.RawMessage `json:"result"`
	}{}
	if json.Unmarshal(out, &env) != nil || len(env.Result) == 0 {
		return fmt.Errorf("%w: herdr %s: %.120q", ErrBadReply, strings.Join(args, " "), out)
	}
	if err := json.Unmarshal(env.Result, result); err != nil {
		return fmt.Errorf("%w: herdr %s: %v", ErrBadReply, strings.Join(args, " "), err)
	}
	return nil
}

// readText returns screen text. herdr prints `agent read` as plain text; a
// JSON envelope with .result.read.text is also accepted. An empty read or an
// envelope without text is a bad reply.
func (c *CLI) readText(ctx context.Context, args ...string) (string, error) {
	out, err := c.run(ctx, args...)
	if err != nil {
		return "", err
	}
	if trimmed := bytes.TrimSpace(out); bytes.HasPrefix(trimmed, []byte("{")) {
		var env struct {
			Result *struct {
				Read *struct {
					Text *string `json:"text"`
				} `json:"read"`
			} `json:"result"`
		}
		if json.Unmarshal(trimmed, &env) == nil && env.Result != nil {
			if env.Result.Read == nil || env.Result.Read.Text == nil {
				return "", fmt.Errorf("%w: read envelope without .result.read.text", ErrBadReply)
			}
			return *env.Result.Read.Text, nil
		}
	}
	if len(bytes.TrimSpace(out)) == 0 {
		return "", fmt.Errorf("%w: empty read output", ErrBadReply)
	}
	return strings.TrimSuffix(string(out), "\n"), nil
}

// CallerWorkspace returns the workspace of the caller's pane (HERDR_PANE_ID,
// trusted only inside herdr). Without it the caller is unknown and new tabs
// must not be guessed into another workspace.
func (c *CLI) CallerWorkspace(ctx context.Context) (string, error) {
	if !c.Env.InHerdr {
		return "", ErrNotInHerdr
	}
	if c.Env.CallerPane == "" {
		return "", ErrNoCallerPane
	}
	p, err := c.Get(ctx, c.Env.CallerPane)
	if err != nil {
		return "", err
	}
	if p.WorkspaceID == "" {
		return "", fmt.Errorf("%w: pane get %s gave no workspace_id", ErrBadReply, c.Env.CallerPane)
	}
	return p.WorkspaceID, nil
}
