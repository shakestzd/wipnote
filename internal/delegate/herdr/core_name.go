package herdr

import (
	"context"
	"errors"
	"os/exec"
	"regexp"
	"time"
)

// Names for what the Herdr app shows. The app labels an unnamed tab with its
// position ("2") and an unnamed terminal with nothing, so delegate names both.

type termNameKey struct{}

// WithTerminalName asks Core to start the next terminal it makes under name
// (hc run --name). The name rides the context so Client keeps its shape: the
// older CLI client and the fakes ignore it. An invalid name is ignored later.
func WithTerminalName(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, termNameKey{}, name)
}

type workerLabelKey struct{}

// WithWorkerLabel asks Core to start the next terminal labelled as this worker,
// so its pane header can name it from the start, before any agent is there to
// rename. Like the terminal name, it rides the context and others ignore it.
func WithWorkerLabel(ctx context.Context, name string) context.Context {
	return context.WithValue(ctx, workerLabelKey{}, name)
}

// withWorkerLabel adds the label ctx carries to a run's arguments, ahead of any
// command, and leaves the given slice alone.
func withWorkerLabel(ctx context.Context, args []string) []string {
	name, _ := ctx.Value(workerLabelKey{}).(string)
	if name == "" || len(args) == 0 {
		return args
	}
	out := append([]string{args[0], "--label", LabelWorker + "=" + name}, args[1:]...)
	return out
}

type workersKeyKey struct{}

// WithWorkersKey names whose workers tab the next TabCreate belongs to, so each
// project gets its own tab even when one caller spawns in two projects. It rides
// the context as the terminal name does. Without one the tab is the caller's.
func WithWorkersKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, workersKeyKey{}, key)
}

// workersTabKey returns the key carried by ctx, or the caller pane.
func (c *Core) workersTabKey(ctx context.Context) string {
	if k, _ := ctx.Value(workersKeyKey{}).(string); k != "" {
		return k
	}
	return string(c.Env.CallerPane)
}

var hcName = regexp.MustCompile(`^[a-z0-9_-]{1,32}$`)

// terminalName returns the valid name carried by ctx, or "".
func terminalName(ctx context.Context) string {
	n, _ := ctx.Value(termNameKey{}).(string)
	if !hcName.MatchString(n) {
		return ""
	}
	return n
}

// runNamed starts a terminal with --name when ctx carries one. hc refuses a
// name a live terminal holds; that must not fail the spawn, so a refusal by hc
// (not a timeout or cancel) starts the terminal unnamed instead.
func (c *Core) runNamed(ctx context.Context, w *wireTerminal, args []string) error {
	args = withWorkerLabel(ctx, args)
	name := terminalName(ctx)
	if name == "" {
		return c.call(ctx, w, args...)
	}
	err := c.call(ctx, w, append(append([]string{}, args...), "--name", name)...)
	var he *Error
	if err != nil && errors.As(err, &he) {
		return c.call(ctx, w, args...)
	}
	return err
}

// Tab rename retry: the app learns of a new tab asynchronously, so the first
// tries can find no such tab. The total stays small so spawn returns promptly.
var (
	tabRenameEvery = 250 * time.Millisecond
	tabRenameTries = 12 // about 3s
	tabRenameCall  = 2 * time.Second
)

// nameTab names the tab best effort with the app's tab.rename. It does nothing
// without herdr-app on PATH and never reports a failure.
func nameTab(ctx context.Context, tab, name string) {
	path, err := exec.LookPath("herdr-app")
	if err != nil || tab == "" || name == "" {
		return
	}
	appDo(ctx, path, "tab.rename", "--id", tab, "--name", name)
}

// appDo runs `herdr-app do args...`, retrying while the app has not learned of
// the tab yet. It reports whether a try succeeded.
func appDo(ctx context.Context, path string, args ...string) bool {
	for i := 0; i < tabRenameTries; i++ {
		cctx, cancel := context.WithTimeout(ctx, tabRenameCall)
		err := exec.CommandContext(cctx, path, append([]string{"do"}, args...)...).Run()
		cancel()
		if err == nil {
			return true
		}
		if ctx.Err() != nil || i == tabRenameTries-1 {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(tabRenameEvery):
		}
	}
	return false
}
