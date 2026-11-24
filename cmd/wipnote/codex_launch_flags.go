package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"
)

// The flags wipnote adds to a Codex launch on its own account. Each one is
// checked against the installed Codex or the user's own sandbox choice first,
// because a flag Codex refuses stops the launch before the TUI starts
// (bug-fc843d71).

const codexWorkspaceWrite = "workspace-write"

// codexHelpFn returns the text of `codex --help`. It is a package-level var
// so tests can describe a Codex version without running one.
var codexHelpFn = func(codexPath string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, codexPath, "--help")
	// codex is a node script that starts a native binary. Killing node leaves
	// the child holding the pipe, so bound the wait for it too.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	return string(out), err
}

// codexAcceptsFlag reports whether the installed Codex lists flag in its help.
// An error means the question was not answered.
func codexAcceptsFlag(codexPath, flag string) (bool, error) {
	help, err := codexHelpFn(codexPath)
	if err != nil {
		return false, err
	}
	for _, word := range strings.Fields(help) {
		if strings.TrimRight(word, ",") == flag {
			return true, nil
		}
	}
	return false, nil
}

// applyCodexHostFlags settles the flags that depend on the installed Codex
// and on the user's sandbox choice, and prints a note for each change.
func applyCodexHostFlags(codexPath string, opts *codexLaunchOpts) {
	if note := applyCodexNoDaemon(codexPath, opts); note != "" {
		fmt.Fprintln(os.Stderr, note)
	}
	mode, roots, notice := resolveCodexWritableRoots(*opts, readCodexSandboxChoice(*opts))
	if mode != "" {
		opts.SandboxMode = mode
	}
	opts.WritableRoots = roots
	if notice != "" {
		fmt.Fprintln(os.Stderr, notice)
	}
}

// applyCodexNoDaemon sets opts.NoDaemon and returns a note when it leaves the
// flag out of an hc terminal launch.
//
// herdr-core's manual: Codex must run with --no-daemon in an hc terminal, or
// its commands execute in the shared server, outside that terminal. A Codex
// that predates the flag exits on it, and so does one that is also given
// --remote, so the launcher asks before it adds the flag.
func applyCodexNoDaemon(codexPath string, opts *codexLaunchOpts) string {
	if os.Getenv("HC_TERMINAL_ID") == "" {
		return ""
	}
	if codexUsesRemoteServer(opts.ExtraArgs) {
		return "wipnote: --no-daemon left out, because Codex refuses it together with --remote."
	}
	ok, err := codexAcceptsFlag(codexPath, "--no-daemon")
	switch {
	case err != nil:
		return fmt.Sprintf("wipnote: could not read `codex --help` (%v), so wipnote left out --no-daemon. Codex commands may run outside this terminal.", err)
	case !ok:
		return "wipnote: this Codex does not list --no-daemon, so wipnote left it out. Upgrade Codex if its commands run outside this terminal."
	}
	opts.NoDaemon = true
	return ""
}

// codexUsesRemoteServer reports whether args connect the TUI to a remote app
// server, which Codex does not allow together with --no-daemon.
func codexUsesRemoteServer(args []string) bool {
	return slices.ContainsFunc(args, func(a string) bool {
		return a == "--remote" || strings.HasPrefix(a, "--remote=")
	})
}

// resolveCodexWritableRoots decides how a launch that needs extra writable
// folders (the app store) gets them. Codex prints "Error adding directories"
// and exits when --add-dir meets a read-only sandbox, which is its default in
// a project with no trust entry (seen on Codex 0.172.0). It returns the
// sandbox to pass ("" for none), the roots to pass and a note ("" for none).
//
// wipnote names a sandbox only when nothing else did. Any choice the user
// made is kept, and a read-only choice drops the roots instead.
func resolveCodexWritableRoots(opts codexLaunchOpts, choice codexSandboxChoice) (mode string, roots []string, notice string) {
	roots = opts.WritableRoots
	if len(roots) == 0 || opts.Yolo || opts.SandboxMode != "" || codexBypassesSandbox(opts.ExtraArgs) {
		return "", roots, ""
	}
	switch {
	case choice.ReadOnly:
		return "", nil, "wipnote: Codex runs read-only here (" + choice.Source + "), so the wipnote store is not writable. Commands that write work items will fail in this session."
	case choice.Set:
		return "", roots, ""
	}
	return codexWorkspaceWrite, roots,
		"wipnote: Codex has no trust entry or sandbox setting for this project. Launching with --sandbox " + codexWorkspaceWrite + " so the session can write the wipnote store. Pass --sandbox <mode> to choose another."
}
