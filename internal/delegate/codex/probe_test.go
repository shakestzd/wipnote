package codex

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// stubPath puts the testdata stubs (bwrap, codex) first on PATH.
func stubPath(t *testing.T) {
	t.Helper()
	stubs, err := filepath.Abs(filepath.Join("testdata", "stubs"))
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", stubs+string(os.PathListSeparator)+os.Getenv("PATH"))
}

// A bwrap that fails on namespaces is reported as a failure with its exact
// output; one that succeeds passes. Off Linux there is no bwrap to probe.
func TestSandboxProbe_ReportsBwrapFailure(t *testing.T) {
	stubPath(t)
	const msg = "bwrap: No permissions to create new namespace, likely because the kernel does not allow non-privileged user namespaces."
	t.Setenv("STUB_BWRAP_EXIT", "1")
	t.Setenv("STUB_BWRAP_OUT", msg)
	got := Probe(context.Background(), "linux")
	if got.Status != ProbeFail || !strings.Contains(got.Output, msg) {
		t.Fatalf("failing bwrap: %+v", got)
	}
	t.Setenv("STUB_BWRAP_EXIT", "0")
	t.Setenv("STUB_BWRAP_OUT", "")
	if got := Probe(context.Background(), "linux"); got.Status != ProbePass {
		t.Fatalf("working bwrap: %+v", got)
	}
	// an unrelated failure is not a bwrap failure: unknown, not fail
	t.Setenv("STUB_BWRAP_EXIT", "2")
	t.Setenv("STUB_BWRAP_OUT", "something else broke")
	if got := Probe(context.Background(), "linux"); got.Status != ProbeUnknown {
		t.Fatalf("unrelated failure: %+v", got)
	}
	if got := Probe(context.Background(), "darwin"); got.Status != ProbeSkip {
		t.Fatalf("darwin: %+v", got)
	}
}

// The probe is bounded: a hung bwrap reads as unknown, never blocks.
func TestSandboxProbe_TimesOut(t *testing.T) {
	stubPath(t)
	t.Setenv("STUB_BWRAP_SLEEP", "5")
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	start := time.Now()
	got := Probe(ctx, "linux")
	if got.Status != ProbeUnknown || time.Since(start) > 3*time.Second {
		t.Fatalf("hung bwrap: %+v after %v", got, time.Since(start))
	}
}

// The preflight caches a definite result per host for its TTL, then probes
// again; an unknown result is not cached.
func TestPreflight_CachesPerHost(t *testing.T) {
	stubPath(t)
	dir := t.TempDir()
	now := time.Date(2026, 10, 28, 12, 0, 0, 0, time.UTC)
	p := Preflight{Dir: dir, Host: host, TTL: time.Minute, GOOS: "linux", Now: func() time.Time { return now }}
	t.Setenv("STUB_BWRAP_EXIT", "1")
	t.Setenv("STUB_BWRAP_OUT", "bwrap: setting up uid map: Permission denied")
	if got := p.Result(context.Background()); got.Status != ProbeFail {
		t.Fatalf("first: %+v", got)
	}
	t.Setenv("STUB_BWRAP_EXIT", "0")
	if got := p.Result(context.Background()); got.Status != ProbeFail {
		t.Fatalf("within the TTL the cached failure is used: %+v", got)
	}
	other := p
	other.Host = "another-host"
	if got := other.Result(context.Background()); got.Status != ProbePass {
		t.Fatalf("another host shares the cache: %+v", got)
	}
	now = now.Add(2 * time.Minute)
	if got := p.Result(context.Background()); got.Status != ProbePass {
		t.Fatalf("after the TTL the probe runs again: %+v", got)
	}
	t.Setenv("STUB_BWRAP_EXIT", "2")
	t.Setenv("STUB_BWRAP_OUT", "unrelated")
	now = now.Add(2 * time.Minute)
	_ = p.Result(context.Background())
	t.Setenv("STUB_BWRAP_EXIT", "0")
	if got := p.Result(context.Background()); got.Status != ProbePass {
		t.Fatalf("an unknown result was cached: %+v", got)
	}
}

// The opt-in is read from the user config only; the committed project file
// (and the local file) are ignored with a note.
func TestOptIn_UserConfigOnly(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	user := write("user.json", `{"delegate":{"codex":{"dangerFullAccessHosts":["devbox-1"," "]}}}`)
	project := write("project.json", `{"delegate":{"codex":{"dangerFullAccessHosts":["evil"]}}}`)
	plain := write("plain.json", `{"delegate":{"layout":"tile"}}`)
	hosts, notes := LoadOptIn(user, project, plain, filepath.Join(dir, "missing.json"))
	if len(hosts) != 1 || hosts[0] != "devbox-1" {
		t.Fatalf("hosts %q", hosts)
	}
	if len(notes) != 1 || !strings.Contains(notes[0], "project.json") {
		t.Fatalf("notes %q, want one naming the ignored file", notes)
	}
	if hosts, _ := LoadOptIn(project); len(hosts) != 1 {
		t.Fatalf("the first file is the user file: %q", hosts)
	}
	if hosts, notes := LoadOptIn(""); hosts != nil || notes != nil {
		t.Fatalf("no user config: %q %q", hosts, notes)
	}
}
