package codex

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"
)

// Check statuses, each four letters so the report lines up.
const (
	CheckPass = "pass"
	CheckWarn = "warn"
	CheckFail = "fail"
	CheckSkip = "skip"
)

// Check is one line of `wipnote delegate doctor`: a name, a status and the
// detail (for the probe, its exact output).
type Check struct {
	Name, Status, Detail string
}

// DoctorOptions are the facts the Codex checks read.
type DoctorOptions struct {
	Host       string
	OptIn      []string
	OptInNotes []string // ignored opt-in keys in files other than the user config
	Probe      ProbeResult
}

// Doctor runs the Codex checks: the binary, the bwrap probe, the opt-in
// and the ladder a worker on this host would get.
func Doctor(ctx context.Context, o DoctorOptions) []Check {
	return []Check{codexCheck(ctx), bwrapCheck(o), optInCheck(o), ladderCheck(o)}
}

func codexCheck(ctx context.Context) Check {
	c := Check{Name: "codex"}
	path, err := exec.LookPath("codex")
	if err != nil {
		c.Status, c.Detail = CheckFail, "codex is not on PATH: "+err.Error()
		return c
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "--version")
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		c.Status, c.Detail = CheckWarn, path+": codex --version failed: "+err.Error()
		return c
	}
	c.Status, c.Detail = CheckPass, path+": "+strings.TrimSpace(string(out))
	return c
}

func bwrapCheck(o DoctorOptions) Check {
	c := Check{Name: "bwrap sandbox", Detail: o.Probe.Output}
	switch o.Probe.Status {
	case ProbePass:
		c.Status = CheckPass
		if c.Detail == "" {
			c.Detail = "bwrap created its namespaces"
		}
	case ProbeSkip:
		c.Status = CheckSkip
	case ProbeFail:
		c.Status = CheckFail
		if OptedIn(o.Host, o.OptIn) {
			c.Status = CheckWarn
			c.Detail += fmt.Sprintf("\nhost %q opted in: workers run with --sandbox danger-full-access", o.Host)
		}
	default:
		c.Status = CheckWarn
		c.Detail += "\nthe probe was inconclusive; workers keep their sandbox and Codex reports any failure"
	}
	return c
}

func optInCheck(o DoctorOptions) Check {
	c := Check{Name: "danger-full-access opt-in", Status: CheckPass}
	listed := "is not listed"
	if OptedIn(o.Host, o.OptIn) {
		listed = "is listed"
	}
	c.Detail = fmt.Sprintf("host %q %s in delegate.codex.%s (~/.wipnote/config.json)", o.Host, listed, OptInKey)
	if len(o.OptInNotes) > 0 {
		c.Status = CheckWarn
		c.Detail += "\n" + strings.Join(o.OptInNotes, "\n")
	}
	return c
}

func ladderCheck(o DoctorOptions) Check {
	c := Check{Name: "sandbox ladder", Status: CheckPass}
	var parts []string
	for _, rung := range []struct {
		label string
		write bool
	}{{"default", false}, {"--write", true}} {
		d, err := Decide(Request{Write: rung.write, Host: o.Host, OptIn: o.OptIn, Probe: o.Probe})
		if err != nil {
			c.Status = CheckFail
			parts = append(parts, rung.label+": refused")
			continue
		}
		if d.Mode == DangerFullAccess && c.Status == CheckPass {
			c.Status = CheckWarn
		}
		parts = append(parts, rung.label+": --sandbox "+string(d.Mode))
	}
	c.Detail = strings.Join(parts, "; ")
	return c
}

// WriteChecks prints one line per check (status, name, first detail line)
// with further detail lines indented, and returns how many failed.
func WriteChecks(w io.Writer, checks []Check) int {
	failed := 0
	for _, c := range checks {
		if c.Status == CheckFail {
			failed++
		}
		lines := strings.Split(strings.TrimSpace(c.Detail), "\n")
		fmt.Fprintf(w, "%-4s  %s: %s\n", c.Status, c.Name, lines[0])
		for _, l := range lines[1:] {
			fmt.Fprintf(w, "        %s\n", l)
		}
	}
	return failed
}
