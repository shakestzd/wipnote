package main

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/hooks"
)

// TestHookSubcmd_CallsHandler pins that the hook dispatcher runs its handler
// for a wipnote project. Handlers take no database at all (feat-174dc958,
// feat-726637b8); the signature enforces that.
func TestHookSubcmd_CallsHandler(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("WIPNOTE_PROJECT_DIR", "")
	t.Setenv("CLAUDE_PROJECT_DIR", "")
	t.Setenv(headlessHookEnv, "")
	projectDir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(projectDir, ".wipnote"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Chdir(projectDir)
	payload := fmt.Sprintf(`{"session_id":"019ee380-abcd-7000-8000-00000000d15b","cwd":%q,`+
		`"hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"ls"}}`, projectDir)
	withStdin(t, payload)

	called := false
	cmd := hookSubcmd("pretooluse", "test", &hooks.HookResult{},
		func(_ *hooks.CloudEvent) (*hooks.HookResult, error) {
			called = true
			return &hooks.HookResult{}, nil
		})
	devNull, _ := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	defer devNull.Close()
	origOut := os.Stdout
	os.Stdout = devNull
	err := cmd.RunE(cmd, nil)
	os.Stdout = origOut
	if err != nil {
		t.Fatalf("RunE: %v", err)
	}
	if !called {
		t.Fatal("handler was not called")
	}
}

// withStdin feeds payload to os.Stdin for the duration of the test.
func withStdin(t *testing.T, payload string) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	if _, err := w.WriteString(payload); err != nil {
		t.Fatalf("write stdin: %v", err)
	}
	_ = w.Close()
	orig := os.Stdin
	os.Stdin = r
	t.Cleanup(func() {
		os.Stdin = orig
		_ = r.Close()
	})
}
