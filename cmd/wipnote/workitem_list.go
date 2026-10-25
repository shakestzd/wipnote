package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/spf13/cobra"
)

// wiListRow is one work item in `wipnote <type> list --format json`: the
// fields a board needs, with the full title the text table truncates.
type wiListRow struct {
	ID            string    `json:"id"`
	Type          string    `json:"type"`
	Title         string    `json:"title"`
	Status        string    `json:"status"`
	Priority      string    `json:"priority"`
	TrackID       string    `json:"track_id,omitempty"`
	AgentAssigned string    `json:"agent_assigned,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func wiListCmd(_ string, dirName string) *cobra.Command {
	var statusFilter, format string
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List " + dirName,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runWiListTo(os.Stdout, dir, dirName, statusFilter, format)
		},
	}
	cmd.Flags().StringVarP(&statusFilter, "status", "s", "",
		"Filter by status (todo, in-progress, blocked, done)")
	cmd.Flags().StringVar(&format, "format", "text", "Output format: json or text")
	return cmd
}

// runWiListTo lists the work items in one collection directory, sorted by ID.
// JSON output is always an array (empty when nothing matches).
func runWiListTo(w io.Writer, wipnoteDir, dirName, statusFilter, format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("unknown format %q: use json or text", format)
	}
	nodes, err := graph.LoadDir(filepath.Join(wipnoteDir, dirName))
	if err != nil {
		return fmt.Errorf("load %s: %w", dirName, err)
	}

	filtered := make([]*models.Node, 0, len(nodes))
	for _, n := range nodes {
		if statusFilter != "" && string(n.Status) != statusFilter {
			continue
		}
		filtered = append(filtered, n)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].ID < filtered[j].ID
	})

	if format == "json" {
		return writeWiListJSON(w, filtered)
	}
	writeWiListTable(w, dirName, filtered)
	return nil
}

func writeWiListJSON(w io.Writer, nodes []*models.Node) error {
	rows := make([]wiListRow, 0, len(nodes))
	for _, n := range nodes {
		rows = append(rows, wiListRow{
			ID: n.ID, Type: n.Type, Title: n.Title, Status: string(n.Status), Priority: string(n.Priority),
			TrackID: n.TrackID, AgentAssigned: n.AgentAssigned, CreatedAt: n.CreatedAt, UpdatedAt: n.UpdatedAt,
		})
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(rows)
}

func writeWiListTable(w io.Writer, dirName string, nodes []*models.Node) {
	if len(nodes) == 0 {
		fmt.Fprintf(w, "No %s found.\n", dirName)
		return
	}
	fmt.Fprintf(w, "%-22s  %-11s  %-8s  %s\n", "ID", "STATUS", "PRIORITY", "TITLE")
	fmt.Fprintln(w, strings.Repeat("-", 80))
	for _, n := range nodes {
		marker := "  "
		if n.Status == models.StatusInProgress {
			marker = "* "
		}
		fmt.Fprintf(w, "%s%-20s  %-11s  %-8s  %s\n",
			marker, n.ID, n.Status, n.Priority, truncate(n.Title, 44))
	}
	fmt.Fprintf(w, "\n%d %s\n", len(nodes), dirName)
}
