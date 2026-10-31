package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

const defaultStaleAge = "90d"

func staleCmd() *cobra.Command {
	var olderThan string
	var apply bool
	cmd := &cobra.Command{
		Use:   "stale",
		Short: "List or mark open work items with no update for a long time",
		Long: `List todo and in-progress features, bugs and spikes whose last update is older
than the threshold. Dry-run by default: nothing changes.

With --apply, each listed item is set to status "stale", its open claims are
closed (outcome "abandoned"), and ONE commit records the change. Use
'wipnote feature reset <id>' to return a stale item to todo.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			age, err := parseStaleAge(olderThan)
			if err != nil {
				return err
			}
			dir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			return runStale(cmd, dir, age, apply)
		},
	}
	cmd.Flags().StringVar(&olderThan, "older-than", defaultStaleAge, "age threshold, e.g. 90d, 12h or 4w")
	cmd.Flags().BoolVar(&apply, "apply", false, "mark the listed items stale and commit (default is dry-run)")
	return cmd
}

// parseStaleAge accepts "90d", "4w", a bare day count, or a Go duration.
func parseStaleAge(s string) (time.Duration, error) {
	s = strings.TrimSpace(s)
	units := []struct {
		suffix string
		unit   time.Duration
	}{{"d", 24 * time.Hour}, {"w", 7 * 24 * time.Hour}}
	for _, u := range units {
		if strings.HasSuffix(s, u.suffix) {
			n, err := strconv.Atoi(strings.TrimSuffix(s, u.suffix))
			if err != nil || n < 0 {
				return 0, fmt.Errorf("invalid --older-than %q", s)
			}
			return time.Duration(n) * u.unit, nil
		}
	}
	if n, err := strconv.Atoi(s); err == nil && n >= 0 {
		return time.Duration(n) * 24 * time.Hour, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < 0 {
		return 0, fmt.Errorf("invalid --older-than %q (use e.g. 90d)", s)
	}
	return d, nil
}

// staleCandidates returns open items not updated since now-age, sorted by ID.
func staleCandidates(dir string, age time.Duration, now time.Time) ([]*models.Node, error) {
	nodes, err := graph.LoadAll(dir)
	if err != nil {
		return nil, fmt.Errorf("load work items: %w", err)
	}
	cutoff := now.Add(-age)
	var out []*models.Node
	for _, n := range nodes {
		switch n.Type {
		case "feature", "bug", "spike":
		default:
			continue
		}
		if n.Status != models.StatusTodo && n.Status != models.StatusInProgress {
			continue
		}
		if n.UpdatedAt.IsZero() || !n.UpdatedAt.Before(cutoff) {
			continue
		}
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func runStale(cmd *cobra.Command, dir string, age time.Duration, apply bool) error {
	out := cmd.OutOrStdout()
	items, err := staleCandidates(dir, age, time.Now().UTC())
	if err != nil {
		return err
	}
	for _, n := range items {
		fmt.Fprintf(out, "%-18s %-8s %-12s %s  %s\n", n.ID, n.Type, n.Status,
			n.UpdatedAt.Format("2006-01-02"), n.Title)
	}
	if !apply {
		fmt.Fprintf(out, "%d stale candidate(s). Dry-run: nothing changed. Pass --apply to mark them.\n", len(items))
		return nil
	}
	if len(items) == 0 {
		fmt.Fprintln(out, "Nothing to mark stale.")
		return nil
	}
	paths, err := applyStale(dir, items)
	if err != nil {
		return err
	}
	if err := commitStalePaths(dir, paths, fmt.Sprintf("wipnote: mark %d open items stale", len(items))); err != nil {
		return err
	}
	fmt.Fprintf(out, "Marked %d item(s) stale.\n", len(items))
	return nil
}

// applyStale writes the status change and closes claims. It returns the
// repo-relative paths to commit.
func applyStale(dir string, items []*models.Node) ([]string, error) {
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	defer p.Close()
	store := claimLedgerStore(dir)
	var paths []string
	for _, n := range items {
		col := collectionFor(p, n.Type)
		if err := col.Edit(n.ID).SetStatus(string(models.StatusStale)).Save(); err != nil {
			return paths, fmt.Errorf("mark %s stale: %w", n.ID, err)
		}
		paths = append(paths, workitemArtifactRelPath(n.Type, n.ID))
		if _, err := store.CloseWorkItem(n.ID, claimledger.OutcomeAbandoned, time.Now().UTC(), time.Time{}); err != nil {
			fmt.Fprintf(os.Stderr, "warning: close claims for %s: %v\n", n.ID, err)
		}
	}
	if _, err := os.Stat(store.Dir()); err == nil {
		if rel, rerr := filepath.Rel(filepath.Dir(dir), store.Dir()); rerr == nil {
			paths = append(paths, rel)
		}
	}
	return paths, nil
}

// commitStalePaths stages the paths and makes one commit.
func commitStalePaths(wipnoteDir string, relPaths []string, msg string) error {
	if skipWipnoteGitMutation(wipnoteDir, "stale commit") {
		return nil
	}
	repoRoot := filepath.Dir(wipnoteDir)
	var resultErr error
	_, lockErr := withGitMutationLock(repoRoot, func() ([]byte, error) {
		addArgs := append([]string{"add", "--"}, relPaths...)
		if o, err := runGitWithLockRetry(repoRoot, addArgs...); err != nil {
			resultErr = fmt.Errorf("stale: git add: %s: %w", strings.TrimSpace(string(o)), err)
			return nil, err
		}
		commitArgs := append([]string{"commit", "-m", msg, "--"}, relPaths...)
		if o, err := runGitWithLockRetry(repoRoot, commitArgs...); err != nil {
			resultErr = fmt.Errorf("stale: git commit: %s: %w", strings.TrimSpace(string(o)), err)
			return nil, err
		}
		return nil, nil
	})
	if resultErr == nil {
		resultErr = lockErr
	}
	return resultErr
}
