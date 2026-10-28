package main

import (
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// TestHookCmd_WiresDelegateRedirect: `wipnote hook` installs the real
// bare-delegation recogniser before any handler runs (plan-28b4a7f6 slice 9),
// and the wired recogniser denies a bare codex exec but not ls.
func TestHookCmd_WiresDelegateRedirect(t *testing.T) {
	prev := hooks.DelegateRedirect
	t.Cleanup(func() { hooks.DelegateRedirect = prev })
	hooks.DelegateRedirect = nil

	cmd := hookCmd()
	cmd.PersistentPreRun(cmd, nil)
	if hooks.DelegateRedirect == nil {
		t.Fatal("hook command did not wire hooks.DelegateRedirect")
	}
	if hooks.DelegateRedirect(`codex exec "x"`, "") == "" {
		t.Error("wired recogniser does not redirect a bare codex exec")
	}
	if hooks.DelegateRedirect("ls", "") != "" {
		t.Error("wired recogniser redirects ls")
	}
}
