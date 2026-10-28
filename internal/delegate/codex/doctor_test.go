package codex

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

// The doctor names each check with pass/fail and the probe's exact output,
// and a failed bwrap fails it unless the host opted in (then it warns).
func TestDoctor_NamesEachCheck(t *testing.T) {
	stubPath(t)
	o := DoctorOptions{Host: host, Probe: probeFailed}
	checks := Doctor(context.Background(), o)
	var names []string
	for _, c := range checks {
		names = append(names, c.Name)
	}
	if got := strings.Join(names, "|"); got != "codex|bwrap sandbox|danger-full-access opt-in|sandbox ladder" {
		t.Fatalf("checks %q", got)
	}
	var out bytes.Buffer
	failed := WriteChecks(&out, checks)
	text := out.String()
	for _, want := range []string{"pass  codex", "codex-cli 0.164.0", "fail  bwrap sandbox", probeFailed.Output, host, "fail  sandbox ladder"} {
		if !strings.Contains(text, want) {
			t.Fatalf("report lacks %q:\n%s", want, text)
		}
	}
	if failed == 0 {
		t.Fatalf("a failed probe on a host that did not opt in passed:\n%s", text)
	}

	o.OptIn = []string{host}
	out.Reset()
	if failed := WriteChecks(&out, Doctor(context.Background(), o)); failed != 0 {
		t.Fatalf("opted host: %d failed\n%s", failed, out.String())
	}
	if text := out.String(); !strings.Contains(text, "warn  bwrap sandbox") || !strings.Contains(text, "danger-full-access") {
		t.Fatalf("opted host does not warn:\n%s", text)
	}

	t.Setenv("PATH", t.TempDir())
	out.Reset()
	WriteChecks(&out, Doctor(context.Background(), DoctorOptions{Host: host, Probe: ProbeResult{Status: ProbePass}}))
	if !strings.Contains(out.String(), "fail  codex") {
		t.Fatalf("missing codex passed:\n%s", out.String())
	}
}
