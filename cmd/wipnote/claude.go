package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/shakestzd/wipnote/internal/launcher"
	"github.com/spf13/cobra"
)

// LaunchOpts controls how Claude Code is launched.
type LaunchOpts struct {
	// Mode is written to the launch marker (e.g. "go", "init", "continue", "default").
	Mode string
	// PluginDir, if non-empty, passes --plugin-dir to claude.
	PluginDir string
	// ModsDir, if non-empty, passes a second --plugin-dir: the folder of
	// wipnote's Claude Code mods. launchClaude fills it from PluginDir
	// (resolveModsDir) when left empty.
	ModsDir string
	// Resume adds --resume to claude args (for --continue mode).
	Resume bool
	// ResumeID, if non-empty, passes --resume <id> to claude to resume a specific session.
	// Takes precedence over Resume.
	ResumeID string
	// InjectSystemPrompt, when true, appends the embedded system prompt via
	// --append-system-prompt-file. Ignored when SystemPromptFile is set.
	InjectSystemPrompt bool
	// SystemPromptFile, if set, reads this file and appends it as system prompt.
	// Takes precedence over InjectSystemPrompt.
	SystemPromptFile string
	// PermissionMode, if set, passes --permission-mode to claude (e.g. "bypassPermissions").
	PermissionMode string
	// Name, if set, passes --name to claude for session naming.
	Name string
	// ExtraArgs are forwarded to the claude process.
	ExtraArgs []string
	// ProjectRoot is the absolute path to the project root (directory containing .wipnote/).
	// When set, Claude Code is started with this as the working directory, and path-sensitive
	// helpers (writeLaunchMarker, etc.) anchor their paths here instead of CWD.
	ProjectRoot string
	// WipnoteRoot, if set, is the main repo root containing the canonical .wipnote/.
	// Used when ProjectRoot is a worktree — all work item tracking resolves to this path
	// instead of the worktree copy. Injected as WIPNOTE_PROJECT_DIR env var.
	WipnoteRoot string
	// Intent records whether the launcher is starting new work or continuing
	// existing work. Harness-neutral contract for downstream launchers.
	Intent launcher.LaunchIntent
	// ExtraEnv is layered onto the child environment after the launcher builds
	// its standard wipnote + telemetry overrides.
	ExtraEnv []string
	// WorkItem is the launch's work item; inside herdr the launcher labels
	// the caller's pane "<id>: <title>" with it, in the background.
	WorkItem string
}

func claudeCmd() *cobra.Command {
	var dev, init_, continue_, auto, tmux, allowNonInteractive bool
	var resumeID, name string
	// Isolation flags (slice-2). --no-worktree and --in-place are mutually equivalent;
	// --in-place is the preferred semantic name going forward.
	var noWorktree, inPlace bool
	var workItem, baseBranch string

	cmd := &cobra.Command{
		Use:   "claude",
		Short: "Launch Claude Code with wipnote",
		RunE: func(cmd *cobra.Command, args []string) error {
			// Tmux wrap must happen before any side-effecting work.
			// When --tmux is set and we are not already inside tmux, this
			// replaces the current process with: tmux new-session -A -s wipnote-dev -- <argv without --tmux>
			// and never returns. If tmux is missing, an error is returned.
			// If we are already inside tmux (TMUX env set), this is a no-op.
			_ = tmux // flag is consumed via os.Args inspection in maybeTmuxWrap
			if err := maybeTmuxWrap("wipnote-dev"); err != nil {
				return err
			}
			// Every mode below launches an interactive Claude TUI. Refuse when
			// stdin is not a terminal BEFORE any launch marker, serve/collector
			// spawn, worktree or session write happens (issue #148).
			if err := requireInteractiveLaunch(harnessClaude, allowNonInteractive, args); err != nil {
				return err
			}
			// --no-worktree is a legacy alias for --in-place.
			effectiveInPlace := inPlace || noWorktree
			_ = baseBranch // reserved for slice-3+; accepted but not yet acted on
			switch {
			case dev:
				return launchClaudeDev(args, auto, resumeID, name, workItem, effectiveInPlace, continue_)
			case auto:
				return launchClaudeAuto(args, resumeID, name)
			case init_:
				return launchClaudeInit(args, resumeID, name)
			case continue_:
				return launchClaudeContinue(args, resumeID)
			default:
				return launchClaudeDefault(args, resumeID, name, workItem, effectiveInPlace)
			}
		},
	}
	cmd.Flags().BoolVar(&dev, "dev", false, "Launch with local Go plugin for development")
	cmd.Flags().BoolVar(&auto, "auto", false, "Launch with auto mode enabled (autonomous operation)")
	cmd.Flags().BoolVar(&init_, "init", false, "Launch with marketplace plugin installation")
	cmd.Flags().BoolVar(&continue_, "continue", false, "Resume last session with marketplace plugin")
	cmd.Flags().BoolVar(&tmux, "tmux", false, "Wrap in a tmux session named 'wipnote-dev' (survives disconnects; reattaches on re-run)")
	cmd.Flags().BoolVar(&noWorktree, "no-worktree", false, "Skip worktree creation; run in project root (alias for --in-place)")
	cmd.Flags().BoolVar(&inPlace, "in-place", false, "Intentional in-place mutation; preserves existing behavior, records opt-out of isolation")
	cmd.Flags().StringVar(&resumeID, "resume", "", "Resume a specific Claude Code session by ID")
	cmd.Flags().StringVar(&name, "name", "", "Session label shown in Claude TUI (default: <project>-<timestamp>)")
	cmd.Flags().StringVar(&workItem, "work-item", "", "Work item ID for isolation planning (e.g. feat-15c458aa, trk-3719d8f3)")
	cmd.Flags().StringVar(&baseBranch, "base", "", "Base branch for managed worktree (advanced; default: current HEAD)")
	cmd.Flags().BoolVar(&allowNonInteractive, allowNonInteractiveFlag, false,
		"Launch even when stdin is not a terminal (default: refuse; env "+allowNonTTYEnv+"=1 is equivalent)")
	cmd.AddCommand(yoloCmd())
	return cmd
}

// resolveSessionName returns the session name to pass to Claude Code.
//
// Rules (in priority order):
//  1. An explicitly provided sessionName is always kept as-is.
//  2. When continue_ is true the caller is resuming the most-recent session via
//     --continue; no synthesized name is emitted so it doesn't conflict with the
//     implicit resume.
//  3. When resumeID is non-empty the caller is resuming a specific session by ID;
//     synthesizing a name would rename or conflict with the existing session.
//  4. Otherwise (fresh launch) a default name is synthesized via defaultSessionName.
func resolveSessionName(sessionName, resumeID string, continue_ bool, projectRoot string) string {
	if sessionName != "" {
		return sessionName
	}
	if continue_ || resumeID != "" {
		return ""
	}
	return defaultSessionName(projectRoot)
}

// defaultSessionName builds a default session label: <project-slug>-<timestamp>.
// projectRoot may be empty, in which case the label is just the timestamp.
func defaultSessionName(projectRoot string) string {
	ts := time.Now().Format("20060102-15.04.05")
	if projectRoot == "" {
		return ts
	}
	projectSlug := slug.Make(filepath.Base(projectRoot), 30)
	if projectSlug == "" {
		return ts
	}
	return projectSlug + "-" + ts
}

// marketplaceArtifactDirs returns all artifact directories that may be removed
// by removeMarketplaceWipnote (wipnote and legacy htmlgraph scopes).
func marketplaceArtifactDirs() []string {
	home, _ := os.UserHomeDir()
	return []string{
		filepath.Join(home, ".claude", "plugins", "marketplaces", "wipnote"),
		filepath.Join(home, ".claude", "plugins", "cache", "wipnote"),
		filepath.Join(home, ".claude", "plugins", "cache", "local-marketplace", "wipnote"),
		filepath.Join(home, ".claude", "plugins", "marketplaces", "htmlgraph"),
		filepath.Join(home, ".claude", "plugins", "cache", "htmlgraph"),
		filepath.Join(home, ".claude", "plugins", "cache", "local-marketplace", "htmlgraph"),
	}
}

// marketplaceWipnotePresent reports whether any marketplace wipnote artifact
// exists that would need removing before a dev-mode launch. It checks:
//   - isAnyMarketplacePluginInstalledAt() — all registry scopes that
//     removeMarketplaceWipnote handles (wipnote@wipnote, wipnote@local-marketplace,
//     htmlgraph@htmlgraph, htmlgraph@local-marketplace)
//   - all artifact directories that removeMarketplaceWipnote would remove
//
// When this returns false, removeMarketplaceWipnote can skip all subprocess
// calls immediately. Pure-ish (reads files + os.Stat) so it is unit-testable.
func marketplaceWipnotePresent() bool {
	if isAnyMarketplacePluginInstalledAt(installedPluginsJSONPath()) {
		return true
	}
	for _, dir := range marketplaceArtifactDirs() {
		if _, err := os.Stat(dir); err == nil {
			return true
		}
	}
	return false
}

// removeMarketplaceWipnote fully removes the wipnote marketplace plugin so it
// cannot shadow --plugin-dir agents/skills during dev mode. Belt-and-braces:
// uninstall removes the install record, disable flips the enabled flag, and
// RemoveAll wipes any cloned/cached files that linger even after uninstall.
//
// Fast-path: when marketplaceWipnotePresent() is false all subprocess calls
// are skipped entirely (~4s saved on every --dev launch when never installed).
func removeMarketplaceWipnote() {
	if !marketplaceWipnotePresent() {
		return
	}
	fmt.Println("Removing marketplace wipnote plugin for dev mode...")
	// Legacy htmlgraph scopes are still removed so old marketplace installs
	// cannot shadow local wipnote dev plugins.
	scopes := []string{"wipnote@wipnote", "wipnote@local-marketplace", "htmlgraph@htmlgraph", "htmlgraph@local-marketplace"}
	total := len(scopes) * 2 // uninstall + disable per scope
	step := 0
	for _, scope := range scopes {
		step++
		fmt.Fprintf(os.Stderr, "wipnote: cleaning legacy marketplace plugin (%d/%d)...\r", step, total)
		if out, err := exec.Command("claude", "plugin", "uninstall", scope).CombinedOutput(); err != nil {
			msg := strings.ToLower(strings.TrimSpace(string(out)))
			if !strings.Contains(msg, "not found") && !strings.Contains(msg, "not installed") && !strings.Contains(msg, "already uninstalled") {
				fmt.Fprintf(os.Stdout, "warning: plugin uninstall %s: %v (%s)\n", scope, err, strings.TrimSpace(string(out)))
			}
		}
		step++
		fmt.Fprintf(os.Stderr, "wipnote: cleaning legacy marketplace plugin (%d/%d)...\r", step, total)
		if out, err := exec.Command("claude", "plugin", "disable", scope).CombinedOutput(); err != nil {
			msg := strings.ToLower(strings.TrimSpace(string(out)))
			if !strings.Contains(msg, "not found") && !strings.Contains(msg, "not installed") && !strings.Contains(msg, "already disabled") {
				fmt.Fprintf(os.Stdout, "warning: plugin disable %s: %v (%s)\n", scope, err, strings.TrimSpace(string(out)))
			}
		}
	}
	fmt.Fprintln(os.Stderr) // terminate the \r line
	for _, dir := range marketplaceArtifactDirs() {
		if err := os.RemoveAll(dir); err != nil {
			fmt.Fprintf(os.Stdout, "warning: could not remove %s: %v\n", dir, err)
		}
	}
	fmt.Println("Marketplace wipnote removed (uninstalled, disabled, cache wiped).")
}

// resolveBundledPluginDir resolves the path to the bundled wipnote Claude
// plugin tree installed by `wipnote build` or `brew install wipnote`. It
// is the Phase-B replacement for relying on the marketplace plugin install.
// Dev mode (--dev) bypasses this and uses the in-tree plugin/ directly.
func resolveBundledPluginDir() (string, error) {
	return resolveSharedTreePath("plugin")
}

const wipnoteMarketplaceRepo = "shakestzd/wipnote"

// launchClaude is the shared launcher used by all modes.
func launchClaude(opts LaunchOpts) error {
	// Compute launcher mode for preflight logging/inspection (no behavior change).
	// devPlugin is true when a PluginDir was supplied in go/dev mode.
	_ = computeLauncherMode(opts.WipnoteRoot, opts.PluginDir != "" && opts.Mode == "go", false)

	// Write launch marker to the main project root, not the worktree.
	markerRoot := opts.ProjectRoot
	if opts.WipnoteRoot != "" {
		markerRoot = opts.WipnoteRoot
	}
	writeLaunchMarker(opts.Mode, markerRoot)
	launchHerdrLabel(markerRoot, opts.WorkItem)

	// Start the work-item daemon ensure now and collect it just before exec,
	// so its startup overlaps the rest of the launch (bug-06d2c91d).
	daemonEnsure := startDaemonEnsure(markerRoot)

	// SystemPromptFile takes precedence over InjectSystemPrompt.
	var systemPrompt string
	if opts.SystemPromptFile != "" {
		if data, err := os.ReadFile(opts.SystemPromptFile); err == nil {
			systemPrompt = string(data)
		}
	} else if opts.InjectSystemPrompt {
		systemPrompt = systemPromptContent
	}
	// Append the shared research-routing disposition exactly once. Source of
	// truth: cmd/wipnote/prompts/research-routing.md (mirrored for subagents in
	// the agent-context skill).
	if systemPrompt != "" {
		systemPrompt = strings.TrimSpace(systemPrompt) + "\n\n" + strings.TrimSpace(researchRoutingContent) + "\n"
	}
	if opts.ModsDir == "" {
		opts.ModsDir = resolveModsDir(opts.PluginDir)
	}
	if rules := modsSystemPrompt(opts.ModsDir); rules != "" {
		systemPrompt = strings.TrimSpace(systemPrompt+"\n\n"+rules) + "\n"
	}

	// Pass the prompt by file, not argv: large prompts fail as a single argv
	// element, and Claude Code's guidance for wrappers is
	// --append-system-prompt-file. The file is private (os.CreateTemp is 0600)
	// and removed once claude exits — via the deferred Remove on early returns
	// and via harnessCleanup, which runHarnessWithCleanup runs even on the
	// os.Exit / signal re-raise paths where defers are skipped.
	var systemPromptFile string
	if systemPrompt != "" {
		path, err := writeClaudeSystemPromptFile(systemPrompt)
		if err != nil {
			return err
		}
		systemPromptFile = path
		defer os.Remove(path) //nolint:errcheck
	}
	claudeArgs := buildClaudeArgs(opts, systemPromptFile)

	claudePath, err := exec.LookPath("claude")
	if err != nil {
		return fmt.Errorf("claude not found in PATH: %w", err)
	}

	// Show the one-time OTel first-launch notice before starting the
	// server so users see the explanation before any server output.
	MaybeShowOtelNotice(opts.ProjectRoot)

	launchTiming("launchClaude: before ensureGuardProfile")
	// Launch-time guard-profile initialization (peer to the OTel collector
	// bootstrap). No-op when already approved or non-interactive; never blocks.
	ensureGuardProfile(opts.ProjectRoot)
	launchTiming("launchClaude: after ensureGuardProfile")

	// Generate a per-session ID and spawn a per-session OTel collector.
	// The collector writes NDJSON to .wipnote/sessions/<sid>/ and
	// exposes an ephemeral OTLP HTTP port. Non-fatal: on failure, the
	// existing serve-based receiver is used as fallback.
	// On a TTY the spawn is wrapped in an animated spinner so the user sees
	// live feedback during the handshake wait instead of a frozen static line
	// (feat-caa02f9a Fix 4b). On non-TTY paths the static line is preserved.
	var envOverrides otelEnvOverrides
	if opts.ProjectRoot != "" && !isExplicitlyDisabled(os.Getenv("WIPNOTE_OTEL_ENABLED")) {
		const collectorLabel = "starting session telemetry collector"
		projectRootCopy := opts.ProjectRoot
		_ = launchtui.RunWithSpinner(os.Stderr, collectorLabel, func(_ io.Writer) error {
			envOverrides = spawnSessionCollector(projectRootCopy)
			return nil
		})
		if envOverrides.Cleanup != nil {
			defer envOverrides.Cleanup()
		}
	}
	launchTiming("launchClaude: after spawnSessionCollector (before exec)")

	c := exec.Command(claudePath, claudeArgs...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr

	// Compose the child env: start from os.Environ, layer
	// WIPNOTE_PROJECT_DIR when running in a worktree, and layer OTel
	// exporter vars when WIPNOTE_OTEL_ENABLED=1 so Claude's OTLP
	// pipeline points at the per-session collector. See
	// claude_env.go:buildClaudeLaunchEnv for precedence rules (user-set
	// OTEL_* always wins).
	c.Env = buildClaudeLaunchEnv(claudeEnvProjectDir(opts), &envOverrides)
	c.Env = launcher.AppendOrReplaceEnv(c.Env, opts.ExtraEnv...)

	// Publish the work-item daemon's socket into the session (feat-f6759e37)
	// if the ensure started above is done. markerRoot is the same root the
	// launch marker is written to — the main project root even when running
	// in a worktree — so the daemon, the lease and the marker all agree on
	// which project this is. A daemon still starting is not waited for
	// (bug-06d2c91d): the session then runs the unguaranteed contract.
	c.Env = launcher.AppendOrReplaceEnv(c.Env, daemonEnsure.sessionEnv(os.Stderr, daemonJoinBudget)...)

	// Set working directory to project root so Claude starts in the right place,
	// even if this command is run from a subdirectory like packages/go.
	if opts.ProjectRoot != "" {
		c.Dir = opts.ProjectRoot
	}

	harnessCleanup := func() {
		if systemPromptFile != "" {
			os.Remove(systemPromptFile) //nolint:errcheck
		}
		if envOverrides.Cleanup != nil {
			envOverrides.Cleanup()
		}
	}
	return runHarnessWithCleanup(c, harnessCleanup)
}

// writeClaudeSystemPromptFile writes prompt to a private temp file (0600, via
// os.CreateTemp) and returns its path for --append-system-prompt-file. The
// caller removes it after claude exits.
func writeClaudeSystemPromptFile(prompt string) (string, error) {
	f, err := os.CreateTemp("", "wipnote-claude-system-*.md")
	if err != nil {
		return "", fmt.Errorf("creating system prompt file: %w", err)
	}
	if _, err := f.WriteString(prompt); err != nil {
		f.Close()
		os.Remove(f.Name()) //nolint:errcheck
		return "", fmt.Errorf("writing system prompt file: %w", err)
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name()) //nolint:errcheck
		return "", fmt.Errorf("closing system prompt file: %w", err)
	}
	return f.Name(), nil
}

// buildClaudeArgs assembles the claude argv from opts. Split out of
// launchClaude so the argument shape is unit-testable without spawning claude.
// systemPromptFile, when non-empty, is the path of the already-written prompt
// file passed via --append-system-prompt-file.
func buildClaudeArgs(opts LaunchOpts, systemPromptFile string) []string {
	var claudeArgs []string
	if opts.ResumeID != "" {
		claudeArgs = append(claudeArgs, "--resume", opts.ResumeID)
	} else if opts.Resume {
		claudeArgs = append(claudeArgs, "--resume")
	}
	if opts.PluginDir != "" {
		claudeArgs = append(claudeArgs, "--plugin-dir", opts.PluginDir)
	}
	if opts.ModsDir != "" {
		claudeArgs = append(claudeArgs, "--plugin-dir", opts.ModsDir)
	}
	// Auto mode is selected by --permission-mode auto alone; the former
	// --enable-auto-mode opt-in flag was removed upstream in Claude Code 2.1.111.
	if opts.PermissionMode != "" {
		claudeArgs = append(claudeArgs, "--permission-mode", opts.PermissionMode)
	}
	if opts.Name != "" {
		claudeArgs = append(claudeArgs, "--name", opts.Name)
	}
	if systemPromptFile != "" {
		claudeArgs = append(claudeArgs, "--append-system-prompt-file", systemPromptFile)
	}
	return append(claudeArgs, opts.ExtraArgs...)
}

// claudeEnvProjectDir is the WIPNOTE_PROJECT_DIR exported to the Claude
// process: the canonical root when Claude runs in a worktree, else the root
// Claude is started in (c.Dir). It is always explicit — never left to
// effectiveProjectDir's os.Getwd fallback. Launched from a linked worktree,
// project resolution yields the MAIN checkout (git-common-dir), so Claude runs
// there; falling back to the launcher's cwd exported the worktree instead, and
// every hook then read the worktree's .wipnote/ while Claude worked in the main
// checkout (issue #202).
func claudeEnvProjectDir(opts LaunchOpts) string {
	if opts.WipnoteRoot != "" {
		return opts.WipnoteRoot
	}
	return opts.ProjectRoot
}

// Harness identifiers for the WIPNOTE_HARNESS env var (feat-9348de66 slice 1).
// These are the closed enum the SessionStart hook accepts; values outside it
// are rejected at the write path. Each launcher stamps its own constant:
// claude.go uses harnessClaude here; codex/gemini/antigravity launchers wire
// their own in later slices.
const (
	harnessEnvKey      = "WIPNOTE_HARNESS"
	harnessClaude      = "claude"
	harnessCodex       = "codex"
	harnessGemini      = "gemini"
	harnessAntigravity = "antigravity"
)

// withHarnessEnv returns env with WIPNOTE_HARNESS set to harness, replacing any
// inherited value (launcher-authoritative). Shared across launchers so each
// harness stamps a single source of truth. Reuses setOrReplaceEnv (claude_env.go).
func withHarnessEnv(env []string, harness string) []string {
	return launcher.SetEnv(withRootSessionEnv(env), harnessEnvKey, harness)
}

// writeLaunchMarker writes .wipnote/.launch-mode for hooks to detect the launch mode.
// projectRoot must be non-empty; if it is empty the write is skipped to avoid
// polluting whatever directory the user happens to be in.
func writeLaunchMarker(mode, projectRoot string) {
	if projectRoot == "" {
		return // No project root — skip rather than polluting CWD
	}
	marker := map[string]any{
		"mode":      mode,
		"pid":       os.Getpid(),
		"timestamp": time.Now().UTC().Format(time.RFC3339),
	}
	data, err := json.Marshal(marker)
	if err != nil {
		return
	}
	dir := filepath.Join(projectRoot, ".wipnote")
	os.MkdirAll(dir, 0755)                                       //nolint:errcheck
	os.WriteFile(filepath.Join(dir, ".launch-mode"), data, 0644) //nolint:errcheck
}
