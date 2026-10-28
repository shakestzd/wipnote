package codex

import (
	"context"
	"slices"
	"testing"
)

// --yolo is Codex's alias for the bypass flag: it must count as a bypass and
// never survive next to the injected --sandbox.
func TestYoloAliasIsABypass(t *testing.T) {
	args := []string{"exec", "--yolo", "--", "do it"}
	if !BypassesSandbox(args) {
		t.Fatal("--yolo must count as bypassing the sandbox")
	}
	if BypassesSandbox([]string{"exec", "--", "--yolo"}) {
		t.Fatal("--yolo after -- is prompt, not a flag")
	}
	got := WithSandbox(args, ReadOnly)
	if slices.Contains(got[:slices.Index(got, "--")], "--yolo") {
		t.Fatalf("--yolo kept beside the injected sandbox: %q", got)
	}
}

// No system bwrap on PATH is inconclusive: Codex falls back to its bundled
// bubblewrap, so the preflight must not refuse the run for it.
func TestProbe_MissingSystemBwrapIsInconclusive(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	r := Probe(context.Background(), "linux")
	if r.Status != ProbeUnknown {
		t.Fatalf("status %q, want %q: %s", r.Status, ProbeUnknown, r.Output)
	}
}
