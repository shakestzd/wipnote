// Register in main.go: rootCmd.AddCommand(wipCmd())
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/sessionledger"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// wipPerSessionSoftLimit is the per-owner-session advisory threshold.
// A session owning this many or more in-progress items is flagged [SOFT LIMIT].
const wipPerSessionSoftLimit = 3

// wipGlobalAdvisoryLimit is the global advisory threshold across all sessions.
// When total in-progress items reach this value the display shows [ADVISORY].
const wipGlobalAdvisoryLimit = 10

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

	// Resolve live session IDs from the authoritative session ledger.
	liveSessions := wipLiveSessions(dir)

	// Group items by owner session (the implemented_in edge target).
	bySession := wipGroupBySession(items)

	globalStatus := "OK"
	if len(items) >= wipGlobalAdvisoryLimit {
		globalStatus = fmt.Sprintf("ADVISORY — global advisory limit %d", wipGlobalAdvisoryLimit)
	}
	fmt.Printf("WIP: %d total  [%s]\n", len(items), globalStatus)

	if len(items) == 0 {
		fmt.Println("\nNo in-progress work items.")
		return nil
	}

	// Print per-session summary table.
	fmt.Println()
	fmt.Printf("%-24s  %-5s  %s\n", "SESSION", "ITEMS", "STATUS")
	fmt.Println(strings.Repeat("-", 50))

	// Stable ordering: sessions sorted, "unknown" always last.
	sessionKeys := wipSortedSessionKeys(bySession)
	for _, sess := range sessionKeys {
		sessItems := bySession[sess]
		sessStatus := "OK"
		if sess != "unknown" {
			if len(sessItems) >= wipPerSessionSoftLimit {
				sessStatus = "SOFT LIMIT"
			}
			if !liveSessions[sess] {
				if sessStatus == "SOFT LIMIT" {
					sessStatus = "SOFT LIMIT  SESSION DEAD?"
				} else {
					sessStatus = "SESSION DEAD?"
				}
			}
		}
		display := truncate(sess, 22)
		fmt.Printf("%-24s  %-5d  %s\n", display, len(sessItems), sessStatus)
	}

	// Print per-item detail table with SESSION column.
	fmt.Println()
	fmt.Printf("%-22s  %-8s  %-16s  %s\n", "ID", "TYPE", "SESSION", "TITLE")
	fmt.Println(strings.Repeat("-", 80))
	for _, sess := range sessionKeys {
		for _, n := range bySession[sess] {
			sessDisplay := truncate(sess, 16)
			if sess != "unknown" && !liveSessions[sess] {
				sessDisplay = truncate(sess, 8) + " [dead?]"
			}
			fmt.Printf("%-22s  %-8s  %-16s  %s\n", n.ID, n.Type, sessDisplay, truncate(n.Title, 36))
		}
	}
	return nil
}

// wipLiveSessions returns the set of currently-active session IDs from the
// canonical session ledger.
// Returns an empty map (not nil) on any error so liveness checks degrade gracefully.
func wipLiveSessions(wipnoteDir string) map[string]bool {
	live := make(map[string]bool)
	sessions, err := sessionledger.NewStore(wipnoteDir).ReadAll()
	if err != nil {
		return live
	}
	for _, s := range sessions {
		if s.IsOpen() {
			live[s.SessionID] = true
		}
	}
	return live
}

// wipLatestImplementedInSession returns the TargetID of the most recent
// implemented_in edge by Edge.Since timestamp, or "unknown" when none exist.
// Items accumulate edges on restart/handoff; the latest edge reflects current
// ownership, not the first session that ever touched the item.
func wipLatestImplementedInSession(n *models.Node) string {
	edges, ok := n.Edges[string(models.RelImplementedIn)]
	if !ok || len(edges) == 0 {
		return "unknown"
	}
	latest := edges[0]
	for _, e := range edges[1:] {
		if e.Since.After(latest.Since) {
			latest = e
		}
	}
	return latest.TargetID
}

// wipGroupBySession groups in-progress nodes by the most-recent implemented_in
// session edge owner. Items with no such edge land in the "unknown" bucket.
func wipGroupBySession(items []*models.Node) map[string][]*models.Node {
	bySession := make(map[string][]*models.Node)
	for _, n := range items {
		sess := wipLatestImplementedInSession(n)
		bySession[sess] = append(bySession[sess], n)
	}
	return bySession
}

// wipSortedSessionKeys returns the session keys in stable alphabetical order,
// with "unknown" always last.
func wipSortedSessionKeys(bySession map[string][]*models.Node) []string {
	keys := make([]string, 0, len(bySession))
	for k := range bySession {
		if k != "unknown" {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	if _, ok := bySession["unknown"]; ok {
		keys = append(keys, "unknown")
	}
	return keys
}

// wipResetCmd marks in-progress items as todo, optionally scoped to dead
// sessions, one session, or orphaned items (GH-#176).
func wipResetCmd() *cobra.Command {
	var force bool
	var scope wipResetScope

	cmd := &cobra.Command{
		Use:   "reset",
		Short: "Reset in-progress items to todo (cleans stale WIP)",
		Long: `Reset in-progress items to todo.

With no scoping flag every in-progress item is reset. Scoping flags narrow the
set and may be combined (union):

  --dead          items whose owning session is not open in the session
                  ledger — exactly the sessions 'wip show' marks SESSION DEAD?
                  (items with no session at all are NOT included; see --orphaned)
  --session <id>  items owned by one session id
  --orphaned      items with no implemented_in session ('unknown' in 'wip show')

--dry-run prints the would-reset list (ID, TYPE, SESSION, TITLE) and writes
nothing; it does not need --force. Any real reset requires --force.

Examples:
  wipnote wip reset --dead --dry-run
  wipnote wip reset --dead --orphaned --force
  wipnote wip reset --session 455daac1-7ac9-4104-91ab-0e2d7a1c9f3e --force`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runWipReset(force, scope)
		},
	}
	cmd.Flags().BoolVar(&force, "force", false, "Required: confirm destructive reset")
	cmd.Flags().BoolVar(&scope.Dead, "dead", false, "Only items whose owning session is not open in the session ledger (SESSION DEAD? in 'wip show')")
	cmd.Flags().StringVar(&scope.Session, "session", "", "Only items owned by this session id")
	cmd.Flags().BoolVar(&scope.Orphaned, "orphaned", false, "Only items with no implemented_in session ('unknown' in 'wip show')")
	cmd.Flags().BoolVar(&scope.DryRun, "dry-run", false, "Print the would-reset list and write nothing")
	return cmd
}

func runWipReset(force bool, scope wipResetScope) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	items, err := scanInProgress(dir)
	if err != nil {
		return err
	}

	live := wipLiveSessions(dir)
	targets := wipResetTargets(items, live, scope)

	if scope.DryRun {
		printWipResetDryRun(os.Stdout, targets, live)
		return nil
	}

	if !force {
		return wipResetForceError(len(targets), scope)
	}

	if len(targets) == 0 {
		if scope.Scoped() {
			fmt.Printf("No in-progress items match scope (%s).\n", scope.Describe())
		} else {
			fmt.Println("No in-progress items found.")
		}
		return nil
	}

	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	reset := 0
	for _, t := range targets {
		if err := resetNodeToTodo(p, t.Node); err != nil {
			fmt.Fprintf(os.Stderr, "warning: reset %s: %v\n", t.Node.ID, err)
			continue
		}
		reset++
		fmt.Printf("Reset: %s  %s\n", t.Node.ID, truncate(t.Node.Title, 50))
	}
	fmt.Printf("\n%d item(s) reset to todo\n", reset)
	return nil
}

// wipResetForceError is the refusal returned when --force is missing.
func wipResetForceError(count int, scope wipResetScope) error {
	if scope.Scoped() {
		return fmt.Errorf("%d in-progress item(s) match scope (%s). This will reset them to todo.\nAdd --force to confirm, or --dry-run to list them first.", count, scope.Describe())
	}
	return fmt.Errorf("%d items are in-progress. This will reset all to todo.\nRun 'wipnote wip reset --force' to confirm, 'wipnote wip reset --dry-run' to list, or 'wipnote wip show' to review first.\nScope with --dead, --session <id>, or --orphaned to keep live sessions' items.", count)
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
