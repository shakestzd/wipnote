package main

import (
	"fmt"
	"os"
	"os/exec"

	"github.com/shakestzd/wipnote/internal/launcher"
)

// codexLaunchOpts controls how Codex is launched.
type codexLaunchOpts struct {
	// ResumeLast, when true, passes "resume --last" to codex.
	ResumeLast bool
	// ResumeID, if non-empty, passes "resume <id>" to codex.
	// Takes precedence over ResumeLast.
	ResumeID string
	// ExtraArgs are forwarded to the codex process.
	ExtraArgs []string
	// ProjectRoot is the absolute path to the project root (or worktree path).
	// When set, Codex is started with this as the working directory, and
	// WIPNOTE_PROJECT_DIR env var is injected.
	ProjectRoot string
	// WorktreeRoot, when non-empty, overrides the working directory for the
	// Codex process. The process runs in WorktreeRoot but WIPNOTE_PROJECT_DIR
	// is set to WipnoteRoot (the canonical project root with .wipnote/).
	WorktreeRoot string
	// WipnoteRoot is the canonical project root containing .wipnote/.
	// Used to set WIPNOTE_PROJECT_DIR when running in a worktree.
	WipnoteRoot string
	// Mode selects the wipnote instruction addendum passed to Codex as
	// developer_instructions.
	Mode codexLaunchMode
	// Yolo passes Codex's explicit approvals/sandbox bypass flag before any
	// subcommand, matching Claude's bypassPermissions launcher behavior.
	Yolo bool
	// SandboxMode overrides Codex's default sandbox selection for this launch.
	// Empty means inherit Codex's own config/default behavior.
	SandboxMode string
	// WritableRoots are passed to Codex before any subcommand so resumed
	// sessions and spawned subagents inherit required writable directories.
	WritableRoots []string
	// ExtraEnv is layered onto the child process after the launcher sets its
	// standard wipnote and telemetry environment.
	ExtraEnv []string
}

// execCodex builds the codex argv and execs it, replacing the current process.
// Returns only on exec error.
func execCodex(opts codexLaunchOpts) error {
	codexPath, err := exec.LookPath("codex")
	if err != nil {
		return fmt.Errorf("codex not found in PATH: %w\nInstall Codex CLI first: https://github.com/openai/codex", err)
	}

	// Resolve the effective project dir for OTel collector spawning.
	effectiveProjDir := opts.effectiveProjectDir()

	// Write launch marker to the main project root (mirrors launchClaude and
	// execAntigravity — see writeLaunchMarker in claude.go). Codex hooks read
	// this to distinguish a launcher-started session (daemon guaranteed) from
	// a bare/IDE/desktop-app session: unlike Claude's ephemeral --plugin-dir
	// model, Codex hooks persist across every entry point once installed
	// (spk-5a716533), so this marker is the only durable signal that a
	// wipnote launcher is behind THIS session.
	writeLaunchMarker(string(opts.effectiveMode()), effectiveProjDir)

	// Start the work-item daemon now and collect it just before exec. Only a
	// launcher starts the writer (bug-5ca53f70): Codex hooks never do.
	daemonEnsure := startDaemonEnsure(effectiveProjDir)

	// Launch-time guard-profile initialization (peer to the OTel collector
	// bootstrap). No-op when already approved or non-interactive; never blocks.
	ensureGuardProfile(effectiveProjDir)

	otelPort, otelSessionID, otelCleanup := spawnCodexOtelIfEnabled(effectiveProjDir)
	if otelCleanup != nil {
		defer otelCleanup()
	}

	instructionArgs := prepareCodexLaunchDir(opts)
	sandboxDegraded := applyCodexSandboxMode(codexPath, &opts)
	// No `-c agents.<name>.*` flags: Codex discovers roles from each config
	// layer's agents/ folder (codex-rs/agent-roles/src/loader.rs:75-79), so
	// the TOMLs synced into ~/.codex/agents are registered natively. The old
	// flags re-registered every TOML (the user's own agents included) a
	// second time at the session-flags layer.
	c := newCodexCommand(codexPath, buildCodexArgs(opts, otelPort, instructionArgs))

	env, workDir := buildCodexChildEnv(opts, otelPort, otelSessionID, effectiveProjDir, sandboxDegraded, daemonEnsure)
	c.Env = env
	if workDir != "" {
		c.Dir = workDir
	}

	return runHarnessWithCleanup(c, otelCleanup)
}

// effectiveProjectDir is the project dir used for OTel collector spawning and
// the launcher-side session state: the wipnote root when set, else the
// project root.
func (opts codexLaunchOpts) effectiveProjectDir() string {
	effectiveProjDir := opts.ProjectRoot
	if opts.WipnoteRoot != "" {
		effectiveProjDir = opts.WipnoteRoot
	}
	return effectiveProjDir
}

// spawnCodexOtelIfEnabled spawns a per-session OTel collector when a project
// dir is known and OTel is not explicitly disabled. Non-fatal: falls back
// gracefully on failure. The caller defers a non-nil cleanup.
func spawnCodexOtelIfEnabled(effectiveProjDir string) (otelPort int, otelSessionID string, otelCleanup func()) {
	if effectiveProjDir != "" && !isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_ENABLED")) {
		return spawnCodexOtelCollector(effectiveProjDir)
	}
	return 0, "", nil
}

// newCodexCommand builds the codex exec.Cmd wired to this process's stdio.
func newCodexCommand(codexPath string, codexArgs []string) *exec.Cmd {
	c := exec.Command(codexPath, codexArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c
}

// prepareCodexLaunchDir builds the developer-instruction config args for the
// launch directory and removes legacy project agent copies there.
func prepareCodexLaunchDir(opts codexLaunchOpts) []string {
	launchDir := opts.WorktreeRoot
	if launchDir == "" {
		launchDir = opts.ProjectRoot
	}
	instructionArgs := buildCodexInstructionConfigArgs(opts.effectiveMode(),
		configuredCodexDeveloperInstructions(codexInstructionLayers{
			System:  codexSystemConfigPath,
			User:    codexHomeConfigPath(),
			Profile: codexProfileFromArgs(opts.ExtraArgs),
		}))
	if codexProjectSetsDeveloperInstructions(launchDir) {
		fmt.Fprintf(os.Stderr, "note: %s/.codex/config.toml sets developer_instructions; wipnote's instructions replace it for this session\n", launchDir)
	}
	// The setup-time cleanup scans the current checkout only; a launch into a
	// different worktree must not keep legacy project copies that shadow
	// ~/.codex/agents (project config outranks the user layer).
	if launchDir != "" {
		if _, err := removeCodexProjectAgentCopies(launchDir); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not remove legacy project wipnote Codex agents in %s: %v\n", launchDir, err)
		}
	}
	return instructionArgs
}

// applyCodexSandboxMode applies the probed sandbox degradation to opts and
// reports whether the sandbox was degraded.
func applyCodexSandboxMode(codexPath string, opts *codexLaunchOpts) bool {
	sandboxDegraded := false
	if sandboxMode, notice := resolveCodexSandboxMode(codexPath, *opts, isDevcontainer()); sandboxMode != "" {
		opts.SandboxMode = sandboxMode
		sandboxDegraded = true
		fmt.Fprintln(os.Stderr, renderCodexWarningBanner(notice))
	}
	return sandboxDegraded
}

// buildCodexChildEnv builds the Codex child environment and working
// directory.
func buildCodexChildEnv(opts codexLaunchOpts, otelPort int, otelSessionID, effectiveProjDir string, sandboxDegraded bool, daemonEnsure *pendingDaemon) (env []string, workDir string) {
	// Build the child env: start from os.Environ, inject WIPNOTE_PROJECT_DIR,
	// and layer OTel exporter vars when a collector was spawned.
	env = os.Environ()

	switch {
	case opts.WorktreeRoot != "":
		projectDir := opts.WipnoteRoot
		if projectDir == "" {
			projectDir = opts.ProjectRoot
		}
		env = launcher.SetEnv(env, "WIPNOTE_PROJECT_DIR", projectDir)
		workDir = opts.WorktreeRoot
	case opts.ProjectRoot != "":
		env = launcher.SetEnv(env, "WIPNOTE_PROJECT_DIR", opts.ProjectRoot)
		workDir = opts.ProjectRoot
	}

	// No WIPNOTE_DB_PATH is injected: wipnote no longer keeps a per-project
	// SQLite file, so there is no path to hand down. A stale value inherited
	// from the parent environment is deliberately left alone rather than
	// unset — nothing in the child reads it, and clearing it would be a
	// silent mutation of the operator's own environment.
	env = buildCodexOtelEnv(env, otelPort, otelSessionID)
	env = buildCodexAgentEnv(env)
	env = launcher.AppendOrReplaceEnv(env, opts.ExtraEnv...)

	env = codexSessionFamilyEnv(env, opts, otelSessionID, effectiveProjDir)

	// Signal YOLO mode to hook subprocesses. WIPNOTE_YOLO=1 is set only on an
	// explicit --yolo launch so the worktree source-isolation guard fires under
	// Codex just as it does under Claude Code's bypassPermissions path.
	if opts.Yolo {
		env = launcher.SetEnv(env, "WIPNOTE_YOLO", "1")
	}

	// When the bwrap sandbox probe determined that Bubblewrap is unavailable
	// and we automatically degraded to danger-full-access, record that outcome
	// in the session environment. WIPNOTE_CODEX_SANDBOX=degraded signals
	// agents and hooks running inside this Codex session to stop retrying
	// nested codex exec / sandbox paths after the first failure — the
	// devcontainer is already the isolation boundary. Only set this in the
	// auto-degraded case; explicit --sandbox and --yolo launches do not set it
	// so they don't silently suppress legitimate sandboxing errors.
	env = applySandboxDegradedEnv(env, sandboxDegraded)

	env = withHarnessEnv(env, harnessCodex)
	env = launcher.AppendOrReplaceEnv(env, daemonEnsure.sessionEnv(os.Stderr, daemonJoinBudget)...)
	return env, workDir
}

// codexSessionFamilyEnv implements session-family continuity (slice-4,
// feat-a225ce7c):
// Resolve which family this Codex session belongs to, then inject
// WIPNOTE_SESSION_FAMILY_ID so the SessionStart hook can write the DB column.
// Also persist the launcher-side state file immediately (concrete write path
// that survives even when hooks are not configured).
func codexSessionFamilyEnv(env []string, opts codexLaunchOpts, otelSessionID, effectiveProjDir string) []string {
	if otelSessionID != "" && effectiveProjDir != "" {
		isResume := opts.ResumeID != "" || opts.ResumeLast
		// opts.ResumeID is the Codex-native session ID. It only matches a
		// wipnote family-index key in the rare case the two coincide; when it
		// does not, resolveSessionFamilyID falls through to the ordered
		// most-recent-session family (correct for "resume last").
		familyID := resolveSessionFamilyID(effectiveProjDir, otelSessionID, opts.ResumeID, isResume)
		env = launcher.SetEnv(env, "WIPNOTE_SESSION_FAMILY_ID", familyID)
		persistLauncherSessionFamily(effectiveProjDir, otelSessionID, "codex", familyID)
	}
	return env
}

var execCodexFn = execCodex

func buildCodexArgs(opts codexLaunchOpts, otelPort int, instructionArgs []string) []string {
	var args []string
	args = append(args, buildCodexOtelConfigArgs(otelPort)...)
	args = append(args, instructionArgs...)
	if opts.Yolo {
		args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		args = append(args, "--dangerously-bypass-hook-trust")
	} else if opts.SandboxMode != "" {
		args = append(args, "--sandbox", opts.SandboxMode)
	}
	for _, root := range opts.WritableRoots {
		if root != "" {
			args = append(args, "--add-dir", root)
		}
	}

	if opts.ResumeID != "" {
		args = append(args, "resume", opts.ResumeID)
	} else if opts.ResumeLast {
		args = append(args, "resume", "--last")
	}

	args = append(args, opts.ExtraArgs...)
	return args
}
