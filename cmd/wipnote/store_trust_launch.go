package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
)

// storeTrustOffHint names the config key that returns trust to the owner.
const storeTrustOffHint = "set launch.claude.trustStore to false in ~/.wipnote/config.json to decide this yourself"

// storeTrustAtLaunch makes sure Claude Code trusts the app store a Claude
// launch is about to add, and returns one line to print, or "" for nothing to
// say. Why: Claude Code asks about an added folder in every new session until
// that folder has its own trust flag, and a delegated worker waits unseen at
// the question (bug-65bf54aa). wipnote made the store and adds it itself, so it
// sets that one flag and says so. It leaves the choice to the owner, with a
// warning, when the config key is off, when the store was not made on this
// machine, when the store holds a .claude entry, or when Claude Code's config
// cannot be written.
func storeTrustAtLaunch(owner, cfgPath string) string {
	if !paths.IsExternalStore(owner) {
		return ""
	}
	ok, err := oneshot.TrustAccepted(owner)
	if err != nil {
		return "" // an unreadable config is Claude Code's to report
	}
	if w := storeClaudeConfigWarning(owner, ok); w != "" || ok {
		return w
	}
	if !launchTrustsStore(cfgPath) || !paths.StoreMadeHere(owner) {
		return storeTrustWarning(owner)
	}
	if err := oneshot.TrustFolder(owner); err != nil {
		return storeTrustWarning(owner) + " (wipnote could not set it: " + err.Error() + ")"
	}
	return "wipnote: marked the store folder " + owner + " as trusted in " + oneshot.ClaudeConfigPath() +
		", so Claude Code does not ask about it in each session; " + storeTrustOffHint
}

// launchTrustsStore reads launch.claude.trustStore from the user config. Only
// an explicit false turns the launch-time trust off.
func launchTrustsStore(cfgPath string) bool {
	if cfgPath == "" {
		return true
	}
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return true
	}
	var f struct {
		Launch struct {
			Claude struct {
				TrustStore *bool `json:"trustStore"`
			} `json:"claude"`
		} `json:"launch"`
	}
	if json.Unmarshal(data, &f) != nil || f.Launch.Claude.TrustStore == nil {
		return true
	}
	return *f.Launch.Claude.TrustStore
}

// trustStoreForClaudeRun handles store trust for a delegate run inside herdr.
// There a claude -p run is an interactive worker that adds the store folder.
// A container run adds no host store folder.
func trustStoreForClaudeRun(w io.Writer, argv []string, projectDir string, container bool) {
	if container || len(argv) == 0 || filepath.Base(argv[0]) != "claude" {
		return
	}
	if line := storeTrustAtLaunch(projectDir, paths.UserConfigPath()); line != "" {
		fmt.Fprintln(w, line)
	}
}

// storeHoldsClaudeConfig reports a .claude entry at the top of the store
// folder. wipnote never writes one there.
func storeHoldsClaudeConfig(owner string) bool {
	_, err := os.Lstat(filepath.Join(owner, ".claude"))
	return err == nil
}

// storeClaudeConfigWarning is the warning for a store that holds a .claude
// entry, or "" when it holds none. Why: Claude Code loads skills, commands and
// subagents from a folder added with --add-dir, and runs a trusted folder's
// hooks when it is opened directly. Agents can write to the store, so a launch
// must not grant trust over a .claude entry nobody checked (bug-65bf54aa).
func storeClaudeConfigWarning(owner string, trusted bool) string {
	if !storeHoldsClaudeConfig(owner) {
		return ""
	}
	entry := filepath.Join(owner, ".claude")
	head := "wipnote: warning: the store folder holds " + entry +
		", which wipnote did not put there; Claude Code loads skills, commands and agents from it"
	if trusted {
		return head + ". Claude Code already trusts this store. Check that entry and remove it if you did not add it"
	}
	return head + ". wipnote did not mark the store as trusted. Check that entry, then " + storeTrustHint
}
