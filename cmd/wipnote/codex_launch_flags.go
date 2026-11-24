package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	toml "github.com/pelletier/go-toml/v2"
)

// The flags wipnote adds to a Codex launch on its own account. Each one is
// checked against the installed Codex or the user's own sandbox choice first,
// because a flag Codex refuses stops the launch before the TUI starts
// (bug-fc843d71).

const codexWorkspaceWrite = "workspace-write"

// codexHelpFn returns the text of `codex --help`. It is a package-level var
// so tests can describe a Codex version without running one.
var codexHelpFn = func(codexPath string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, codexPath, "--help").CombinedOutput()
	return string(out)
}

// codexAcceptsFlag reports whether the installed Codex lists flag in its help.
func codexAcceptsFlag(codexPath, flag string) bool {
	for _, word := range strings.Fields(codexHelpFn(codexPath)) {
		if strings.TrimRight(word, ",") == flag {
			return true
		}
	}
	return false
}

// applyCodexHostFlags settles the flags that depend on the installed Codex
// and on the user's sandbox choice, and prints a note for each change.
func applyCodexHostFlags(codexPath string, opts *codexLaunchOpts) {
	// herdr-core's manual: Codex must run with --no-daemon in an hc terminal,
	// or its commands execute in the shared server, outside that terminal.
	// A Codex that predates the flag exits on it, so ask before adding it.
	if os.Getenv("HC_TERMINAL_ID") != "" {
		if codexAcceptsFlag(codexPath, "--no-daemon") {
			opts.NoDaemon = true
		} else {
			fmt.Fprintln(os.Stderr, "wipnote: this Codex does not accept --no-daemon, so wipnote left it out. Upgrade Codex if its commands run outside this terminal.")
		}
	}
	mode, roots, notice := resolveCodexWritableRoots(*opts, readCodexSandboxConfig(codexHomeConfigPath()))
	if mode != "" {
		opts.SandboxMode = mode
	}
	opts.WritableRoots = roots
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
}

// codexSandboxConfig is what the user's Codex config says about the sandbox.
type codexSandboxConfig struct {
	SandboxMode string `toml:"sandbox_mode"`
	Profile     string `toml:"profile"`
}

// readCodexSandboxConfig reads the user config layer. A missing or unparsable
// file counts as no choice.
func readCodexSandboxConfig(path string) codexSandboxConfig {
	var cfg codexSandboxConfig
	if data, err := os.ReadFile(path); err == nil {
		_ = toml.Unmarshal(data, &cfg)
	}
	return cfg
}

// resolveCodexWritableRoots decides how a launch that needs extra writable
// folders (the app store) gets them. Codex refuses --add-dir and exits when
// the effective sandbox is read-only, which is its default in a project it
// does not yet trust. It returns the sandbox to pass ("" for none), the roots
// to pass and a note for the user ("" for none).
//
// The user's own choice always wins: a sandbox flag, a bypass, a -c override,
// a profile or a configured sandbox_mode is never replaced.
func resolveCodexWritableRoots(opts codexLaunchOpts, cfg codexSandboxConfig) (mode string, roots []string, notice string) {
	roots = opts.WritableRoots
	if len(roots) == 0 || opts.Yolo || opts.SandboxMode != "" || codexBypassesSandbox(opts.ExtraArgs) {
		return "", roots, ""
	}
	chosen := codexRequestedSandbox(opts.ExtraArgs)
	if chosen == "" {
		chosen = codexSandboxOverride(opts.ExtraArgs)
	}
	if chosen == "" && codexPicksOwnSandbox(opts.ExtraArgs) {
		return "", roots, ""
	}
	if chosen == "" && cfg.Profile == "" {
		chosen = cfg.SandboxMode
	}
	switch {
	case chosen == "read-only":
		return "", nil, "wipnote: Codex runs read-only here, so the wipnote store is not writable. Commands that write work items will fail in this session."
	case chosen != "" || cfg.Profile != "":
		return "", roots, ""
	}
	return codexWorkspaceWrite, roots,
		"wipnote: launching Codex with --sandbox " + codexWorkspaceWrite + " so the session can write the wipnote store. Pass --sandbox <mode> to choose another."
}

// codexSandboxOverride is the sandbox_mode a -c/--config override sets, or "".
func codexSandboxOverride(args []string) string {
	for i, a := range args {
		var kv string
		switch {
		case a == "--":
			return ""
		case (a == "-c" || a == "--config") && i+1 < len(args):
			kv = args[i+1]
		case strings.HasPrefix(a, "--config="):
			kv = strings.TrimPrefix(a, "--config=")
		}
		if v, ok := strings.CutPrefix(kv, "sandbox_mode="); ok {
			return strings.Trim(v, `"'`)
		}
	}
	return ""
}

// codexPicksOwnSandbox reports whether args carry a flag that selects the
// sandbox by another route: a preset flag or a profile.
func codexPicksOwnSandbox(args []string) bool {
	for _, a := range args {
		switch {
		case a == "--":
			return false
		case a == "--approve-for-me", a == "--full-auto", a == "-p", a == "--profile", strings.HasPrefix(a, "--profile="):
			return true
		}
	}
	return false
}
