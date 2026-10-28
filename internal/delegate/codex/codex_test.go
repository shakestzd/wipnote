package codex

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const host = "devbox-1"

var probeFailed = ProbeResult{Status: ProbeFail, Output: "bwrap: No permissions to create a new namespace"}

func decide(t *testing.T, r Request) Decision {
	t.Helper()
	d, err := Decide(r)
	if err != nil {
		t.Fatalf("Decide(%+v): %v", r, err)
	}
	return d
}

// Ladder rung 1: with no flag a worker is read-only, opted host or not,
// and no warning is printed.
func TestSandbox_DefaultReadOnly(t *testing.T) {
	for _, opt := range [][]string{nil, {host}} {
		d := decide(t, Request{Host: host, OptIn: opt, Probe: ProbeResult{Status: ProbePass}})
		if d.Mode != ReadOnly || d.Warning != "" {
			t.Fatalf("opt-in %v: %+v, want read-only with no warning", opt, d)
		}
	}
	// the other side: --write leaves read-only
	if d := decide(t, Request{Write: true, Host: host}); d.Mode == ReadOnly {
		t.Fatalf("--write stayed read-only: %+v", d)
	}
}

// Ladder rung 2: --write (or the tool's own workspace-write or --full-auto)
// is workspace-write, never more.
func TestSandbox_WriteIsWorkspaceWrite(t *testing.T) {
	for _, r := range []Request{
		{Write: true},
		{Args: []string{"--sandbox", "workspace-write"}},
		{Args: []string{"-s", "workspace-write"}},
		{Args: []string{"--full-auto"}},
		{Write: true, OptIn: []string{host}, Host: host},
	} {
		if d := decide(t, r); d.Mode != WorkspaceWrite || d.Warning != "" {
			t.Fatalf("%+v: %+v, want workspace-write", r, d)
		}
	}
	// --write cannot be combined with the tool's own read-only
	if _, err := Decide(Request{Write: true, Args: []string{"--sandbox", "read-only"}}); err == nil {
		t.Fatal("--write with --sandbox read-only was accepted")
	}
}

// Ladder rung 3: danger-full-access is passed, with a warning, only for a
// host the user config lists; asked for explicitly or because bwrap fails.
func TestSandbox_DangerOnlyForOptedHost(t *testing.T) {
	for _, r := range []Request{
		{Args: []string{"--sandbox", "danger-full-access"}, Host: host, OptIn: []string{"other", " DevBox-1 "}},
		{Args: []string{"--dangerously-bypass-approvals-and-sandbox"}, Host: host, OptIn: []string{host}},
		{Host: host, OptIn: []string{host}, Probe: probeFailed},
		{Write: true, Host: host, OptIn: []string{host}, Probe: probeFailed},
	} {
		d := decide(t, r)
		if d.Mode != DangerFullAccess || !strings.Contains(d.Warning, "danger-full-access") || !strings.Contains(d.Warning, host) {
			t.Fatalf("%+v: %+v, want danger-full-access with a warning naming the host", r, d)
		}
	}
	if d := decide(t, Request{Host: host, OptIn: []string{host}, Probe: probeFailed}); !strings.Contains(d.Warning, probeFailed.Output) {
		t.Fatalf("the widening warning does not carry the probe output: %q", d.Warning)
	}
}

// The other side of rung 3: without the opt-in, danger-full-access is
// refused, and a failed probe refuses with its exact output.
func TestSandbox_DangerRefusedWithoutOptIn(t *testing.T) {
	for _, r := range []Request{
		{Args: []string{"--sandbox", "danger-full-access"}, Host: host},
		{Args: []string{"--sandbox=danger-full-access"}, Host: host, OptIn: []string{"other"}},
		{Args: []string{"--dangerously-bypass-approvals-and-sandbox"}, Host: host},
		{Args: []string{"--sandbox", "danger-full-access"}, Host: "", OptIn: []string{""}},
	} {
		_, err := Decide(r)
		if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "dangerFullAccessHosts") {
			t.Fatalf("%+v: err %v, want a refusal naming the opt-in", r, err)
		}
	}
	_, err := Decide(Request{Host: host, OptIn: []string{"other"}, Probe: probeFailed})
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), probeFailed.Output) || !strings.Contains(err.Error(), host) {
		t.Fatalf("failed probe: err %v, want a refusal with the probe output and the host", err)
	}
	// an unknown probe (a timeout) is not a failure: the run keeps its rung
	if d := decide(t, Request{Probe: ProbeResult{Status: ProbeUnknown}}); d.Mode != ReadOnly {
		t.Fatalf("unknown probe: %+v", d)
	}
	if _, err := Decide(Request{Args: []string{"--sandbox", "everything"}}); err == nil {
		t.Fatal("an unknown sandbox mode was accepted")
	}
}

// The final argv always carries exactly one --sandbox, before the prompt,
// so the launcher's devcontainer fallback (which skips when --sandbox is
// given) can never widen a worker; the caller's own sandbox words go.
func TestSandbox_AlwaysPassesFlagSoLauncherFallbackSkips(t *testing.T) {
	cases := [][]string{
		{"do it"},
		{"-s", "read-only", "do it"},
		{"--full-auto", "--sandbox=workspace-write", "do it"},
		{"-sworkspace-write", "--dangerously-bypass-approvals-and-sandbox", "--", "--sandbox", "prompt"},
	}
	for _, args := range cases {
		got := WithSandbox(args, ReadOnly)
		if RequestedSandbox(got) != string(ReadOnly) || BypassesSandbox(got) {
			t.Fatalf("WithSandbox(%q) = %q, want --sandbox read-only and no bypass", args, got)
		}
		own := got
		if i := slices.Index(got, "--"); i >= 0 {
			own = got[:i]
		}
		n := 0
		for _, w := range own {
			if w == "--sandbox" || strings.HasPrefix(w, "-s") || w == "--full-auto" {
				n++
			}
		}
		if n != 1 {
			t.Fatalf("WithSandbox(%q) = %q: %d sandbox words, want 1", args, got, n)
		}
	}
	// the prompt after -- is never touched
	if got := WithSandbox(cases[3], ReadOnly); !slices.Equal(got[len(got)-3:], []string{"--", "--sandbox", "prompt"}) {
		t.Fatalf("prompt changed: %q", got)
	}
}

// A recorded codex exec --json log yields the thread id and the last
// turn's four usage fields; a log with neither yields nothing.
func TestJSONL_ThreadIDAndUsage(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "exec_thread.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	got := ParseExecJSONL(raw)
	want := ExecInfo{ThreadID: "0199a213-81c0-7800-8aa1-bbab2a035a53", HasUsage: true,
		Usage: Usage{Input: 24763, Cached: 24448, Output: 122, Reasoning: 64}}
	if got != want {
		t.Fatalf("got %+v\nwant %+v", got, want)
	}
	for _, raw := range []string{
		"",
		"plain text\n{\"type\":\"turn.started\"}\n",
		`{"type":"thread.started","thread_id":"has a space"}` + "\n",
		`{"type":"thread.started","thread_id":"/home/someone/x"}` + "\n",
	} {
		if got := ParseExecJSONL([]byte(raw)); got != (ExecInfo{}) {
			t.Fatalf("%q: got %+v, want nothing", raw, got)
		}
	}
}
