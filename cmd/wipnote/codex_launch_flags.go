package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/shakestzd/wipnote/core/hooks"
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
func codexAcceptsFlag(help func() (string, error), flag string) (bool, error) {
	text, err := help()
	if err != nil {
		return false, err
	}
	for _, word := range strings.Fields(text) {
		if strings.TrimRight(word, ",") == flag {
			return true, nil
		}
	}
	return false, nil
}

// applyCodexHostFlags settles the flags that depend on the installed Codex
// and on the user's sandbox choice, and prints a note for each change.
func applyCodexHostFlags(codexPath string, opts *codexLaunchOpts) {
	// One `codex --help` run answers every flag question of this launch.
	help := sync.OnceValues(func() (string, error) { return codexHelpFn(codexPath) })
	notes := []string{applyCodexNoDaemon(help, opts), applyCodexApproveForMe(help, opts)}
	mode, roots, notice := resolveCodexWritableRoots(*opts, readCodexSandboxChoice(*opts))
	if mode != "" {
		opts.SandboxMode = mode
	}
	opts.WritableRoots = roots
	for _, note := range append(notes, notice) {
		if note != "" {
			fmt.Fprintln(os.Stderr, note)
		}
	}
}

const codexApproveForMe = "--approve-for-me"

// applyCodexApproveForMe makes Codex's automatic approval review the default
// for a wipnote launch (feat-c1c93fd2: the owner wants Codex to start the way
// Claude Code's auto mode does). It adds the flag only when the user decided
// nothing about the sandbox or approvals, so every other choice still wins.
// The flag selects the workspace-write sandbox, so it is also left out where
// wipnote itself picked a sandbox, and for Codex subcommands that open no
// session. It returns a note when it adds the flag.
func applyCodexApproveForMe(help func() (string, error), opts *codexLaunchOpts) string {
	if opts.Yolo || opts.SandboxMode != "" || codexBypassesSandbox(opts.ExtraArgs) {
		return ""
	}
	// A delegated worker runs with nobody watching it. It keeps the mode its
	// caller's recipe gives it, and never gets this default (owner decision
	// on feat-c1c93fd2).
	if os.Getenv(hooks.DelegateRunEnv) != "" {
		return ""
	}
	// Flag-like words after a forwarded "--" are prompt text to Codex. The
	// user may have meant them as flags, such as --sandbox read-only, so the
	// default must not widen that launch.
	if codexDashWordsAfterSeparator(opts.ExtraArgs) {
		return "wipnote: the words after -- reach Codex as prompt text, not as flags, so wipnote left out " + codexApproveForMe + ". Put Codex flags before the prompt."
	}
	if words := passthroughLaunchers[harnessCodex].positionals(opts.ExtraArgs); len(words) > 0 &&
		passthroughLaunchers[harnessCodex].subcommands[words[0]] && words[0] != "resume" && words[0] != "fork" {
		return ""
	}
	// An untrusted mark is the user's own stricter approval choice.
	if choice := readCodexSandboxChoice(*opts); (choice.Set && !choice.Trust) || choice.Untrusted || codexApprovalChosen(*opts) {
		return ""
	}
	// An older Codex exits on a flag it does not know. A failed probe leaves
	// the flag out too: Codex then starts in its own default mode.
	if ok, err := codexAcceptsFlag(help, codexApproveForMe); err != nil || !ok {
		return ""
	}
	opts.ExtraArgs = append([]string{codexApproveForMe}, opts.ExtraArgs...)
	return "wipnote: launching Codex with " + codexApproveForMe + " (automatic approval review, workspace-write sandbox). Pass -a <policy> or --sandbox <mode> to choose another."
}

// codexDashWordsAfterSeparator reports whether args hold a "--" followed by a
// word that starts with a dash.
func codexDashWordsAfterSeparator(args []string) bool {
	at := slices.Index(args, "--")
	return at >= 0 && slices.ContainsFunc(args[at+1:], func(a string) bool { return strings.HasPrefix(a, "-") && a != "-" })
}

// applyCodexNoDaemon sets opts.NoDaemon and returns a note when it leaves the
// flag out of an hc terminal launch.
//
// herdr-core's manual: Codex must run with --no-daemon in an hc terminal, or
// its commands execute in the shared server, outside that terminal. A Codex
// that predates the flag exits on it, and so does one that is also given
// --remote, so the launcher asks before it adds the flag.
func applyCodexNoDaemon(help func() (string, error), opts *codexLaunchOpts) string {
	if os.Getenv("HC_TERMINAL_ID") == "" {
		return ""
	}
	if codexUsesRemoteServer(opts.ExtraArgs) {
		return "wipnote: --no-daemon left out, because Codex refuses it with a remote or shared server."
	}
	ok, err := codexAcceptsFlag(help, "--no-daemon")
	switch {
	case err != nil:
		return fmt.Sprintf("wipnote: could not read `codex --help` (%v), so wipnote left out --no-daemon. Codex commands may run outside this terminal.", err)
	case !ok:
		return "wipnote: this Codex does not list --no-daemon, so wipnote left it out. Upgrade Codex if its commands run outside this terminal."
	}
	opts.NoDaemon = true
	return ""
}

// codexUsesRemoteServer reports whether args use a remote or shared app
// server, which Codex does not allow together with --no-daemon: the --remote
// flag and the queue and remote-control subcommands.
func codexUsesRemoteServer(args []string) bool {
	if slices.ContainsFunc(args, func(a string) bool { return a == "--remote" || strings.HasPrefix(a, "--remote=") }) {
		return true
	}
	words := passthroughLaunchers[harnessCodex].positionals(args)
	return len(words) > 0 && (words[0] == "queue" || words[0] == "remote-control")
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
