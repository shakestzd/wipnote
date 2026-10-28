package main

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/provenance"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

func wiShowCmd(typeName string) *cobra.Command {
	var format string
	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "Show " + typeName + " details",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runWiShowWithFormat(args[0], format)
		},
	}
	cmd.Flags().StringVar(&format, "format", "text", "Output format: json or text")
	return cmd
}

// runWiShowWithFormat shows a work item in the requested format (text or json).
func runWiShowWithFormat(id, format string) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}
	// resolveNodeByUnionID resolves against both live and archived IDs together,
	// ensuring that ambiguous prefixes spanning both sources return an error.
	node, err := resolveNodeByUnionID(dir, id)
	if err != nil {
		return err
	}
	switch format {
	case "json":
		return printNodeDetailJSON(node)
	default:
		printNodeDetail(node)
		// Background runs recorded for the item (feat-36658c22).
		printLineageRuns(os.Stdout, loadLineageRuns(dir, node.ID, detectLineageKind(node.ID)))
		return nil
	}
}

// printNodeDetailJSON outputs a node as indented JSON.
func printNodeDetailJSON(node *models.Node) error {
	return printJSON(node)
}

func printNodeDetail(n *models.Node) {
	printNodeHeader(n)
	printLinkedPRLine(os.Stdout, linkRunner, n.ID)
	printNodeSteps(n)

	if len(n.Edges) > 0 {
		fmt.Println("\nEdges:")
		for rel, edges := range n.Edges {
			for _, e := range edges {
				fmt.Printf("  %-15s → %s\n", rel, e.TargetID)
			}
		}
	}

	printRollup(n)
	printResearchRecord(os.Stdout, n, "")

	if n.Content != "" {
		fmt.Println("\nContent:")
		for _, line := range strings.Split(n.Content, "\n") {
			fmt.Printf("  %s\n", line)
		}
	}

	// Hint for finalized plans: surface the idempotent dispatch command.
	if n.Type == "plan" && string(n.Status) == "finalized" {
		fmt.Printf("\nNext: wipnote plan finalize-yaml %s   (idempotent — creates features, embeds decisions, prints dispatch summary)\n", n.ID)
	}
}

// printNodeHeader prints the title banner and the metadata lines of a node.
func printNodeHeader(n *models.Node) {
	sep := strings.Repeat("─", 60)
	fmt.Println(sep)
	fmt.Printf("  %s\n", n.Title)
	fmt.Println(sep)
	fmt.Printf("  ID        %s\n", n.ID)
	fmt.Printf("  Type      %s\n", n.Type)
	fmt.Printf("  Status    %s\n", n.Status)
	fmt.Printf("  Priority  %s\n", n.Priority)
	if n.TrackID != "" {
		fmt.Printf("  Track     %s\n", n.TrackID)
	}
	if !n.CreatedAt.IsZero() {
		fmt.Printf("  Created   %s\n", n.CreatedAt.Format("2006-01-02"))
	}

	// Provenance line — surface the harness/model/role/CLI version captured
	// at creation. Always print so consumers see an explicit "unknown" rather
	// than silently missing context for legacy items (feat-40ef1333).
	prov := provenance.Provenance{
		Agent:      n.CreatedByAgent,
		Model:      n.CreatedByModel,
		Role:       n.CreatedByRole,
		CLIVersion: n.CreatedByCLIVersion,
	}
	fmt.Printf("  Created by  %s\n", prov.HumanString())

	if adv := acceptedAdvisoryOf(n); adv != "" {
		fmt.Printf("  Accepted-advisory  %s\n", adv)
	}
}

// printNodeSteps prints a node's step checklist with its completion count.
func printNodeSteps(n *models.Node) {
	if len(n.Steps) > 0 {
		done := 0
		for _, s := range n.Steps {
			if s.Completed {
				done++
			}
		}
		fmt.Printf("\nSteps: %d/%d complete\n", done, len(n.Steps))
		for _, s := range n.Steps {
			tick := "[ ]"
			if s.Completed {
				tick = "[x]"
			}
			fmt.Printf("  %s  %s\n", tick, s.Description)
		}
	}
}

// printRollup renders the outcome rollup that Collection.Complete persisted
// into the item's canonical HTML (feat-7ee73444). This is the surface agents
// actually read, so each number is printed next to its provenance rather than
// alone — a cost figure whose source says "degraded_under_report" must not be
// readable as a clean total. Metrics that were omitted for want of data are
// simply not here; the markers (telemetry/git/unavailable) explain why.
func printRollup(n *models.Node) {
	props := workitem.RollupProps(n)
	if len(props) == 0 {
		return
	}

	keys := make([]string, 0, len(props))
	for k := range props {
		if strings.HasSuffix(k, "-source") || strings.HasSuffix(k, "-coverage") {
			continue
		}
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Println("\nRollup:")
	for _, k := range keys {
		line := fmt.Sprintf("  %-16s %s", k, props[k])
		var qual []string
		if src := props[k+"-source"]; src != "" {
			qual = append(qual, src)
		}
		if cov := props[k+"-coverage"]; cov != "" {
			qual = append(qual, "coverage "+cov)
		}
		if len(qual) > 0 {
			line += "  (" + strings.Join(qual, ", ") + ")"
		}
		fmt.Println(line)
	}
}
