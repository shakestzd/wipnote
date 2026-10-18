package main

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// TestRunHookNamed_HeadlessSkipsHandler pins that a process wipnote started
// headlessly (WIPNOTE_HEADLESS=1) allows every hook without running it, so
// compliance grading, plan chat and the headless-runs mod record nothing.
func TestRunHookNamed_HeadlessSkipsHandler(t *testing.T) {
	t.Setenv(headlessHookEnv, "1")

	inR, inW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := inW.WriteString(`{"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`); err != nil {
		t.Fatal(err)
	}
	inW.Close()
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origIn, origOut := os.Stdin, os.Stdout
	os.Stdin, os.Stdout = inR, outW
	t.Cleanup(func() { os.Stdin, os.Stdout = origIn, origOut })

	called := false
	runErr := runHookNamed("pretooluse", func(*hooks.CloudEvent) (*hooks.HookResult, error) {
		called = true
		return nil, nil
	})
	outW.Close()
	os.Stdin, os.Stdout = origIn, origOut
	out, _ := io.ReadAll(outR)

	if runErr != nil {
		t.Fatalf("runHookNamed: %v", runErr)
	}
	if called {
		t.Fatal("handler must not run when WIPNOTE_HEADLESS=1")
	}
	if strings.Contains(string(out), "block") || strings.Contains(string(out), "deny") {
		t.Fatalf("headless hook must allow, got %q", out)
	}
}

// TestRunHookNamed_HeadlessAllowsAntigravityPreToolUse guards the Codex review
// finding on #210: Antigravity reads a bare {} on PreToolUse as a deny, so the
// headless allow must carry the event name and emit allowTool:true.
func TestRunHookNamed_HeadlessAllowsAntigravityPreToolUse(t *testing.T) {
	t.Setenv(headlessHookEnv, "1")
	orig := hookHarnessOverride
	hookHarnessOverride = "antigravity"
	t.Cleanup(func() { hookHarnessOverride = orig })

	stdout, _, err := captureHookCommand(t,
		[]byte(`{"hook_event_name":"PreToolUse","tool_name":"run_shell_command","tool_input":{"command":"ls"}}`),
		func() error {
			return runHookNamed("pretooluse", func(*hooks.CloudEvent) (*hooks.HookResult, error) {
				t.Fatal("handler must not run when WIPNOTE_HEADLESS=1")
				return nil, nil
			})
		})
	if err != nil {
		t.Fatalf("runHookNamed: %v", err)
	}
	if !strings.Contains(stdout, `"allowTool":true`) {
		t.Fatalf("headless Antigravity PreToolUse must allow the tool, got %q", stdout)
	}
}
