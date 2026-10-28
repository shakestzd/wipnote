package codex

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// DefaultPreflightTTL is how long a probe result is trusted for one host.
const DefaultPreflightTTL = 10 * time.Minute

// Preflight is the cached bwrap probe before a Codex spawn or run: one
// probe per host per TTL, kept in the run store (Dir, gitignored). Only a
// definite result (pass, fail, skip) is cached, so a timeout probes again
// next time. A cache that cannot be read or written only costs a probe.
type Preflight struct {
	Dir  string // the run store root; "" disables the cache
	Host string
	TTL  time.Duration    // DefaultPreflightTTL when zero
	GOOS string           // runtime.GOOS when empty
	Now  func() time.Time // time.Now when nil
}

var hostFileChars = regexp.MustCompile(`[^A-Za-z0-9._-]`)

func (p Preflight) now() time.Time {
	if p.Now != nil {
		return p.Now().UTC()
	}
	return time.Now().UTC()
}

func (p Preflight) ttl() time.Duration {
	if p.TTL > 0 {
		return p.TTL
	}
	return DefaultPreflightTTL
}

func (p Preflight) cachePath() string {
	if p.Dir == "" {
		return ""
	}
	return filepath.Join(p.Dir, "codex-sandbox-"+hostFileChars.ReplaceAllString(p.Host, "_")+".json")
}

// Result is the cached probe when it is fresh, else a new probe.
func (p Preflight) Result(ctx context.Context) ProbeResult {
	if r, ok := p.cached(); ok {
		return r
	}
	return p.Fresh(ctx)
}

// Fresh probes now and caches a definite result.
func (p Preflight) Fresh(ctx context.Context) ProbeResult {
	r := Probe(ctx, p.GOOS)
	r.Host, r.CheckedAt = p.Host, p.now()
	if path := p.cachePath(); path != "" && r.Status != ProbeUnknown {
		if data, err := json.Marshal(r); err == nil && os.MkdirAll(p.Dir, 0o700) == nil {
			_ = filelock.WriteFileAtomic(path, data, 0o600)
		}
	}
	return r
}

func (p Preflight) cached() (ProbeResult, bool) {
	path := p.cachePath()
	if path == "" {
		return ProbeResult{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ProbeResult{}, false
	}
	var r ProbeResult
	if json.Unmarshal(data, &r) != nil || r.Host != p.Host || r.Status == "" || r.Status == ProbeUnknown {
		return ProbeResult{}, false
	}
	if age := p.now().Sub(r.CheckedAt); age < 0 || age >= p.ttl() {
		return ProbeResult{}, false
	}
	return r, true
}
