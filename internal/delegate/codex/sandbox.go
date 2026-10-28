// Package codex holds what wipnote knows about running Codex CLI as a
// delegated worker (plan-28b4a7f6 slice 12): the sandbox ladder and its
// bwrap preflight, shared with the `wipnote codex` launcher, and the
// thread id and usage a `codex exec --json` log carries.
//
// # The ladder (decision S)
//
// A worker is read-only by default; --write makes it workspace-write;
// danger-full-access is passed only for a host the user config lists in
// delegate.codex.dangerFullAccessHosts, with a warning. A failed bwrap probe
// refuses the run with the probe's output unless the host opted in. The
// final argv always carries exactly one --sandbox, so the launcher's
// devcontainer fallback (which skips when --sandbox is given) never widens a
// worker. Nothing here falls back to danger-full-access silently.
package codex

import (
	"errors"
	"fmt"
	"slices"
	"strings"
)

// Mode is a Codex --sandbox value.
type Mode string

const (
	ReadOnly         Mode = "read-only"
	WorkspaceWrite   Mode = "workspace-write"
	DangerFullAccess Mode = "danger-full-access"
)

// OptInKey is the user-config key under delegate.codex that lists hosts
// allowed danger-full-access.
const OptInKey = "dangerFullAccessHosts"

// ErrRefused marks a sandbox refusal: the run must not start.
var ErrRefused = errors.New("codex sandbox refused")

// bypassFlag is Codex's flag that turns off the sandbox and approvals.
const bypassFlag = "--dangerously-bypass-approvals-and-sandbox"

// ParseMode reads a --sandbox value.
func ParseMode(s string) (Mode, bool) {
	m := Mode(strings.TrimSpace(s))
	switch m {
	case ReadOnly, WorkspaceWrite, DangerFullAccess:
		return m, true
	}
	return "", false
}

// Request is what a delegate run or spawn asks of the ladder.
type Request struct {
	Write bool     // --write
	Args  []string // the codex words after the subcommand, as the caller gave them
	Host  string   // this host's name
	OptIn []string // delegate.codex.dangerFullAccessHosts from the user config
	Probe ProbeResult
}

// Decision is the sandbox the worker gets and, when it is widened, the
// warning to print.
type Decision struct {
	Mode    Mode
	Warning string
}

// Decide applies the ladder to r.
func Decide(r Request) (Decision, error) {
	want, err := wanted(r)
	if err != nil {
		return Decision{}, err
	}
	opted := OptedIn(r.Host, r.OptIn)
	if want == DangerFullAccess {
		if !opted {
			return Decision{}, fmt.Errorf("%w: danger-full-access was asked for, but host %q is not in delegate.codex.%s in ~/.wipnote/config.json", ErrRefused, r.Host, OptInKey)
		}
		return Decision{Mode: DangerFullAccess, Warning: dangerWarning(r.Host, "it was asked for")}, nil
	}
	if r.Probe.Status != ProbeFail {
		return Decision{Mode: want}, nil
	}
	if !opted {
		return Decision{}, probeRefusal(r)
	}
	return Decision{Mode: DangerFullAccess, Warning: dangerWarning(r.Host, "the bwrap probe failed:\n"+strings.TrimSpace(r.Probe.Output))}, nil
}

// wanted is the rung the caller asked for: the tool's own sandbox words,
// else workspace-write for --write, else read-only.
func wanted(r Request) (Mode, error) {
	own, err := askedMode(r.Args)
	if err != nil {
		return "", err
	}
	if r.Write && own == ReadOnly {
		return "", fmt.Errorf("%w: --write conflicts with the tool's own --sandbox read-only", ErrRefused)
	}
	switch {
	case own != "":
		return own, nil
	case r.Write:
		return WorkspaceWrite, nil
	}
	return ReadOnly, nil
}

// askedMode reads the sandbox the codex words ask for; "" when none.
func askedMode(args []string) (Mode, error) {
	words := ownWords(args)
	if BypassesSandbox(words) {
		return DangerFullAccess, nil
	}
	if v, ok := sandboxValue(words); ok {
		m, valid := ParseMode(v)
		if !valid {
			return "", fmt.Errorf("%w: unknown --sandbox %q (read-only, workspace-write or danger-full-access)", ErrRefused, v)
		}
		return m, nil
	}
	if slices.Contains(words, "--full-auto") {
		return WorkspaceWrite, nil
	}
	return "", nil
}

func dangerWarning(host, why string) string {
	return fmt.Sprintf("wipnote: warning: Codex runs with --sandbox danger-full-access (no sandbox) because host %q opted in through delegate.codex.%s and %s", host, OptInKey, why)
}

func probeRefusal(r Request) error {
	return fmt.Errorf(`%w: Codex's bwrap sandbox does not work on host %q, so the worker could not run tools. The probe said:
%s
Fix one of: install bubblewrap; let the container create user namespaces (seccomp/AppArmor); or, when the container itself is the isolation boundary, add %q to delegate.codex.%s in ~/.wipnote/config.json. Run "wipnote delegate doctor" to check.`,
		ErrRefused, r.Host, strings.TrimSpace(r.Probe.Output), r.Host, OptInKey)
}

// OptedIn reports whether host is listed (trimmed, any case). An empty host
// never matches.
func OptedIn(host string, hosts []string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	return slices.ContainsFunc(hosts, func(h string) bool { return strings.EqualFold(strings.TrimSpace(h), host) })
}
