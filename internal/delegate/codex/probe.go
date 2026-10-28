package codex

import (
	"context"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// ProbeTimeout bounds one bwrap probe: a hung probe must never hold up a
// launch or a run.
const ProbeTimeout = 5 * time.Second

// ProbeStatus is the outcome of a bwrap probe.
type ProbeStatus string

const (
	ProbePass    ProbeStatus = "pass"    // bwrap made its namespaces
	ProbeFail    ProbeStatus = "fail"    // a definite bwrap/namespace failure
	ProbeSkip    ProbeStatus = "skip"    // not Linux: Codex does not use bwrap here
	ProbeUnknown ProbeStatus = "unknown" // timed out or failed for another reason
)

// ProbeResult is one probe, with bwrap's exact output.
type ProbeResult struct {
	Status    ProbeStatus `json:"status"`
	Output    string      `json:"output"`
	Host      string      `json:"host,omitempty"`
	CheckedAt time.Time   `json:"checked_at"`
}

// ProbeBwrap exercises the user-namespace path Codex's bubblewrap sandbox
// needs. Codex runs bwrap with --unshare-user (and --unshare-pid,
// --unshare-net) plus --ro-bind / /. In Codespaces and devcontainers the
// kernel capabilities for user namespaces are often absent, so this fails
// with "operation not permitted" or similar. bwrap is probed directly (not
// codex --help, which never runs bwrap) so the real failure path fires.
//
// bwrap missing from PATH is reported as ("bwrap not found on PATH", err),
// which callers treat as unavailable. The probe is bounded by ProbeTimeout.
func ProbeBwrap(ctx context.Context) ([]byte, error) {
	bwrapPath, err := exec.LookPath("bwrap")
	if err != nil {
		return []byte("bwrap not found on PATH"), err
	}
	ctx, cancel := context.WithTimeout(ctx, ProbeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, bwrapPath, "--unshare-user", "--ro-bind", "/", "/", "true")
	cmd.WaitDelay = time.Second // a stub's children cannot hold the pipes open
	out, err := cmd.CombinedOutput()
	if ctx.Err() != nil {
		return out, ctx.Err()
	}
	return out, err
}

// Probe runs ProbeBwrap and classifies it. goos is runtime.GOOS in
// production; off Linux (macOS uses Seatbelt) nothing is probed.
func Probe(ctx context.Context, goos string) ProbeResult {
	if goos == "" {
		goos = runtime.GOOS
	}
	if goos != "linux" {
		return ProbeResult{Status: ProbeSkip, Output: "not Linux (" + goos + "): Codex does not sandbox with bwrap here", CheckedAt: time.Now().UTC()}
	}
	out, err := ProbeBwrap(ctx)
	r := ProbeResult{Output: strings.TrimSpace(string(out)), CheckedAt: time.Now().UTC()}
	switch {
	case errors.Is(err, exec.ErrNotFound):
		// Codex falls back to its bundled bubblewrap when no system bwrap
		// exists, so a missing one settles nothing.
		r.Status, r.Output = ProbeUnknown, r.Output+"; Codex may use its bundled bubblewrap"
	case err == nil:
		r.Status = ProbePass
	case errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled):
		r.Status, r.Output = ProbeUnknown, strings.TrimSpace(r.Output+"\nbwrap probe did not finish: "+err.Error())
	case IsBubblewrapFailure(out, err):
		r.Status = ProbeFail
		if r.Output == "" {
			r.Output = err.Error()
		}
	default:
		r.Status, r.Output = ProbeUnknown, strings.TrimSpace(r.Output+"\n"+err.Error())
	}
	return r
}

// namespaceContextWords are terms that tie a failure to Linux namespace or
// bubblewrap setup rather than an unrelated EPERM.
var namespaceContextWords = []string{"bwrap", "bubblewrap", "namespace", "unshare"}

// IsBubblewrapFailure reports whether a probe's output and error describe
// a bwrap/namespace failure.
func IsBubblewrapFailure(out []byte, err error) bool {
	text := strings.ToLower(string(out))
	if err != nil {
		text += "\n" + strings.ToLower(err.Error())
	}
	// Direct bwrap/namespace error indicators: unambiguous on their own.
	for _, s := range []string{"bwrap", "bubblewrap", "failed to make / slave", "cannot create namespace"} {
		if strings.Contains(text, s) {
			return true
		}
	}
	// "operation not permitted" (EPERM) is too broad alone (file
	// permissions, capability drops); it counts only beside a namespace word.
	if strings.Contains(text, "operation not permitted") {
		for _, word := range namespaceContextWords {
			if strings.Contains(text, word) {
				return true
			}
		}
	}
	return false
}
