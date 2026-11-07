package bgproc

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/daemon"
	"github.com/shakestzd/wipnote/observe/otel/collector"
)

// candidates lists every pid a wipnote lease or PID file names for root,
// one entry per pid (the first file to name it wins).
func candidates(root string) []Proc {
	var out []Proc
	seen := map[int]bool{}
	add := func(p Proc) {
		if p.PID > 0 && !seen[p.PID] {
			seen[p.PID] = true
			out = append(out, p)
		}
	}
	if rec, ok := readWriterLease(root); ok {
		add(Proc{Kind: KindWriter, PID: rec.PID, Source: daemon.LeasePath(root)})
	}
	if rec, err := collector.ReadSharedRecord(root); err == nil {
		add(Proc{Kind: KindSharedCollector, PID: rec.PID, Port: rec.Port, Source: collector.SharedLeasePath(root)})
	}
	for _, p := range sessionCollectors(root) {
		add(p)
	}
	return out
}

// readWriterLease reads the writer lease record whether or not its heartbeat
// is fresh: a stale heartbeat on a live process is itself a symptom.
func readWriterLease(root string) (daemon.LeaseRecord, bool) {
	data, err := os.ReadFile(daemon.LeasePath(root))
	if err != nil {
		return daemon.LeaseRecord{}, false
	}
	var rec daemon.LeaseRecord
	if json.Unmarshal(data, &rec) != nil || rec.PID <= 0 {
		return daemon.LeaseRecord{}, false
	}
	return rec, true
}

// sessionCollectors reads every session's .collector-pid.
func sessionCollectors(root string) []Proc {
	files, _ := filepath.Glob(filepath.Join(root, ".wipnote", "sessions", "*", ".collector-pid"))
	sort.Strings(files)
	var out []Proc
	for _, f := range files {
		pid, _, _, err := collector.ReadCollectorPIDFile(f)
		if err != nil {
			continue
		}
		out = append(out, Proc{
			Kind: KindSessionCollector, PID: pid, Port: collector.ReadCollectorPort(f),
			Session: filepath.Base(filepath.Dir(f)), Source: f,
		})
	}
	return out
}

// probeWriter pings the writer's read socket. Three tries spread over the
// probe timeout keep one slow answer on a loaded host from reading as hung.
func probeWriter(ctx context.Context, root string, p *Proc, opts Options) {
	client := daemon.NewReadClient(root)
	var err error
	for i := 0; i < 3; i++ {
		pctx, cancel := context.WithTimeout(ctx, opts.ProbeTimeout/3)
		_, err = client.Ping(pctx)
		cancel()
		if err == nil || errors.Is(err, daemon.ErrReadUnsupported) {
			p.State, p.Reason = StateHealthy, "read socket answers"
			return
		}
	}
	if state := daemon.InspectLease(root); state.Status == daemon.LeaseFree {
		p.State, p.Reason = StateOrphaned, "lease heartbeat is stale and the read socket does not answer"
		return
	}
	p.State, p.Reason = StateHung, "holds the lease but the read socket does not answer: "+err.Error()
}

// probeCollector asks a collector's /healthz, and decides orphaning from the
// lease (shared) or the parent pid (per-session).
func probeCollector(root string, p *Proc, opts Options) {
	if p.Kind == KindSharedCollector {
		if rec, err := collector.ReadSharedRecord(root); err == nil && rec.PID != p.PID && collector.PIDAlive(rec.PID) {
			p.State, p.Reason = StateOrphaned, "superseded: the lease names another shared collector"
			return
		}
	} else if p.PPID == 1 {
		p.State, p.Reason = StateOrphaned, "its launcher exited"
		return
	}
	if p.Port == 0 {
		p.State, p.Reason = StateUnknown, "no recorded port to probe"
		return
	}
	h, err := collector.ProbeHealth(p.Port, opts.ProbeTimeout)
	switch {
	case err == nil && h.PID == p.PID:
		p.State, p.Reason = StateHealthy, "health endpoint answers"
	case err == nil:
		p.State, p.Reason = StateHung, "its port answers for another pid"
	case strings.Contains(err.Error(), "HTTP 404"):
		p.State, p.Reason = StateHealthy, "answers HTTP (older collector without /healthz)"
	default:
		p.State, p.Reason = StateHung, "health probe failed: "+err.Error()
	}
}
