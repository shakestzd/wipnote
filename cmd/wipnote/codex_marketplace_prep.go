package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/cmd/wipnote/launchtui"
)

// prepareCodexMarketplace runs pre-launch marketplace setup and returns any
// noteworthy setup actions as BannerDetail rows for the caller to fold into
// the single launch banner (rather than printing separate setup/launch boxes).
func prepareCodexMarketplace(configPath string, source codexMarketplaceSource, dryRun bool) ([]launchtui.BannerDetail, error) {
	switch source {
	case codexMarketplaceSourceDev:
		return prepareCodexDevMarketplace(configPath, dryRun)
	default:
		return prepareCodexBundledMarketplace(configPath)
	}
}

// prepareCodexBundledMarketplace sets up the bundled marketplace and returns
// any noteworthy actions as BannerDetail rows (no banner rendered here).
func prepareCodexBundledMarketplace(configPath string) ([]launchtui.BannerDetail, error) {
	var setup []launchtui.BannerDetail
	if !isCodexMarketplaceInstalledAt(configPath) {
		bundled, err := resolveSharedTreePath("codex-marketplace")
		if err != nil {
			return nil, fmt.Errorf("resolving bundled Codex marketplace: %w", err)
		}
		bundledDir := codexMarketplaceAddArg(bundled)
		if out, addErr := exec.Command("codex", "plugin", "marketplace", "add", bundledDir).CombinedOutput(); addErr != nil {
			outStr := strings.TrimSpace(string(out))
			return nil, fmt.Errorf("WIPNOTE AGENTS NOT LOADED\n─────────────────────────\nFailed to register the wipnote marketplace with Codex CLI:\n  %v\n\nThe Codex session will run WITHOUT wipnote agents (researcher, feature-coder, etc.).\n\nTry:\n  - Run `wipnote codex --init` manually to retry the setup\n  - Check ~/.codex/config.toml for a stale marketplace entry under [plugins.\"wipnote@wipnote\"]\n  - Report this at https://github.com/shakestzd/wipnote/issues\n\nOutput:\n%s", addErr, outStr)
		}
		setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "registered (bundled): " + bundledDir})
	}
	// Hooks are on by default in Codex; never rewrite config.toml for them,
	// only warn when the user explicitly turned them off.
	warnIfCodexHooksDisabled(configPath)
	if !isCodexPluginEnabledAt(configPath) {
		if err := ensureCodexPluginEnabled(configPath); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not enable wipnote Codex plugin: %v\n", err)
		} else {
			setup = append(setup, launchtui.BannerDetail{Label: "Plugin", Value: "enabled"})
		}
	}
	if err := refreshCodexPluginCacheBestEffort(configPath, &setup); err != nil {
		return nil, err
	}
	return setup, nil
}

func refreshCodexPluginCacheBestEffort(configPath string, setup *[]launchtui.BannerDetail) error {
	if !isCodexPluginInstalledAt(codexPluginCachePath()) {
		if installed, err := ensureCodexLocalPluginInstalled(configPath, false); err != nil {
			fmt.Fprintf(os.Stderr, "warning: could not install local wipnote Codex plugin cache: %v\n", err)
		} else if installed {
			*setup = append(*setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "installed locally"})
		}
	}
	if changed, err := pruneCodexGlobalHooksFromCache(); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove mirrored wipnote Codex hooks: %v\n", err)
	} else if changed {
		*setup = append(*setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "removed from ~/.codex/hooks.json"})
	} else {
		*setup = append(*setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "none found in ~/.codex/hooks.json"})
	}
	pluginDir := codexInstalledPluginDirAt(codexPluginCachePath())
	if changed, err := ensureCodexCustomAgentsInstalled(pluginDir, codexAgentsPath()); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not install wipnote Codex agents: %v\n", err)
	} else if changed {
		*setup = append(*setup, launchtui.BannerDetail{Label: "User agents", Value: "installed in ~/.codex/agents"})
	}
	// No project-level copy: Codex already discovers ~/.codex/agents for
	// every project. Clean up copies left by older wipnote versions so they
	// cannot shadow the user-level agents (see removeCodexProjectAgentCopies).
	projectRoot, _ := resolveProjectRoot()
	if changed, err := removeCodexProjectAgentCopies(projectRoot); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not remove legacy project wipnote Codex agents: %v\n", err)
	} else if changed {
		*setup = append(*setup, launchtui.BannerDetail{Label: "Project agents", Value: "removed legacy copies from .codex/agents"})
	}
	return nil
}

// prepareCodexDevMarketplace sets up the local dev marketplace and returns
// noteworthy actions as BannerDetail rows for the caller to fold into the
// single launch banner (no separate "Preparing..." banner rendered here).
func prepareCodexDevMarketplace(configPath string, dryRun bool) ([]launchtui.BannerDetail, error) {
	localMarketplace, err := resolveLocalCodexMarketplace()
	if err != nil {
		return nil, err
	}
	var setup []launchtui.BannerDetail
	setup = append(setup, launchtui.BannerDetail{Label: "Local marketplace", Value: localMarketplace})
	regDetails, err := registerCodexDevMarketplace(configPath, localMarketplace, dryRun)
	if err != nil {
		return nil, err
	}
	setup = append(setup, regDetails...)
	// Hooks are on by default in Codex; never rewrite config.toml for them,
	// only warn when the user explicitly turned them off.
	warnIfCodexHooksDisabled(configPath)
	if !dryRun && !isCodexPluginEnabledAt(configPath) {
		if err := ensureCodexPluginEnabled(configPath); err != nil {
			return nil, fmt.Errorf("enabling local wipnote plugin in %s: %w", configPath, err)
		}
		setup = append(setup, launchtui.BannerDetail{Label: "Plugin", Value: "enabled (local)"})
	}
	if dryRun {
		setup = append(setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "[dry-run] would install locally"})
		setup = append(setup, launchtui.BannerDetail{Label: "Hooks", Value: "[dry-run] would remove mirrored hooks from ~/.codex/hooks.json"})
		setup = append(setup, launchtui.BannerDetail{Label: "Agents", Value: "[dry-run] would install wipnote agents into ~/.codex/agents"})
		return setup, nil
	}
	cacheDetails, err := refreshCodexPluginCacheDev(configPath)
	if err != nil {
		return nil, err
	}
	setup = append(setup, cacheDetails...)
	return setup, nil
}

// registerCodexDevMarketplace replaces a mismatched marketplace registration
// and registers the local dev marketplace.
func registerCodexDevMarketplace(configPath, localMarketplace string, dryRun bool) ([]launchtui.BannerDetail, error) {
	var setup []launchtui.BannerDetail
	registeredPath := getCodexMarketplacePathAt(configPath)
	localAbs, _ := filepath.Abs(localMarketplace)
	registeredAbs, _ := filepath.Abs(registeredPath)
	if registeredAbs != "" && registeredAbs != localAbs {
		oldPathDisplay := registeredPath
		if oldPathDisplay == "" {
			oldPathDisplay = "(unknown previous path)"
		}
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Registration", Value: "[dry-run] would remove and replace (" + oldPathDisplay + ")"})
		} else {
			removed, rmErr := removeCodexWipnoteRegistrations(configPath)
			if rmErr != nil {
				return nil, fmt.Errorf("removing mismatched marketplace from %s: %w", configPath, rmErr)
			}
			if removed {
				setup = append(setup, launchtui.BannerDetail{Label: "Registration", Value: "replaced mismatched entry (" + oldPathDisplay + ")"})
			}
		}
		registeredPath = ""
	}
	localDir := codexMarketplaceAddArg(localMarketplace)
	localDirAbs, _ := filepath.Abs(localDir)
	if registeredAbs != localDirAbs {
		addArgs := []string{"plugin", "marketplace", "add", localDir}
		if dryRun {
			setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "[dry-run] codex " + strings.Join(addArgs, " ")})
		} else if out, err := exec.Command("codex", addArgs...).CombinedOutput(); err != nil {
			return nil, fmt.Errorf("registering local marketplace failed: %w\n%s", err, strings.TrimSpace(string(out)))
		} else {
			setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "registered (local)"})
		}
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Marketplace", Value: "already registered (local)"})
	}
	return setup, nil
}

// refreshCodexPluginCacheDev refreshes the local dev plugin cache and returns
// any noteworthy actions as BannerDetail rows for the caller to fold into the
// single launch banner (no banner rendered here).
func refreshCodexPluginCacheDev(configPath string) ([]launchtui.BannerDetail, error) {
	var setup []launchtui.BannerDetail
	if installed, err := ensureCodexLocalPluginInstalled(configPath, true); err != nil {
		return nil, fmt.Errorf("installing local wipnote plugin cache: %w", err)
	} else if installed {
		setup = append(setup, launchtui.BannerDetail{Label: "Plugin cache", Value: "installed locally"})
	}
	if changed, err := pruneCodexGlobalHooksFromCache(); err != nil {
		return nil, fmt.Errorf("removing mirrored wipnote Codex hooks: %w", err)
	} else if changed {
		setup = append(setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "removed from ~/.codex/hooks.json"})
	} else {
		setup = append(setup, launchtui.BannerDetail{Label: "Mirrored hooks", Value: "none found in ~/.codex/hooks.json"})
	}
	pluginDir := codexInstalledPluginDirAt(codexPluginCachePath())
	if changed, err := ensureCodexCustomAgentsInstalled(pluginDir, codexAgentsPath()); err != nil {
		return nil, fmt.Errorf("installing wipnote Codex agents: %w", err)
	} else if changed {
		setup = append(setup, launchtui.BannerDetail{Label: "User agents", Value: "installed in ~/.codex/agents"})
	}
	// No project-level copy (see refreshCodexPluginCacheBestEffort); only
	// remove legacy copies so they cannot shadow the user-level agents.
	projectRoot, _ := resolveProjectRoot()
	if changed, err := removeCodexProjectAgentCopies(projectRoot); err != nil {
		return nil, fmt.Errorf("removing legacy project wipnote Codex agents: %w", err)
	} else if changed {
		setup = append(setup, launchtui.BannerDetail{Label: "Project agents", Value: "removed legacy copies from .codex/agents"})
	}
	return setup, nil
}

// resolveLocalCodexMarketplace returns the absolute path to packages/codex-marketplace/
// by walking up from CWD to find the project root (directory containing .wipnote/).
// Returns an error if no project root is found or the marketplace directory is missing.
func resolveLocalCodexMarketplace() (string, error) {
	wipnoteDir, err := findWipnoteDir()
	if err != nil {
		return "", fmt.Errorf("could not find project root (.wipnote/ directory not found)\n" +
			"Run from the wipnote project directory, or use wipnote codex --init for the marketplace version")
	}
	projectRoot := filepath.Dir(wipnoteDir)
	marketplacePath := filepath.Join(projectRoot, "port", "packages", "codex-marketplace")
	if _, statErr := os.Stat(marketplacePath); os.IsNotExist(statErr) {
		return "", fmt.Errorf("port/packages/codex-marketplace/ not found at %s\n"+
			"Run from the wipnote repo root, or use wipnote codex --init for the marketplace version",
			marketplacePath)
	}
	abs, err := filepath.Abs(marketplacePath)
	if err != nil {
		return "", fmt.Errorf("resolving absolute path for %s: %w", marketplacePath, err)
	}
	return abs, nil
}
