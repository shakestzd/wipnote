package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

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
// warning, when the config key is off or unreadable, when the store was not made
// on this machine, when the store holds a .claude entry, or when Claude Code's
// config cannot be read or written.
func storeTrustAtLaunch(owner, cfgPath string) string {
	if !paths.IsExternalStore(owner) {
		return ""
	}
	ok, err := oneshot.TrustAccepted(owner)
	if err != nil {
		// Say so: a silent return left a delegated worker waiting unseen.
		return "wipnote: warning: wipnote could not read Claude Code's config, so it did not check or set trust for the store folder " +
			owner + ". A delegated worker may wait at a trust question (" + err.Error() + ")"
	}
	if w := storeClaudeConfigWarning(owner, ok); w != "" || ok {
		return w
	}
	on, problem := launchTrustsStore(cfgPath)
	if problem != "" {
		return storeUntrustedWarning(owner) + " (wipnote did not set it: " + problem + ")"
	}
	if !on {
		return storeUntrustedWarning(owner)
	}
	if why := paths.StoreNotMadeHere(owner); why != "" {
		return storeUntrustedWarning(owner) + " (wipnote did not set it: " + why + ")"
	}
	wrote, err := oneshot.TrustFolder(owner)
	if err != nil {
		return storeUntrustedWarning(owner) + " (wipnote could not set it: " + err.Error() + ")"
	}
	if !wrote {
		return "" // another launch set the flag first and said so
	}
	return "wipnote: marked the store folder " + owner + " as trusted in " + oneshot.ClaudeConfigPath() +
		", so Claude Code does not ask about it in each session; " + storeTrustOffHint
}

// launchTrustsStore reads launch.claude.trustStore from the user config. A
// missing file or key means on, and false means off. Anything wipnote cannot
// read as true or false means off, with the problem to print. Why: the owner
// may have meant to opt out, and an earlier version read a mistyped "false" or
// a broken file as on, with no warning (bug-65bf54aa).
func launchTrustsStore(cfgPath string) (on bool, problem string) {
	if cfgPath == "" {
		return true, ""
	}
	data, err := os.ReadFile(cfgPath)
	if os.IsNotExist(err) || (err == nil && len(bytes.TrimSpace(data)) == 0) {
		return true, ""
	}
	if err != nil {
		return false, "could not read " + cfgPath + ": " + err.Error()
	}
	var f struct {
		Launch struct {
			Claude struct {
				TrustStore json.RawMessage `json:"trustStore"`
			} `json:"claude"`
		} `json:"launch"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		return false, "could not read launch.claude.trustStore in " + cfgPath + ": " + err.Error()
	}
	switch v := string(bytes.TrimSpace(f.Launch.Claude.TrustStore)); v {
	case "", "true":
		return true, ""
	case "false":
		return false, ""
	default:
		return false, "launch.claude.trustStore in " + cfgPath + " is " + v + ", not true or false"
	}
}

// printStoreTrustLine runs the launch-time store trust and prints its line.
func printStoreTrustLine(w io.Writer, owner string) {
	if line := storeTrustAtLaunch(owner, paths.UserConfigPath()); line != "" {
		fmt.Fprintln(w, line)
	}
}

// storeClaudeEntry returns the first entry in the store folder that blocks
// launch-time trust, or "": an entry named .claude at any depth, or a folder
// wipnote cannot list. It looks at every depth because Claude Code also finds
// .claude folders below the folder it was given, and agents write under the
// store's .wipnote folder. wipnote never writes a .claude entry into a store.
//
// Four rules, each from a bypass a review found (bug-65bf54aa):
//   - The name matches without case. macOS opens .Claude as .claude.
//   - A store folder that is a link is resolved first. WalkDir does not
//     descend a root that is a link.
//   - Only the store's own .git folder is skipped. A .git folder deeper in
//     the store is an ordinary folder that an agent can write.
//   - A folder that cannot be listed blocks trust. Its entries still open by
//     name, so it may hold a .claude entry.
//
// It follows no link below the root.
func storeClaudeEntry(owner string) string {
	root := owner
	if r, err := filepath.EvalSymlinks(owner); err == nil {
		root = r
	}
	ownGit, found := filepath.Join(root, ".git"), ""
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		switch {
		case err != nil && d == nil:
			return nil // the store folder itself is missing
		case err != nil || (path != root && strings.EqualFold(d.Name(), ".claude")):
			found = path
			return filepath.SkipAll
		case path == ownGit && d.IsDir():
			return filepath.SkipDir
		}
		return nil
	})
	if found != "" {
		found = owner + found[len(root):] // name it by the path the owner knows
	}
	return found
}

// storeClaudeClause says what storeClaudeEntry found, as the middle of a
// sentence that starts "the store folder".
func storeClaudeClause(entry string) string {
	if strings.EqualFold(filepath.Base(entry), ".claude") {
		return "holds " + entry + ", which wipnote did not put there; Claude Code loads skills, commands and agents from it"
	}
	return "holds " + entry + ", which wipnote cannot list, so it may hold a .claude folder that Claude Code loads skills, commands and agents from"
}

// storeClaudeConfigWarning is the warning for a store that holds a .claude
// entry, or "" when it holds none. Why: Claude Code loads skills, commands and
// subagents from a folder added with --add-dir, and runs a trusted folder's
// hooks when it is opened directly. Agents can write to the store, so a launch
// must not grant trust over a .claude entry nobody checked (bug-65bf54aa).
func storeClaudeConfigWarning(owner string, trusted bool) string {
	entry := storeClaudeEntry(owner)
	if entry == "" {
		return ""
	}
	head := "wipnote: warning: the store folder " + storeClaudeClause(entry)
	if trusted {
		return head + ". Claude Code already trusts this store. Check that entry and remove it if you did not add it"
	}
	return head + ". wipnote did not mark the store as trusted. Check that entry, then " + storeTrustHint
}
