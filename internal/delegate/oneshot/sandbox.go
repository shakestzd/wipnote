package oneshot

import (
	"context"
	"fmt"
	"io"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/codex"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// SandboxOptions feed codex exec's sandbox ladder (plan-28b4a7f6 slice 12,
// decision S): read-only by default, workspace-write with Write, and
// danger-full-access only for a host listed in OptIn.
type SandboxOptions struct {
	Write bool     // --write
	Host  string   // this host's name, matched against OptIn
	OptIn []string // delegate.codex.dangerFullAccessHosts (user config only)
	// Preflight is the cached bwrap probe; nil runs none, so the run keeps
	// its rung and Codex reports a sandbox failure itself.
	Preflight func(context.Context) codex.ProbeResult
}

// sandboxFor applies the ladder to a codex exec argv and prints a widening
// warning. Other tools get no sandbox flag, and --write is refused for them.
func sandboxFor(ctx context.Context, o Options, r Recipe) (codex.Mode, error) {
	if r.Kind != KindCodex {
		if o.Sandbox.Write {
			return "", fmt.Errorf("delegate run: --write applies to codex exec only, not %s", r.Name())
		}
		return "", nil
	}
	probe := codex.ProbeResult{Status: codex.ProbeUnknown}
	if o.Sandbox.Preflight != nil {
		probe = o.Sandbox.Preflight(ctx)
	}
	d, err := codex.Decide(codex.Request{Write: o.Sandbox.Write, Args: r.Argv[2:], Host: o.Sandbox.Host,
		OptIn: o.Sandbox.OptIn, Probe: probe})
	if err != nil {
		return "", fmt.Errorf("delegate run: %w", err)
	}
	if d.Warning != "" && o.Stderr != nil {
		io.WriteString(o.Stderr, d.Warning+"\n")
	}
	return d.Mode, nil
}

// enrichRow records what the run folder holds on the run's row: from a
// codex exec --json log, Codex's thread id (the worker session) and the four
// usage fields; otherwise the usage line's counts.
func enrichRow(l *delegationledger.Store, rf *runstore.RunFolder, runID string) {
	raw, _, _ := rf.ReadFrom(runstore.FileRawLog, 0)
	x := codex.ParseExecJSONL(raw)
	var e delegationledger.Enrichment
	if x.ThreadID != "" {
		e.WorkerSessions = []string{x.ThreadID}
	}
	if u := x.Usage; x.HasUsage {
		e.UsageInput, e.UsageCached, e.UsageOutput, e.UsageReasoning = &u.Input, &u.Cached, &u.Output, &u.Reasoning
	} else if info, _ := rf.Read(); info.Usage != "" {
		if in, cached, out, ok := UsageCounts(info.Usage); ok {
			e.UsageInput, e.UsageCached, e.UsageOutput = &in, &cached, &out
		}
	}
	if e.WorkerSessions != nil || e.UsageInput != nil {
		_, _ = l.Enrich(runID, e)
	}
}
