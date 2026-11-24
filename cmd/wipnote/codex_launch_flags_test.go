package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func stubCodexHelp(t *testing.T, help string) {
	t.Helper()
	prev := codexHelpFn
	codexHelpFn = func(string) string { return help }
	t.Cleanup(func() { codexHelpFn = prev })
}

// isolateCodexHome points the launcher at an empty Codex config.
func isolateCodexHome(t *testing.T, config string) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("CODEX_HOME", dir)
	if config != "" {
		if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(config), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

const helpWithNoDaemon = "Options:\n      --no-alt-screen\n          Disable alternate screen\n      --no-daemon\n          Run without the shared background server\n"

// herdr-core's manual requires --no-daemon in an hc terminal. A Codex that
// predates the flag exits on it, so the launcher adds it only when the
// installed Codex lists it (bug-fc843d71).
func TestApplyCodexHostFlags_NoDaemonNeedsAnHcTerminalAndSupport(t *testing.T) {
	isolateCodexHome(t, "")
	for _, tt := range []struct {
		name, terminal, help string
		want                 bool
	}{
		{"hc terminal, flag listed", "t7", helpWithNoDaemon, true},
		{"hc terminal, old codex", "t7", "Options:\n      --no-alt-screen\n", false},
		{"hc terminal, help failed", "t7", "", false},
		{"outside hc", "", helpWithNoDaemon, false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("HC_TERMINAL_ID", tt.terminal)
			stubCodexHelp(t, tt.help)
			opts := codexLaunchOpts{}
			applyCodexHostFlags("codex", &opts)
			if got := slices.Contains(buildCodexArgs(opts, 0, nil), "--no-daemon"); got != tt.want {
				t.Errorf("--no-daemon passed = %v, want %v", got, tt.want)
			}
		})
	}
}

// Codex refuses --add-dir and exits when its sandbox is read-only, its default
// in a project it does not trust. The launcher names workspace-write unless
// the user chose a sandbox themselves (bug-fc843d71).
func TestResolveCodexWritableRoots(t *testing.T) {
	roots := []string{"/store"}
	for _, tt := range []struct {
		name      string
		opts      codexLaunchOpts
		cfg       codexSandboxConfig
		wantMode  string
		wantRoots bool
	}{
		{"no choice anywhere", codexLaunchOpts{WritableRoots: roots}, codexSandboxConfig{}, codexWorkspaceWrite, true},
		{"no writable root", codexLaunchOpts{}, codexSandboxConfig{}, "", false},
		{"yolo", codexLaunchOpts{WritableRoots: roots, Yolo: true}, codexSandboxConfig{}, "", true},
		{"launcher already set a sandbox", codexLaunchOpts{WritableRoots: roots, SandboxMode: "danger-full-access"}, codexSandboxConfig{}, "", true},
		{"user flag", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--sandbox", "danger-full-access"}}, codexSandboxConfig{}, "", true},
		{"user flag read-only drops the root", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"-s", "read-only"}}, codexSandboxConfig{}, "", false},
		{"user bypass", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--dangerously-bypass-approvals-and-sandbox"}}, codexSandboxConfig{}, "", true},
		{"approve-for-me picks its own", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--approve-for-me"}}, codexSandboxConfig{}, "", true},
		{"profile flag", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"-p", "work"}}, codexSandboxConfig{}, "", true},
		{"-c override", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"-c", `sandbox_mode="workspace-write"`}}, codexSandboxConfig{}, "", true},
		{"-c override read-only", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--config=sandbox_mode=read-only"}}, codexSandboxConfig{}, "", false},
		{"configured mode is kept", codexLaunchOpts{WritableRoots: roots}, codexSandboxConfig{SandboxMode: "danger-full-access"}, "", true},
		{"configured read-only drops the root", codexLaunchOpts{WritableRoots: roots}, codexSandboxConfig{SandboxMode: "read-only"}, "", false},
		{"configured profile is kept", codexLaunchOpts{WritableRoots: roots}, codexSandboxConfig{Profile: "work", SandboxMode: "read-only"}, "", true},
		{"a flag beats the config", codexLaunchOpts{WritableRoots: roots, ExtraArgs: []string{"--sandbox=workspace-write"}}, codexSandboxConfig{SandboxMode: "read-only"}, "", true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			mode, got, notice := resolveCodexWritableRoots(tt.opts, tt.cfg)
			if mode != tt.wantMode || (len(got) > 0) != tt.wantRoots {
				t.Errorf("mode = %q roots = %q, want mode %q roots kept %v", mode, got, tt.wantMode, tt.wantRoots)
			}
			changed := mode != "" || (len(tt.opts.WritableRoots) > 0 && len(got) == 0)
			if changed != (notice != "") {
				t.Errorf("notice = %q for a change = %v", notice, changed)
			}
		})
	}
}

// End to end through the argv: the sandbox flag comes before --add-dir, and a
// configured sandbox_mode is read from the user's Codex config.
func TestApplyCodexHostFlags_WritableRootArgv(t *testing.T) {
	t.Setenv("HC_TERMINAL_ID", "")
	isolateCodexHome(t, "")
	opts := codexLaunchOpts{WritableRoots: []string{"/store"}}
	applyCodexHostFlags("codex", &opts)
	if got := strings.Join(buildCodexArgs(opts, 0, nil), " "); !strings.Contains(got, "--sandbox workspace-write --add-dir /store") {
		t.Errorf("argv = %q", got)
	}

	isolateCodexHome(t, "model = \"x\"\nsandbox_mode = \"read-only\"\n")
	opts = codexLaunchOpts{WritableRoots: []string{"/store"}}
	applyCodexHostFlags("codex", &opts)
	if got := buildCodexArgs(opts, 0, nil); slices.Contains(got, "--add-dir") || slices.Contains(got, "--sandbox") {
		t.Errorf("configured read-only: argv = %q", got)
	}
}
