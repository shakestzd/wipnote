package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRequireInteractiveLaunch pins the non-TTY refusal (issue #148): a
// scripted or nested `wipnote claude` / `wipnote codex` with no terminal on
// stdin is refused before any launch side effect, unless the caller opted in
// via flag or env, or the pass-through args already select a headless mode.
func TestRequireInteractiveLaunch(t *testing.T) {
	tests := []struct {
		name      string
		harness   string
		tty       bool
		allowFlag bool
		env       string
		extraArgs []string
		wantErr   bool
	}{
		{name: "claude: tty launches", harness: harnessClaude, tty: true},
		{name: "claude: non-tty refused", harness: harnessClaude, wantErr: true},
		{name: "codex: non-tty refused", harness: harnessCodex, wantErr: true},
		{name: "claude: --allow-non-interactive overrides", harness: harnessClaude, allowFlag: true},
		{name: "codex: WIPNOTE_ALLOW_NON_TTY=1 overrides", harness: harnessCodex, env: "1"},
		{name: "env true overrides", harness: harnessClaude, env: "true"},
		{name: "env 0 does not override", harness: harnessClaude, env: "0", wantErr: true},
		{name: "claude -p headless keeps working", harness: harnessClaude, extraArgs: []string{"-p", "hi"}},
		{name: "claude --print headless keeps working", harness: harnessClaude, extraArgs: []string{"--print", "hi"}},
		{name: "codex exec headless keeps working", harness: harnessCodex, extraArgs: []string{"exec", "hi"}},
		{name: "codex: exec not first is still interactive", harness: harnessCodex, extraArgs: []string{"--model", "exec"}, wantErr: true},
		{name: "claude: -p is not a codex headless marker", harness: harnessCodex, extraArgs: []string{"-p", "x"}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(allowNonTTYEnv, tc.env)
			prev := launchStdinIsTTYFn
			launchStdinIsTTYFn = func() bool { return tc.tty }
			t.Cleanup(func() { launchStdinIsTTYFn = prev })

			err := requireInteractiveLaunch(tc.harness, tc.allowFlag, tc.extraArgs)
			if (err != nil) != tc.wantErr {
				t.Fatalf("requireInteractiveLaunch() err = %v, wantErr %v", err, tc.wantErr)
			}
			if err == nil {
				return
			}
			var refusal *errNonInteractiveLaunch
			if !errors.As(err, &refusal) {
				t.Fatalf("error type = %T, want *errNonInteractiveLaunch", err)
			}
			msg := err.Error()
			for _, want := range []string{"stdin is not a terminal", "--" + allowNonInteractiveFlag, allowNonTTYEnv + "=1", "wipnote " + tc.harness} {
				if !strings.Contains(msg, want) {
					t.Errorf("message missing %q:\n%s", want, msg)
				}
			}
		})
	}
}

// TestLaunchCommands_RefuseNonTTYBeforeSideEffects drives the cobra commands
// end to end with a non-TTY stdin and asserts the refusal is the only thing
// that happens: no .wipnote/.launch-mode marker and no session state appear in
// the project, i.e. the guard runs before every launch side effect.
func TestLaunchCommands_RefuseNonTTYBeforeSideEffects(t *testing.T) {
	t.Setenv(allowNonTTYEnv, "")
	prev := launchStdinIsTTYFn
	launchStdinIsTTYFn = func() bool { return false }
	t.Cleanup(func() { launchStdinIsTTYFn = prev })

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "claude default", args: nil},
		{name: "claude --continue", args: []string{"--continue"}},
		{name: "codex default", args: nil},
		{name: "codex --continue", args: []string{"--continue"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectDir := t.TempDir()
			t.Chdir(projectDir)
			t.Setenv("WIPNOTE_PROJECT_DIR", projectDir)
			cmd := claudeCmd()
			if strings.HasPrefix(tc.name, "codex") {
				cmd = codexCmd()
			}
			cmd.SetArgs(tc.args)
			cmd.SetOut(&strings.Builder{})
			cmd.SetErr(&strings.Builder{})
			err := cmd.Execute()
			var refusal *errNonInteractiveLaunch
			if !errors.As(err, &refusal) {
				t.Fatalf("Execute() err = %v, want non-interactive refusal", err)
			}
			if _, statErr := os.Stat(filepath.Join(projectDir, ".wipnote", ".launch-mode")); statErr == nil {
				t.Fatalf("launch marker was written despite the refusal")
			}
			if _, statErr := os.Stat(filepath.Join(projectDir, ".wipnote", ".active-session")); statErr == nil {
				t.Fatalf(".active-session was written despite the refusal")
			}
		})
	}
}
