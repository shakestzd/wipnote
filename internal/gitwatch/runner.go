// Package gitwatch reads the state of a branch's pull request (its checks,
// its basics and the work item it links to) by shelling out to the gh and
// git CLIs. It is read-only toward GitHub and never handles a token: gh owns
// authentication.
package gitwatch

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// DefaultTimeout bounds a single gh or git call.
const DefaultTimeout = 25 * time.Second

// ErrNotAvailable marks a condition where the data cannot be read at all:
// the executable is missing or gh is not signed in. Callers report it as a
// state, not a failure.
var ErrNotAvailable = errors.New("not available")

// Runner runs one external command and returns its output. A non-zero exit
// is reported through exitCode, not err: gh exits non-zero with valid JSON
// on stdout (8 pending, 1 failed). err is for "could not run it at all".
type Runner interface {
	Run(ctx context.Context, name string, args ...string) (stdout, stderr []byte, exitCode int, err error)
}

// ExecRunner is the os/exec Runner. A zero Timeout means DefaultTimeout.
type ExecRunner struct {
	Timeout time.Duration
}

// Run implements Runner.
func (r ExecRunner) Run(ctx context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	var stdout, stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	cmd.WaitDelay = 2 * time.Second
	err := cmd.Run()
	if ctx.Err() != nil {
		return stdout.Bytes(), stderr.Bytes(), -1, fmt.Errorf("%s: %w", name, ctx.Err())
	}
	var exitErr *exec.ExitError
	switch {
	case err == nil:
		return stdout.Bytes(), stderr.Bytes(), 0, nil
	case errors.As(err, &exitErr):
		return stdout.Bytes(), stderr.Bytes(), exitErr.ExitCode(), nil
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, os.ErrNotExist):
		return nil, nil, -1, fmt.Errorf("%s not found: %w", name, ErrNotAvailable)
	default:
		return nil, nil, -1, fmt.Errorf("run %s: %w", name, err)
	}
}

// FakeResult is one recorded outcome for FakeRunner.
type FakeResult struct {
	Stdout string
	Stderr string
	Exit   int
	Err    error
}

// FakeRunner answers from recorded results keyed by the space-joined argv
// ("gh pr view --json number"). An argv with no result is an error, so a
// test never reaches a real command. Calls records every argv in order.
type FakeRunner struct {
	Results map[string]FakeResult
	Calls   []string
}

// Run implements Runner.
func (f *FakeRunner) Run(_ context.Context, name string, args ...string) ([]byte, []byte, int, error) {
	key := strings.Join(append([]string{name}, args...), " ")
	f.Calls = append(f.Calls, key)
	res, ok := f.Results[key]
	if !ok {
		return nil, nil, -1, fmt.Errorf("fake runner: no result for %q", key)
	}
	return []byte(res.Stdout), []byte(res.Stderr), res.Exit, res.Err
}
