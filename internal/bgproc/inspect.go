// Package bgproc finds this user's wipnote background processes for one
// project — the headless writer daemon and the otel-collect collectors —
// probes each, and stops the hung or orphaned ones (`wipnote doctor
// --processes`, `--restart-all`; issue #183).
//
// A process is a candidate ONLY when a wipnote lease or PID file names it:
// the writer lease (.wipnote/writer.lease.pid), the shared collector lease
// (.wipnote/otel-collector.lease.pid) or a session's .collector-pid. It is
// touched only when it is also in the current user's process table AND its
// argv is a wipnote background command for this project. Nothing else — a
// `wipnote claude` launcher, Herdr, an unrelated process that reused a pid —
// can pass both checks.
package bgproc

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/procsweep"
)

// Kind is the sort of background process.
type Kind string

const (
	KindWriter           Kind = "writer"
	KindSharedCollector  Kind = "otel-shared"
	KindSessionCollector Kind = "otel-session"
)

// State is what the probe concluded.
type State string

const (
	StateHealthy    State = "healthy"
	StateHung       State = "hung"
	StateOrphaned   State = "orphaned"
	StateUnknown    State = "unknown"    // ours, but there is nothing to probe
	StateUnverified State = "unverified" // a file names it, argv does not match: never touched
	StateGone       State = "gone"       // a file names it, no such process of ours
)

// Proc is one background process candidate and its verdict.
type Proc struct {
	Kind    Kind   `json:"kind"`
	PID     int    `json:"pid"`
	PPID    int    `json:"ppid,omitempty"`
	Port    int    `json:"port,omitempty"`
	Session string `json:"session,omitempty"`
	Source  string `json:"source"` // the lease or PID file that named it
	Command string `json:"command,omitempty"`
	State   State  `json:"state"`
	Reason  string `json:"reason,omitempty"`
}

// Stoppable reports whether Restart may stop p.
func (p Proc) Stoppable() bool { return p.State == StateHung || p.State == StateOrphaned }

// Inspect lists and probes the project's wipnote background processes. It
// never sends a signal.
func Inspect(ctx context.Context, root string, opts Options) ([]Proc, error) {
	opts = opts.withDefaults()
	table, err := userTable(ctx, opts.Lister)
	if err != nil {
		return nil, err
	}
	procs := candidates(root)
	for i := range procs {
		classify(ctx, root, &procs[i], table, opts)
	}
	return procs, nil
}

// userTable lists the current user's processes keyed by pid.
func userTable(ctx context.Context, l procsweep.Lister) (map[int]procsweep.RawProc, error) {
	raws, err := l.List(ctx)
	if err != nil {
		return nil, err
	}
	table := make(map[int]procsweep.RawProc, len(raws))
	for _, r := range raws {
		table[r.PID] = r
	}
	return table, nil
}

// classify fills p's verdict from the process table, its argv and a probe.
func classify(ctx context.Context, root string, p *Proc, table map[int]procsweep.RawProc, opts Options) {
	raw, ok := table[p.PID]
	if !ok || p.PID == os.Getpid() {
		p.State, p.Reason = StateGone, "no such process of this user; the file is stale"
		return
	}
	p.PPID, p.Command = raw.PPID, raw.Command
	if !argvMatches(p, root, opts.ExeNames) {
		p.State, p.Reason = StateUnverified, "argv is not a wipnote "+string(p.Kind)+" for this project; not touched"
		return
	}
	if p.Port == 0 {
		p.Port = listenPort(raw.Command)
	}
	switch p.Kind {
	case KindWriter:
		probeWriter(ctx, root, p, opts)
	default:
		probeCollector(root, p, opts)
	}
}

// argvMatches reports whether cmd is a wipnote background command of p's
// kind for root, started from an executable named in exeNames.
func argvMatches(p *Proc, root string, exeNames []string) bool {
	fields := strings.Fields(p.Command)
	if len(fields) == 0 || !nameAllowed(filepath.Base(fields[0]), exeNames) || !hasProjectDir(p.Command, root) {
		return false
	}
	padded := " " + p.Command + " "
	switch p.Kind {
	case KindWriter:
		return strings.Contains(padded, " _serve-child ")
	case KindSharedCollector:
		return strings.Contains(padded, " otel-collect ") && strings.Contains(padded, " --shared ")
	case KindSessionCollector:
		// The pid file of a session on the shared collector names the shared
		// process; that argv is accepted here and reported as shared.
		if strings.Contains(padded, " otel-collect ") && strings.Contains(padded, " --shared ") {
			p.Kind = KindSharedCollector
			return true
		}
		return strings.Contains(padded, " otel-collect ") && strings.Contains(padded, " --session-id "+p.Session+" ")
	}
	return false
}

func nameAllowed(base string, names []string) bool {
	for _, n := range names {
		if base == n {
			return true
		}
	}
	return false
}

// hasProjectDir reports whether cmd carries --project-dir root (or root with
// symlinks resolved).
func hasProjectDir(cmd, root string) bool {
	padded := " " + cmd + " "
	roots := []string{root}
	if resolved, err := filepath.EvalSymlinks(root); err == nil && resolved != root {
		roots = append(roots, resolved)
	}
	for _, r := range roots {
		if strings.Contains(padded, " --project-dir "+r+" ") {
			return true
		}
	}
	return false
}

// listenPort reads a fixed port from "--listen 127.0.0.1:<port>"; 0 when the
// collector asked for an ephemeral port or has no such flag.
func listenPort(cmd string) int {
	fields := strings.Fields(cmd)
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "--listen" {
			if idx := strings.LastIndexByte(fields[i+1], ':'); idx >= 0 {
				n, _ := strconv.Atoi(fields[i+1][idx+1:])
				return n
			}
		}
	}
	return 0
}

// Options configures Inspect and Restart. The zero value is production.
type Options struct {
	Lister        procsweep.Lister
	ExeNames      []string // accepted executable basenames
	ProbeTimeout  time.Duration
	Grace         time.Duration // SIGTERM to SIGKILL
	RestartWriter func(ctx context.Context, root string) error
}

func (o Options) withDefaults() Options {
	if o.Lister == nil {
		o.Lister = procsweep.DefaultLister()
	}
	if len(o.ExeNames) == 0 {
		o.ExeNames = []string{"wipnote"}
		if exe, err := os.Executable(); err == nil {
			o.ExeNames = append(o.ExeNames, filepath.Base(exe))
		}
	}
	if o.ProbeTimeout <= 0 {
		o.ProbeTimeout = 2 * time.Second
	}
	if o.Grace <= 0 {
		o.Grace = 5 * time.Second
	}
	return o
}
