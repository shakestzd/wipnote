package main

import (
	"fmt"
	"os"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// wiReopenCmd builds `<type> reopen` for features, bugs and spikes. All three
// share executeReopen; an overturned owner question reopens through it too.
func wiReopenCmd(typeName string) *cobra.Command {
	return &cobra.Command{
		Use:   "reopen <id>",
		Short: "Reopen a completed " + typeName + ", setting it back to in-progress",
		Long: "Reopen a completed " + typeName + " by setting its status back to 'in-progress'.\n" +
			"This is a non-destructive operation; all history is preserved.\n\n" +
			"Errors if the " + typeName + " is not currently done.",
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			if err := executeReopen(typeName, args[0]); err != nil {
				return err
			}
			fmt.Printf("Reopened %s %s (status: in-progress).\n", typeName, args[0])
			return nil
		},
	}
}

// executeReopen sets a done feature, bug or spike back to in-progress and
// autocommits the artifact so the transition is durable.
func executeReopen(typeName, id string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	if id, err = workitem.ResolvePartialID(dir, id); err != nil {
		return err
	}
	if kind := workitem.KindForID(id); kind != typeName {
		return fmt.Errorf("%s is a %s, not a %s", id, kind, typeName)
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()
	col := collectionFor(p, typeName)
	// The done check and the write are one locked transaction (col.Reopen),
	// so a concurrent start or complete is never rolled back.
	if _, err := col.Update(id, func(n *models.Node) error {
		if !col.Reopen(n) {
			return fmt.Errorf("%s %s is not done (status: %q) — nothing to reopen", typeName, id, n.Status)
		}
		return nil
	}); err != nil {
		return fmt.Errorf("reopen %s %s: %w", typeName, id, err)
	}
	persistReopen(dir, typeName, id)
	return nil
}

// persistReopen autocommits a reopened item's artifact so the transition is
// durable. Non-fatal: failures log to stderr (feat-712f9194 / roborev #1678).
func persistReopen(dir, typeName, id string) {
	if !shouldAutocommitWorkitemArtifact(typeName) {
		return
	}
	if commitErr := persistWorkitemArtifactTransition(dir, typeName, id, "reopen"); commitErr != nil {
		fmt.Fprintf(os.Stderr, "autocommit warning: %v\n", commitErr)
	}
}
