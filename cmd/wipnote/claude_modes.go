package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
	"github.com/shakestzd/wipnote/internal/launcher"
)

func launchClaudeDev(extraArgs []string, auto bool, resumeID, name, workItem string, inPlace, continue_ bool) error {
	if err := requireWipnoteOnPath(); err != nil {
		return err
	}

	// Resolve the source project root (the wipnote repo with plugin/).
	// This must happen BEFORE intent resolution so
	// resolveProjectPluginDirFrom can anchor to the source tree, not CWD.
	projectRoot := ""
	if wipnoteDir, err := findWipnoteDir(); err == nil {
		projectRoot = filepath.Dir(wipnoteDir)
	}

	// Resolve the in-tree plugin/ from the source root NOW, before intent
	// resolution might redirect childDir to a worktree. The plugin source always
	// lives in the wipnote source tree, never in a linked worktree.
	pluginDir, err := devLaunchPluginDir(projectRoot)
	if err != nil {
		return err
	}
	// Verify expected plugin structure.
	if _, err := os.Stat(filepath.Join(pluginDir, ".claude-plugin", "plugin.json")); os.IsNotExist(err) {
		return fmt.Errorf("plugin.json not found at %s",
			filepath.Join(pluginDir, ".claude-plugin", "plugin.json"))
	}

	// Resolve canonical root (non-empty only when projectRoot is a linked worktree).
	wipnoteRoot := canonicalProjectRoot(projectRoot)

	// Run the intent chooser and isolation planner — same path as launchClaudeDefault.
	// continue_ suppresses the chooser (explicit "resume most recent" intent) and is
	// honored below via Resume: true on the launch opts.
	lctx, err := resolveClaudeIntentIsolation(projectRoot, wipnoteRoot, resumeID, workItem, inPlace, continue_, extraArgs)
	if err != nil {
		return err
	}
	resumeID = lctx.intentResult.resumeID

	browse := lctx.intentResult.browse
	sessionName := resolveSessionName(name, resumeID, continue_ || browse, projectRoot, lctx.intentResult.workItem)

	devHeadline := "Launching Claude Code with local plugin (--plugin-dir mode)"
	if auto {
		devHeadline += " + auto mode"
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        devHeadline,
		PluginSource:    pluginDir,
		Session:         sessionName,
		Warning:         lctx.dirtyMainWarning,
		WarningSeverity: "amber",
	}))

	// Remove any installed marketplace plugin after intent and isolation resolution
	// succeeds so --plugin-dir cannot be shadowed by a stale install.
	removeMarketplaceWipnote()

	return launchClaude(LaunchOpts{
		// Mode is always "go" for dev sessions: it identifies the dev-plugin
		// launcher type (opts.PluginDir != "" && opts.Mode == "go") for
		// computeLauncherMode and writeLaunchMarker. The intent's continue/new
		// distinction is captured via ResumeID and Intent — not the mode string.
		Mode:      "go",
		PluginDir: pluginDir,
		// Resume the most recent session when --dev --continue was requested
		// (mirrors launchClaudeContinue). ResumeID, when set, still takes
		// precedence in launchClaude's arg construction.
		// "Browse all sessions" in the picker is the same bare --resume.
		Resume:             continue_ || browse,
		ResumeID:           resumeID,
		InjectSystemPrompt: true,
		PermissionMode:     autoPermissionMode(auto),
		Name:               sessionName,
		ExtraArgs:          extraArgs,
		// childDir is the worktree (or projectRoot when in-place). Claude Code
		// runs here so the agent works on the right branch, but WIPNOTE_PROJECT_DIR
		// still points at the canonical source root via WipnoteRoot.
		ProjectRoot: lctx.childDir,
		WipnoteRoot: lctx.wipnoteRoot,
		Intent:      lctx.intentResult.intent,
		ExtraEnv:    lctx.continueEnv,
		WorkItem:    lctx.intentResult.workItem,
	})
}

func requireWipnoteOnPath() error {
	if _, err := exec.LookPath("wipnote"); err != nil {
		return fmt.Errorf("wipnote binary not found on PATH\nBuild with: wipnote build")
	}
	return nil
}

// autoPermissionMode returns "auto" when enabled is true, otherwise empty string.
// This avoids passing --permission-mode when auto mode is not requested.
func autoPermissionMode(enabled bool) string {
	if enabled {
		return "auto"
	}
	return ""
}

// launchClaudeAuto launches Claude Code with auto mode enabled for autonomous operation.
// It uses the bundled plugin (like normal mode) but adds --permission-mode auto so
// Claude starts in autonomous operation immediately. (--enable-auto-mode is no
// longer passed: Claude Code 2.1.111 removed it — auto mode no longer requires it.)
func launchClaudeAuto(extraArgs []string, resumeID, name string) error {
	projectRoot, _ := resolveProjectRoot()
	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	wipnoteRoot := canonicalProjectRoot(projectRoot)
	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}
	sessionName := name
	// Only synthesize a default name for new sessions. When resuming an existing
	// session, skip default-name generation so we don't rename or conflict with
	// the resumed session. The user can still override with an explicit --name.
	if sessionName == "" && resumeID == "" {
		sessionName = defaultSessionName(projectRoot)
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        "Launching Claude Code in auto mode (autonomous operation)...",
		PluginSource:    pluginDir,
		Session:         sessionName,
		Warning:         "Actions will be approved by the background classifier, not prompted.",
		WarningSeverity: "amber",
	}))
	return launchClaude(LaunchOpts{
		Mode:               "auto",
		PluginDir:          pluginDir,
		ResumeID:           resumeID,
		InjectSystemPrompt: true,
		PermissionMode:     "auto",
		Name:               sessionName,
		ExtraArgs:          extraArgs,
		ProjectRoot:        projectRoot,
		WipnoteRoot:        wipnoteRoot,
	})
}

// installedPluginsJSON is the path to the Claude Code installed plugins registry.
func installedPluginsJSONPath() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".claude", "plugins", "installed_plugins.json")
}

func launchClaudeInit(extraArgs []string, resumeID, name string) error {
	// --init always uses CWD — never walk up to a parent with .wipnote/.
	// The user explicitly wants to work in THIS directory, which may not
	// have .wipnote/ yet. Walk-up would anchor to the wrong project.
	projectRoot, _ := os.Getwd()
	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	wipnoteRoot := canonicalProjectRoot(projectRoot)
	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}
	sessionName := name
	// Only synthesize a default name for new sessions. When resuming an existing
	// session, skip default-name generation so we don't rename or conflict with
	// the resumed session. The user can still override with an explicit --name.
	if sessionName == "" && resumeID == "" {
		sessionName = defaultSessionName(projectRoot)
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:     "Launching Claude Code with bundled wipnote plugin (init mode)...",
		PluginSource: pluginDir,
		Session:      sessionName,
	}))
	return launchClaude(LaunchOpts{
		Mode:               "init",
		PluginDir:          pluginDir,
		ResumeID:           resumeID,
		InjectSystemPrompt: true,
		Name:               sessionName,
		ExtraArgs:          extraArgs,
		ProjectRoot:        projectRoot,
		WipnoteRoot:        wipnoteRoot,
	})
}

func launchClaudeContinue(extraArgs []string, resumeID string) error {
	projectRoot, _ := resolveProjectRoot()
	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	wipnoteRoot := canonicalProjectRoot(projectRoot)
	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:     "Resuming last Claude Code session (continue mode)...",
		PluginSource: pluginDir,
	}))
	return launchClaude(LaunchOpts{
		Mode:        "continue",
		PluginDir:   pluginDir,
		Resume:      true,
		ResumeID:    resumeID,
		ExtraArgs:   extraArgs,
		ProjectRoot: projectRoot,
		WipnoteRoot: wipnoteRoot,
		Intent:      launcher.ContinueWorkIntent("", "claude", resumeID, "", true),
	})
}

func launchClaudeDefault(extraArgs []string, resumeID, name, workItem, workDir string, inPlace bool) error {
	projectRoot, err := claudeLaunchRoot(workDir)
	if err != nil {
		return err
	}

	// Resolve canonical main repo root when CWD is a linked worktree (slice-3).
	// canonicalProjectRoot returns "" for the main worktree (no override needed).
	wipnoteRoot := canonicalProjectRoot(projectRoot)

	lctx, err := resolveClaudeIntentIsolation(projectRoot, wipnoteRoot, resumeID, workItem, inPlace, false, extraArgs)
	if err != nil {
		return err
	}
	resumeID = lctx.intentResult.resumeID

	pluginDir, err := resolveBundledPluginDir()
	if err != nil {
		return err
	}
	sessionName := resolveSessionName(name, resumeID, lctx.intentResult.browse, projectRoot, lctx.intentResult.workItem)
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline:        fmt.Sprintf("Launching Claude Code (%s mode)...", lctx.intentResult.mode),
		PluginSource:    pluginDir,
		Session:         sessionName,
		Warning:         lctx.dirtyMainWarning,
		WarningSeverity: "amber",
	}))
	return launchClaude(LaunchOpts{
		Mode:      lctx.intentResult.mode,
		PluginDir: pluginDir,
		// "Browse all sessions" opens Claude's own picker: a bare --resume.
		Resume:             lctx.intentResult.browse,
		ResumeID:           resumeID,
		InjectSystemPrompt: true,
		Name:               sessionName,
		ExtraArgs:          extraArgs,
		ProjectRoot:        lctx.childDir,
		WipnoteRoot:        lctx.wipnoteRoot,
		Intent:             lctx.intentResult.intent,
		ExtraEnv:           lctx.continueEnv,
		WorkItem:           lctx.intentResult.workItem,
	})
}

// claudeLaunchRoot is the dir a default launch runs claude in: --work-dir
// when given (a delegate worker runs where its caller ran, a linked
// worktree too), else the project root.
func claudeLaunchRoot(workDir string) (string, error) {
	if workDir == "" {
		root, _ := resolveProjectRoot()
		return root, nil
	}
	abs, err := filepath.Abs(workDir)
	if err != nil {
		return "", err
	}
	if fi, err := os.Stat(abs); err != nil || !fi.IsDir() {
		return "", fmt.Errorf("claude --work-dir %s: not a directory", workDir)
	}
	return abs, nil
}
