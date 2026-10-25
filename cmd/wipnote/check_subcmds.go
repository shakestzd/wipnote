package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/slug"
	"github.com/spf13/cobra"
)

// checkAcceptedAdvisoryCmd surfaces work items that were completed via the
// provenance-gate override (feat-7b593272). Each such item carries an
// `accepted_advisory` property recording the operator rationale for
// completing a code-bearing item with zero linked source commits. This is
// the compliance/audit surface for those overrides.
//
// Reads canonical HTML (graph.LoadDir) so it works on a fresh clone with no
// SQLite read index, mirroring `wipnote check dups`.
func checkAcceptedAdvisoryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "accepted-advisory",
		Short: "Report items completed via the zero-commit provenance override",
		Long: `Scan features, bugs, and spikes for the accepted_advisory marker
recorded when an item was completed with --accepted-advisory (an audited
override of the zero-commit provenance gate).

Reads canonical HTML, so it works even when the SQLite read index is absent.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCheckAcceptedAdvisory(os.Stdout)
		},
	}
}

type advisoryItem struct {
	id, title, itemType, reason string
}

func runCheckAcceptedAdvisory(w io.Writer) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	var items []advisoryItem
	for _, d := range []struct{ subdir, typeName string }{
		{"features", "feature"},
		{"bugs", "bug"},
		{"spikes", "spike"},
	} {
		nodes, lerr := graph.LoadDir(filepath.Join(dir, d.subdir))
		if lerr != nil {
			// Missing subdir / unreadable store: graceful skip, never fail.
			continue
		}
		for _, n := range nodes {
			if reason := acceptedAdvisoryOf(n); reason != "" {
				items = append(items, advisoryItem{
					id: n.ID, title: n.Title,
					itemType: d.typeName, reason: reason,
				})
			}
		}
	}

	if len(items) == 0 {
		fmt.Fprintln(w, "No items completed via the accepted-advisory provenance override.")
		return nil
	}

	sort.Slice(items, func(i, j int) bool { return items[i].id < items[j].id })

	fmt.Fprintf(w, "Found %d item(s) completed via accepted-advisory (zero-commit provenance override):\n", len(items))
	fmt.Fprintln(w, strings.Repeat("-", 72))
	for _, it := range items {
		fmt.Fprintf(w, "  %-7s  %-20s  %s\n", it.itemType, it.id, slug.Truncate(it.title, 34))
		fmt.Fprintf(w, "           reason: %s\n", it.reason)
	}
	return nil
}

// checkDupsCmd surfaces needs-triage-dup clusters created by the
// dedup-at-create flow (slice-6, feat-e8879220). It scans canonical HTML
// (graph.LoadDir) — NOT the SQLite index — so it works on a fresh clone with
// no read index built, mirroring `wipnote check orphans`. A cluster is any
// item carrying a relates_to edge whose Title begins with "needs-triage-dup".
func checkDupsCmd() *cobra.Command {
	var strict bool
	cmd := &cobra.Command{
		Use:   "dups",
		Short: "Report items flagged as possible duplicates (needs-triage-dup)",
		Long: `Scan features and bugs for the needs-triage-dup marker auto-attached
at create time when a strong title+description similarity match was found.

Reads canonical HTML, so it works even when the SQLite read index is absent.
Use --strict to return exit code 1 if any unresolved duplicate clusters exist.`,
		RunE: func(_ *cobra.Command, _ []string) error {
			return runCheckDups(strict)
		},
	}
	cmd.Flags().BoolVar(&strict, "strict", false,
		"exit code 1 if needs-triage-dup clusters exist")
	return cmd
}

func runCheckDups(strict bool) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	type dupItem struct {
		id, title, relatesTo, itemType string
	}
	var dups []dupItem

	for _, d := range []struct{ subdir, typeName string }{
		{"features", "feature"},
		{"bugs", "bug"},
	} {
		nodes, lerr := graph.LoadDir(filepath.Join(dir, d.subdir))
		if lerr != nil {
			// Missing subdir / unreadable store: graceful skip, never fail.
			continue
		}
		for _, n := range nodes {
			for _, e := range n.Edges[string(models.RelRelatesTo)] {
				isDup := strings.HasPrefix(e.Title, needsTriageDupTag) ||
					(e.Properties != nil && e.Properties["tag"] == needsTriageDupTag)
				if isDup {
					dups = append(dups, dupItem{
						id: n.ID, title: n.Title,
						relatesTo: e.TargetID, itemType: d.typeName,
					})
				}
			}
		}
	}

	if len(dups) == 0 {
		fmt.Println("No needs-triage-dup clusters found.")
		return nil
	}

	fmt.Printf("Found %d possible-duplicate item(s) needing triage:\n", len(dups))
	fmt.Println(strings.Repeat("-", 70))
	for _, d := range dups {
		fmt.Printf("  %-7s  %-18s  ~ %-18s  %s\n",
			d.itemType, d.id, d.relatesTo, slug.Truncate(d.title, 30))
	}
	fmt.Printf("\nResolve by closing the duplicate or removing the relates_to edge\n" +
		"(e.g. 'wipnote link remove <id> <target> relates_to').\n")

	if strict {
		os.Exit(1)
	}
	return nil
}
