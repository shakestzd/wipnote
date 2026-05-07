// Register in main.go: rootCmd.AddCommand(wipCmd())
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/internal/htmlparse"
	"github.com/shakestzd/wipnote/internal/models"
	"github.com/shakestzd/wipnote/internal/workitem"
	"github.com/spf13/cobra"
)

const wipLimit = 5

func wipCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wip",
		Short: "Manage WIP (work-in-progress) limits",
	}
	cmd.AddCommand(wipShowCmd())
	cmd.AddCommand(wipResetCmd())
	return cmd
}

// wipShowCmd displays in-progress items against the WIP limit.
func wipShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show",
		Short: "Show current WIP count and in-progress items",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWipShow()
		},
	}
}

func runWipShow() error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	items, err := scanInProgress(dir)
	if err != nil {
		return err
	}

	status := "OK"
	if len(items) >= wipLimit {
		status = "AT LIMIT"
	}
	fmt.Printf("WIP: %d / %d  [%s]\n\n", len(items), wipLimit, status)

	if len(items) == 0 {
		fmt.Println("No in-progress work items.")
		return nil
	}

	fmt.Printf("%-22s  %-8s  %s\n", "ID", "TYPE", "TITLE")
	fmt.Println(strings.Repeat("-", 70))
	for _, n := range items {
		fmt.Printf("%-22s  %-8s  %s\n", n.ID, n.Type, truncate(n.Title, 44))
	}
	return nil
}

// wipResetCmd marks all in-progress items as todo.
func wipResetCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset all in-progress items to todo (cleans stale WIP)",
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWipReset(force)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Required: confirm destructive reset")
	return cmd
}

func runWipReset(force bool) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	items, err := scanInProgress(dir)
	if err != nil {
		return err
	}

	if !force {
		count := len(items)
		return fmt.Errorf("%d items are in-progress. This will reset all to todo.\nRun 'wipnote wip reset --force' to confirm, or 'wipnote wip show' to review first.", count)
	}

	if len(items) == 0 {
		fmt.Println("No in-progress items found.")
		return nil
	}

	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	for _, n := range items {
		if err := resetNodeToTodo(p, n); err != nil {
			fmt.Fprintf(os.Stderr, "warning: reset %s: %v\n", n.ID, err)
			continue
		}
		fmt.Printf("Reset: %s  %s\n", n.ID, truncate(n.Title, 50))
	}
	fmt.Printf("\n%d item(s) reset to todo\n", len(items))
	return nil
}

// resetNodeToTodo writes the node back with status=todo and cleared agent.
func resetNodeToTodo(p *workitem.Project, n *models.Node) error {
	n.Status = models.StatusTodo
	n.AgentAssigned = ""
	n.UpdatedAt = time.Now().UTC()
	dir := collectionDir(p, n.Type)
	_, err := workitem.WriteNodeHTML(dir, n)
	return err
}

// collectionDir maps a node type to its collection directory.
func collectionDir(p *workitem.Project, nodeType string) string {
	switch nodeType {
	case "bug":
		return p.BugsDir()
	case "spike":
		return p.SpikesDir()
	default: // "feature" and anything else
		return p.FeaturesDir()
	}
}

// scanInProgress collects all in-progress nodes across features, bugs, spikes.
func scanInProgress(wipnoteDir string) ([]*models.Node, error) {
	dirs := []struct {
		path     string
		nodeType string
	}{
		{filepath.Join(wipnoteDir, "features"), "feature"},
		{filepath.Join(wipnoteDir, "bugs"), "bug"},
		{filepath.Join(wipnoteDir, "spikes"), "spike"},
	}

	var items []*models.Node
	for _, d := range dirs {
		found, err := loadInProgressFromDir(d.path)
		if err != nil {
			return nil, fmt.Errorf("scan %s: %w", d.nodeType, err)
		}
		items = append(items, found...)
	}
	return items, nil
}

// loadInProgressFromDir scans one directory for in-progress nodes.
func loadInProgressFromDir(dir string) ([]*models.Node, error) {
	entries, err := os.ReadDir(dir)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	var out []*models.Node
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".html") {
			continue
		}
		node, err := htmlparse.ParseFile(filepath.Join(dir, entry.Name()))
		if err != nil {
			continue // skip unparseable files
		}
		if node.Status == models.StatusInProgress {
			out = append(out, node)
		}
	}
	return out, nil
}
