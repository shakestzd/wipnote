package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/internal/htmlparse"
	"github.com/shakestzd/wipnote/internal/models"
	"github.com/shakestzd/wipnote/internal/workitem"
	"github.com/spf13/cobra"
)

// workItemIDPattern matches canonical work item IDs like feat-abc12345, bug-abc12345, etc.
var workItemIDPattern = regexp.MustCompile(`^(feat|bug|spk|trk|plan|pln|spec|spc)-[0-9a-f]{8}$`)

// knownCollections is the set of valid collection names for find.
var knownCollections = map[string]bool{
	"features": true,
	"bugs":     true,
	"spikes":   true,
	"tracks":   true,
	"plans":    true,
	"specs":    true,
	"all":      true,
}

func findCmd() *cobra.Command {
	var (
		status   string
		priority string
		title    string
		trackID  string
		agent    string
		orderBy  string
		limit    int
	)

	cmd := &cobra.Command{
		Use:   "find <collection>",
		Short: "Query work items with filters",
		Long: `Search across collections using composable filters.

Collections: features, bugs, spikes, tracks, plans, specs, all

Examples:
  wipnote find features --status blocked
  wipnote find bugs --priority high --status todo
  wipnote find all --status in-progress --order-by created
  wipnote find features --title "auth" --limit 5`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runFind(args[0], findOpts{
				status:   status,
				priority: priority,
				title:    title,
				trackID:  trackID,
				agent:    agent,
				orderBy:  orderBy,
				limit:    limit,
			})
		},
	}

	cmd.Flags().StringVarP(&status, "status", "s", "",
		"Filter by status (todo, in-progress, blocked, done)")
	cmd.Flags().StringVarP(&priority, "priority", "p", "",
		"Filter by priority (low, medium, high, critical)")
	cmd.Flags().StringVarP(&title, "title", "t", "",
		"Filter by title substring (case-insensitive)")
	cmd.Flags().StringVar(&trackID, "track", "",
		"Filter by track ID")
	cmd.Flags().StringVar(&agent, "agent", "",
		"Filter by assigned agent")
	cmd.Flags().StringVar(&orderBy, "order-by", "",
		"Sort field: created, updated, title, priority, id")
	cmd.Flags().IntVarP(&limit, "limit", "n", 0,
		"Maximum number of results")

	return cmd
}

// findOpts holds parsed CLI flags for the find command.
type findOpts struct {
	status   string
	priority string
	title    string
	trackID  string
	agent    string
	orderBy  string
	limit    int
}

func runFind(collection string, opts findOpts) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	// If the argument looks like a work item ID, do a direct lookup.
	if workItemIDPattern.MatchString(collection) {
		return runFindByID(dir, collection)
	}

	// If the argument is not a known collection name, treat it as a title search.
	if !knownCollections[collection] {
		opts.title = collection
		collection = "all"
	}

	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return fmt.Errorf("open project: %w", err)
	}
	defer p.Close()

	// Build query.
	var q *workitem.Query
	if collection == "all" {
		q = p.FindAll()
	} else {
		q = p.Find(collection)
	}

	// Apply filters.
	if opts.status != "" {
		q = q.Where(workitem.StatusIs(opts.status))
	}
	if opts.priority != "" {
		q = q.Where(workitem.PriorityIs(opts.priority))
	}
	if opts.title != "" {
		q = q.Where(workitem.TitleContains(opts.title))
	}
	if opts.trackID != "" {
		q = q.Where(workitem.TrackIs(opts.trackID))
	}
	if opts.agent != "" {
		q = q.Where(workitem.AgentIs(opts.agent))
	}

	// Apply ordering.
	if opts.orderBy != "" {
		q = q.OrderBy(opts.orderBy, workitem.Asc)
	}

	// Apply limit.
	if opts.limit > 0 {
		q = q.Limit(opts.limit)
	}

	nodes, err := q.Execute()
	if err != nil {
		return fmt.Errorf("find: %w", err)
	}

	if len(nodes) == 0 {
		fmt.Println("No matching items found.")
		return nil
	}

	printFindResults(nodes)
	return nil
}

// runFindByID resolves a work item by its canonical ID and prints it.
func runFindByID(dir, id string) error {
	path := resolveNodePath(dir, id)
	if path == "" {
		kind := kindFromPrefix(id)
		return fmt.Errorf("find: no item found with ID %q\nRun 'wipnote %s list' to see valid IDs, or 'wipnote find all --title <keyword>' to search by title", id, kind)
	}
	node, err := htmlparse.ParseFile(path)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	printFindResults([]*models.Node{node})
	return nil
}

func printFindResults(nodes []*models.Node) {
	fmt.Printf("%-22s  %-8s  %-11s  %-8s  %s\n",
		"ID", "TYPE", "STATUS", "PRIORITY", "TITLE")
	fmt.Println(strings.Repeat("-", 80))

	for _, n := range nodes {
		marker := "  "
		if n.Status == models.StatusInProgress {
			marker = "* "
		}
		fmt.Printf("%s%-20s  %-8s  %-11s  %-8s  %s\n",
			marker, n.ID, n.Type, n.Status, n.Priority,
			truncate(n.Title, 36))
	}

	fmt.Printf("\n%d item(s)\n", len(nodes))
}
