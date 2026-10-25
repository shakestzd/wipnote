package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
)

// printCodexSetupSummary renders a summary banner for runCodexInit (--init mode).
// In-launch setup details are folded into the launch banner by prepareCodexMarketplace.
func printCodexSetupSummary(details []launchtui.BannerDetail) {
	if len(details) == 0 {
		return
	}
	fmt.Println(launchtui.RenderLaunchBanner(nil, launchtui.BannerInput{
		Headline: "Codex wipnote setup",
		Details:  details,
	}))
}

// runCodexInit installs the wipnote Codex marketplace plugin, idempotently.
// Corresponds to: wipnote codex --init
// Phase 1: Install / verify marketplace (idempotent).
// Phase 2: Warn (never rewrite) if the user explicitly disabled Codex hooks.
//
// `yes` no longer drives any prompt here (the hooks-flag prompt was the only
// one), but the parameter and the --yes flag are kept so existing scripts
// that pass `wipnote codex --init --yes` keep working.
//
// Phase B of the marketplace-to-bundled-plugin migration: --init now points
// Codex at the bundled local marketplace tree resolved via
// resolveSharedTreePath("codex-marketplace") instead of cloning the marketplace
// from GitHub. The Codex plugin tree is shipped alongside the wipnote binary
// (in the release tarball or via brew install) and mirrored by `wipnote build`.
func runCodexInit(yes, dryRun bool) error {
	configPath := codexConfigPath()

	setup, err := codexInitMarketplace(configPath, dryRun)
	if err != nil {
		return err
	}
	setup = append(setup, codexInitHooksFlag(configPath)...)
	setup = append(setup, codexInitPlugin(configPath, dryRun)...)
	setup = append(setup, codexInitPluginCache(configPath, dryRun)...)
	setup = append(setup, codexInitMirroredHooks(dryRun)...)
	setup = append(setup, codexInitAgents(dryRun)...)

	printCodexSetupSummary(setup)
	fmt.Println()
	fmt.Println("Setup complete. Run: wipnote codex")
	return nil
}

// codexInitMarketplace registers the bundled marketplace (phase 1).
func codexInitMarketplace(configPath string, dryRun bool) ([]launchtui.BannerDetail, error) {
	var setup []launchtui.BannerDetail
	bundledMarketplace, bundleErr := resolveSharedTreePath("codex-marketplace")
	if bundleErr != nil {
		return nil, fmt.Errorf("resolving bundled Codex marketplace: %w", bundleErr)
	}
	// Codex CLI requires a marketplace ROOT DIRECTORY (layout
	// <root>/.agents/plugins/marketplace.json), NOT the manifest file path —
	// passing the file fails with "local marketplace source must be a
	// directory, not a file". Register the directory and compare detection
	// against the SAME directory value to avoid re-registering every launch.
	bundledDir := codexMarketplaceAddArg(bundledMarketplace)

	// Phase 1: Install or verify marketplace. If a different marketplace is
	// already registered (e.g. from a previous GitHub-clone-based --init), we
	// rewrite the registration to point at the bundled local path.
	registeredPath := getCodexMarketplacePathAt(configPath)
	registeredAbs, _ := filepath.Abs(registeredPath)
	bundledAbs, _ := filepath.Abs(bundledDir)

	if registeredAbs != "" && registeredAbs != bundledAbs {
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "would replace existing registration (" + registeredPath + ")"})
			setup = append(setup, launchtui.BannerDetail{Label: "Registration", Value: "[dry-run] would remove wipnote registrations from " + configPath})
		} else if _, rmErr := removeCodexWipnoteRegistrations(configPath); rmErr != nil {
			return nil, fmt.Errorf("removing stale Codex marketplace registration: %w", rmErr)
		}
		registeredPath = ""
		registeredAbs = ""
	}

	if registeredAbs != bundledAbs {
		addArgs := []string{"plugin", "marketplace", "add", bundledDir}
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "[dry-run] codex " + strings.Join(addArgs, " ")})
		} else {
			if out, err := exec.Command("codex", addArgs...).CombinedOutput(); err != nil {
				return nil, fmt.Errorf("codex marketplace add failed: %w\n%s", err, strings.TrimSpace(string(out)))
			}
			setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "installed (bundled): " + bundledDir})
		}
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "already installed (bundled)"})
	}
	return setup, nil
}

// codexInitHooksFlag reports an explicit hooks opt-out (phase 2).
func codexInitHooksFlag(configPath string) []launchtui.BannerDetail {
	var setup []launchtui.BannerDetail
	// Phase 2: hooks are on by default in Codex, so --init no longer writes
	// [features] hooks = true; it only reports an explicit user opt-out.
	if isCodexHooksExplicitlyDisabledAt(configPath) {
		warnIfCodexHooksDisabled(configPath)
		setup = append(setup, launchtui.BannerDetail{Label: "Hooks flag", Value: "disabled by user config (not changed)"})
	}
	return setup
}

// codexInitPlugin enables the wipnote plugin (phase 3).
func codexInitPlugin(configPath string, dryRun bool) []launchtui.BannerDetail {
	var setup []launchtui.BannerDetail
	// Phase 3: enable the actual plugin. Without this, the marketplace is
	// registered but skills/commands are not loaded in Codex sessions.
	if !isCodexPluginEnabledAt(configPath) {
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Plugin", Value: "[dry-run] would enable plugin wipnote@wipnote in ~/.codex/config.toml"})
		} else {
			if err := ensureCodexPluginEnabled(configPath); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not enable wipnote plugin: %v\n", err)
			} else {
				setup = append(setup, launchtui.BannerDetail{Label: "Plugin", Value: "enabled"})
			}
		}
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Plugin", Value: "already enabled"})
	}
	return setup
}

// codexInitPluginCache installs the plugin tree behind the enabled stanza
// (phase 4).
func codexInitPluginCache(configPath string, dryRun bool) []launchtui.BannerDetail {
	var setup []launchtui.BannerDetail
	// Phase 4: ensure Codex has an installed plugin tree behind the enabled
	// stanza. Git marketplaces are installed via Codex's upgrade command; local
	// dev marketplaces are materialized directly into Codex's cache.
	if !isCodexPluginInstalledAt(codexPluginCachePath()) {
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "[dry-run] would install plugin wipnote@wipnote into Codex plugin cache"})
		} else if installed, err := ensureCodexLocalPluginInstalled(configPath, false); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not install local wipnote plugin cache: %v\n", err)
		} else if installed {
			setup = append(setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "installed in local cache"})
		} else if out, err := exec.Command("codex", "plugin", "marketplace", "upgrade", "wipnote").CombinedOutput(); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not install wipnote plugin cache from marketplace: %v\n%s\n", err, strings.TrimSpace(string(out)))
		} else {
			setup = append(setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "installed"})
		}
	}
	return setup
}

// codexInitMirroredHooks removes legacy mirrored hooks from ~/.codex/hooks.json.
func codexInitMirroredHooks(dryRun bool) []launchtui.BannerDetail {
	var setup []launchtui.BannerDetail
	if dryRun {
		setup = append(setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "[dry-run] would remove mirrored wipnote hooks from ~/.codex/hooks.json"})
	} else if changed, err := pruneCodexGlobalHooksFromCache(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove mirrored wipnote Codex hooks: %v\n", err)
	} else if changed {
		setup = append(setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "removed from ~/.codex/hooks.json"})
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "none found in ~/.codex/hooks.json"})
	}
	return setup
}

// codexInitAgents installs the wipnote Codex agents into ~/.codex/agents.
func codexInitAgents(dryRun bool) []launchtui.BannerDetail {
	var setup []launchtui.BannerDetail
	if dryRun {
		setup = append(setup, launchtui.BannerDetail{Label: "Agents", Value: "[dry-run] would install wipnote Codex agents into ~/.codex/agents"})
	} else if changed, err := ensureCodexAgentsFromCache(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not install wipnote Codex agents: %v\n", err)
	} else if changed {
		setup = append(setup, launchtui.BannerDetail{Label: "Agents", Value: "installed in ~/.codex/agents"})
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Agents", Value: "already installed"})
	}
	return setup
}
