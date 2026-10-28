package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"time"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/gitwatch"
	"github.com/shakestzd/wipnote/internal/procsweep"
	"github.com/shakestzd/wipnote/internal/registry"
	"github.com/shakestzd/wipnote/internal/statusall"
)

// Per-call bounds for `status --all`; the whole run is bounded by --timeout.
const (
	statusAllGHTimeout    = 8 * time.Second
	statusAllHerdrTimeout = 3 * time.Second
)

// statusAllGHRunner is the gh runner for one project folder. Tests swap it.
var statusAllGHRunner = func(dir string) gitwatch.Runner {
	return gitwatch.ExecRunner{Timeout: statusAllGHTimeout, Dir: dir}
}

// statusAllProcLister lists processes for the orphan sweep. Tests swap it.
var statusAllProcLister = procsweep.DefaultLister()

type statusAllFlags struct {
	all, json, noGH, noProcs bool
	timeout         time.Duration
	runs            int
}

func addStatusAllFlags(cmd *cobra.Command, f *statusAllFlags) {
	cmd.Flags().BoolVar(&f.all, "all", false, "Show every registered project (read-only; see docs/design/status-all.md)")
	cmd.Flags().BoolVar(&f.json, "json", false, "With --all: print the JSON document")
	cmd.Flags().BoolVar(&f.noGH, "no-gh", false, "With --all: skip every gh call (no PR or CI)")
	cmd.Flags().BoolVar(&f.noProcs, "no-procs", false, "With --all: skip the process sweep (no orphans)")
	cmd.Flags().DurationVar(&f.timeout, "timeout", statusall.DefaultDeadline, "With --all: bound for the whole run")
	cmd.Flags().IntVar(&f.runs, "runs", statusall.DefaultRunsLimit, "With --all: recent runs kept per project")
}

// isStatusAll reports whether cmd is `status --all`, which must skip the
// prerun's session and registry writes: it is read-only.
func isStatusAll(cmd *cobra.Command) bool {
	if cmd.Name() != "status" {
		return false
	}
	all, err := cmd.Flags().GetBool("all")
	return err == nil && all
}

// runStatusAll reads every registered project and prints the report. It
// writes nothing: the registry is loaded, never saved.
func runStatusAll(ctx context.Context, w io.Writer, f statusAllFlags) error {
	if ctx == nil {
		ctx = context.Background()
	}
	reg, err := registry.Load(defaultRegistryPath())
	if err != nil {
		return fmt.Errorf("load registry: %w", err)
	}
	o := statusall.Options{Entries: reg.List(), Deadline: f.timeout, RunsLimit: f.runs}
	o.GH, o.GHReason = statusAllGH(f.noGH)
	o.Herdr, o.HerdrReason = statusAllHerdr(os.Getenv)
	if !f.noProcs {
		o.Procs = statusAllProcLister
	}
	r := statusall.Collect(ctx, o)
	if f.json {
		return statusall.WriteJSON(w, r)
	}
	statusall.WriteText(w, r, time.Now())
	return nil
}

// statusAllGH returns the gh runner factory, or nil and why gh is not used.
func statusAllGH(noGH bool) (func(string) gitwatch.Runner, string) {
	if noGH {
		return nil, statusall.SourceSkipped
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, statusall.SourceNotAvailable + ": gh not found on PATH"
	}
	return statusAllGHRunner, ""
}

// statusAllHerdr returns the herdr client, or nil and why herdr is not used.
// herdr is only reachable from inside a herdr pane.
func statusAllHerdr(getenv func(string) string) (statusall.HerdrSource, string) {
	env := herdr.EnvFrom(getenv)
	if !env.InHerdr {
		return nil, statusall.SourceNotAvailable + ": not inside a herdr pane"
	}
	c := herdr.New(env)
	c.Timeout = statusAllHerdrTimeout
	return c, ""
}
