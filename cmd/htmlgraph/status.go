package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	dbpkg "github.com/shakestzd/htmlgraph/internal/db"
	"github.com/shakestzd/htmlgraph/internal/graph"
	"github.com/shakestzd/htmlgraph/internal/models"
	"github.com/shakestzd/htmlgraph/internal/storage"
	versionpkg "github.com/shakestzd/htmlgraph/internal/version"
	"github.com/spf13/cobra"
)

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show work item status summary",
		RunE:  runStatus,
	}
}

func runStatus(_ *cobra.Command, _ []string) error {
	dir, err := findHtmlgraphDir()
	if err != nil {
		return err
	}

	nodes, err := graph.LoadAll(dir)
	if err != nil {
		return fmt.Errorf("load work items: %w", err)
	}

	// Group by type then status.
	type counts struct {
		todo, inProgress, blocked, done, other int
	}
	byType := make(map[string]*counts)
	var inProgress []*models.Node

	for _, n := range nodes {
		if byType[n.Type] == nil {
			byType[n.Type] = &counts{}
		}
		c := byType[n.Type]
		switch n.Status {
		case models.StatusTodo:
			c.todo++
		case models.StatusInProgress:
			c.inProgress++
			inProgress = append(inProgress, n)
		case models.StatusBlocked:
			c.blocked++
		case models.StatusDone:
			c.done++
		default:
			c.other++
		}
	}

	fmt.Printf("HtmlGraph status  (%s)\n\n", dir)

	types := []string{"feature", "bug", "spike", "track"}
	for _, t := range types {
		c := byType[t]
		if c == nil {
			continue
		}
		total := c.todo + c.inProgress + c.blocked + c.done + c.other
		fmt.Printf("  %-10s  %d total  (todo:%d  active:%d  blocked:%d  done:%d)\n",
			t+"s", total, c.todo, c.inProgress, c.blocked, c.done)
	}

	if len(inProgress) > 0 {
		sort.Slice(inProgress, func(i, j int) bool {
			return inProgress[i].ID < inProgress[j].ID
		})
		fmt.Println("\nIn progress:")
		for _, n := range inProgress {
			fmt.Printf("  %-20s  %s\n", n.ID, truncate(n.Title, 60))
		}
	}

	// Attribution rate from git_commits table.
	if dbPath, pathErr := storage.CanonicalDBPath(filepath.Dir(dir)); pathErr == nil {
		fmt.Printf("\nDB: %s\n", dbPath)
		if database, dbErr := dbpkg.Open(dbPath); dbErr == nil {
			defer database.Close()
			total, attributed := dbpkg.CommitAttributionRate(database)
			if total > 0 {
				pct := float64(attributed) / float64(total) * 100
				fmt.Printf("Attribution: %d/%d commits (%.1f%%)\n", attributed, total, pct)
			}
		}
	}

	fmt.Printf("\nTotal: %d work items\n", len(nodes))

	// Collector health — scan .htmlgraph/sessions/ for recent PID files.
	printCollectorHealth(filepath.Dir(dir))

	if latest, newer, _ := versionpkg.CheckForUpdate(version); newer {
		fmt.Printf("\nUpdate available: v%s (current: v%s)\n", latest, version)
	}

	return nil
}

// printCollectorHealth lists per-session collector status for any session
// directory that contains a .collector-pid file. Best-effort: silently skips
// sessions where the PID file is missing or unreadable.
func printCollectorHealth(projectDir string) {
	sessionsDir := filepath.Join(projectDir, ".htmlgraph", "sessions")
	entries, err := os.ReadDir(sessionsDir)
	if err != nil {
		return // no sessions dir yet — nothing to print
	}

	printed := false
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		sessDir := filepath.Join(sessionsDir, e.Name())
		pidPath := filepath.Join(sessDir, ".collector-pid")
		if _, statErr := os.Stat(pidPath); statErr != nil {
			continue // no PID file — collector was never spawned for this session
		}
		cs, err := ReadCollectorStatus(sessDir)
		if err != nil {
			continue
		}
		if !printed {
			fmt.Println("\nCollector health:")
			printed = true
		}
		if cs.Alive {
			fmt.Printf("  %-36s  pid=%-7d port=%-5d alive=true  uptime=%ds\n",
				e.Name(), cs.PID, cs.Port, cs.UptimeSec)
		} else {
			fmt.Printf("  %-36s  pid=%-7d port=%-5d alive=false last_seen=%d\n",
				e.Name(), cs.PID, cs.Port, cs.LastActivityMs)
		}
	}
}
