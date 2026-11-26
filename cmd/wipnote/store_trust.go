package main

import (
	"fmt"
	"io"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/paths"
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"github.com/spf13/cobra"
)

// wipnote claude adds an app store to the session with --add-dir. Claude Code
// 2.1.326 and later asks for trust on an added folder in every new session, so
// a delegated worker waits for an answer nobody sees (bug-65bf54aa). Trust set
// once in Claude Code's own config lasts. A Claude launch sets it for a store
// made on this machine (storeTrustAtLaunch), and this command sets it on request.

// storeTrustHint names the fix for an untrusted app store.
const storeTrustHint = "run `wipnote store trust` once so Claude Code stops asking about this project's store"

func storeTrustCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:   "trust",
		Short: "Mark this project's app store as trusted in Claude Code, once",
		Long: `wipnote claude gives each session access to the project's app store, a folder
under ~/.wipnote/projects. Claude Code asks whether you trust an added folder in
every new session, which stops a delegated worker until someone answers.

This sets Claude Code's own trust flag for that one folder
(projects["<store>"].hasTrustDialogAccepted in ~/.claude.json), the edit Claude
Code documents for trusting a folder by hand. It changes no other key. A store
inside the project needs no trust of its own.

wipnote claude and a delegated Claude worker set the same flag at launch for a
store made on this machine, and print one line when they do. Run this command
for a store copied from another machine, for a store that holds a .claude entry, or
after you set launch.claude.trustStore to false in ~/.wipnote/config.json.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			// Never make a store here: a dry run in a plain folder must change nothing.
			wipnoteDir, err := findExistingWipnoteDir()
			if err != nil {
				return err
			}
			return runStoreTrust(cmd.OutOrStdout(), filepath.Dir(wipnoteDir), dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "say what would change and change nothing")
	return cmd
}

func runStoreTrust(w io.Writer, owner string, dryRun bool) error {
	if !paths.IsExternalStore(owner) {
		fmt.Fprintf(w, "The store is inside the project (%s). It needs no trust of its own.\n", owner)
		return nil
	}
	ok, err := oneshot.TrustAccepted(owner)
	if err != nil {
		return fmt.Errorf("store trust: %w", err)
	}
	if entry := storeClaudeEntry(owner); entry != "" && !ok {
		fmt.Fprintf(w, "Note: the store folder %s. That applies once the store is trusted.\n", storeClaudeClause(entry))
	}
	switch {
	case ok:
		fmt.Fprintf(w, "Claude Code already trusts %s. Nothing was changed.\n", owner)
	case dryRun:
		fmt.Fprintf(w, "Dry run: would mark %s as trusted in %s. Nothing was changed.\n", owner, oneshot.ClaudeConfigPath())
	default:
		if _, err := oneshot.TrustFolder(owner); err != nil {
			return fmt.Errorf("store trust: %w", err)
		}
		fmt.Fprintf(w, "Marked %s as trusted in %s. New Claude sessions will not ask about it.\n", owner, oneshot.ClaudeConfigPath())
	}
	return nil
}

// storeUntrustedWarning is the warning text for an app store Claude Code does
// not trust. The caller has checked that. The text fits a person's own launch
// and a delegated worker alike.
func storeUntrustedWarning(owner string) string {
	return "wipnote: warning: Claude Code does not trust the store folder " + owner +
		", so each new session asks about it, and a delegated worker waits at that question until someone answers; " + storeTrustHint
}
