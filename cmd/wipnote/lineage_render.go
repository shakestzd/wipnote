package main

import (
	"database/sql"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/projection"
	"github.com/shakestzd/wipnote/core/slug"
)

// sortLineageTimeline sorts nodes in place by ascending Timestamp, pushing
// nodes without a timestamp to the END so "oldest first" rendering is honest
// even when only part of the walk has temporal data.
func sortLineageTimeline(nodes []lineageNode) {
	sort.SliceStable(nodes, func(i, j int) bool {
		ai, bj := nodes[i].Timestamp, nodes[j].Timestamp
		if ai == "" && bj == "" {
			return false
		}
		if ai == "" {
			return false
		}
		if bj == "" {
			return true
		}
		return ai < bj
	})
}

// renderLineageJSON emits the stable {root, kind, forward, backward, agent_tree?, orch_*?} schema.
func renderLineageJSON(w io.Writer, root string, kind lineageKind, forward, backward []lineageNode, agentTree string, orch *lineageOrchView) error {
	out := lineageJSON{
		Root:      root,
		Kind:      kind.String(),
		Forward:   forward,
		Backward:  backward,
		AgentTree: agentTree,
	}
	if orch != nil {
		out.OrchSessions, out.OrchError = orch.Sessions, orch.Err
	}
	return writeJSON(w, out)
}

// renderLineageTree prints a human-readable indented tree with the query node
// as the pivot. Backward chains print above the pivot, forward chains below.
// When timeline=true, the same nodes render as a chronological list.
func renderLineageTree(
	w io.Writer,
	db *sql.DB,
	root string,
	kind lineageKind,
	forward, backward []lineageNode,
	timeline bool,
) error {
	// nil arch source: detectLineageKind recognises no "arch:" root, so the
	// pivot node here is never an architecture card. Cards reached as
	// neighbours during the walk are labelled in lineage.resolveTitles.
	rootLabel := graph.FormatNodeLabel(root, graph.ResolveToMap(db, nil, []string{root}))

	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Lineage: %s  [%s]\n", rootLabel, kind)
	fmt.Fprintln(w, sep)

	if timeline {
		all := make([]lineageNode, 0, len(forward)+len(backward))
		all = append(all, backward...)
		all = append(all, forward...)
		sortLineageTimeline(all)
		fmt.Fprintln(w, "\n  Timeline (oldest first):")
		if len(all) == 0 {
			fmt.Fprintln(w, "    (no related nodes)")
			return nil
		}
		for _, n := range all {
			ts := n.Timestamp
			if ts == "" {
				ts = "—"
			}
			fmt.Fprintf(w, "    %s  %s  (%s, d%d)%s\n", ts, n.ID, n.EdgeType, n.Depth, edgeCaveat(n.Metadata))
		}
		return nil
	}

	if len(backward) > 0 {
		fmt.Fprintf(w, "\n  Ancestors (%d):\n", len(backward))
		printLineageBranches(w, root, backward)
	}
	fmt.Fprintf(w, "\n  Pivot: %s\n", rootLabel)
	if len(forward) > 0 {
		fmt.Fprintf(w, "\n  Descendants (%d):\n", len(forward))
		printLineageBranches(w, root, forward)
	}
	if len(forward) == 0 && len(backward) == 0 {
		fmt.Fprintln(w, "\n  (no related nodes — try `wipnote trace` for file/commit attribution)")
	}
	return nil
}

func renderLineageProjectionTree(
	w io.Writer,
	snap *projection.Snapshot,
	root string,
	kind lineageKind,
	forward, backward []lineageNode,
	timeline bool,
) error {
	rootLabel := graph.FormatNodeLabel(root, snap.ResolveToMap([]string{root}))
	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Lineage: %s  [%s]\n", rootLabel, kind)
	fmt.Fprintln(w, sep)
	if timeline {
		all := make([]lineageNode, 0, len(forward)+len(backward))
		all = append(all, backward...)
		all = append(all, forward...)
		sortLineageTimeline(all)
		fmt.Fprintln(w, "\n  Timeline (oldest first):")
		if len(all) == 0 {
			fmt.Fprintln(w, "    (no related nodes)")
			return nil
		}
		for _, n := range all {
			ts := n.Timestamp
			if ts == "" {
				ts = "—"
			}
			fmt.Fprintf(w, "    %s  %s  (%s, d%d)%s\n", ts, n.ID, n.EdgeType, n.Depth, edgeCaveat(n.Metadata))
		}
		return nil
	}
	if len(backward) > 0 {
		fmt.Fprintf(w, "\n  Ancestors (%d):\n", len(backward))
		printLineageBranches(w, root, backward)
	}
	fmt.Fprintf(w, "\n  Pivot: %s\n", rootLabel)
	if len(forward) > 0 {
		fmt.Fprintf(w, "\n  Descendants (%d):\n", len(forward))
		printLineageBranches(w, root, forward)
	}
	if len(forward) == 0 && len(backward) == 0 {
		fmt.Fprintln(w, "\n  (no related nodes — try `wipnote trace` for file/commit attribution)")
	}
	return nil
}

// edgeCaveat renders a terse suffix that stops a text-similarity guess from
// printing identically to a hand-asserted causal claim (bug-b4458e51). The
// graph_edges.metadata JSON is the only place this confidence signal lives —
// an edge from `link add` carries no metadata at all, which is why nil/empty
// is the common case and renders as "" (asserted, no marker).
//
// Two known shapes exist today (see core/graph/pattern.go and
// maybeAttachDedupRelation in workitem_create.go):
//
//   - similarity_score  → a dedup heuristic guess auto-attached by `create`
//     and tagged for human triage. Rendered with a "⚠" marker — the same
//     glyph `wipnote who` already uses for "needs a human look".
//
//   - origin            → a mechanically synthesized edge (plan-slice
//     ordering, batch-apply spec). Rendered as "(derived: <origin>)" — a
//     plain caveat, not a warning, since these are legitimate structural
//     edges rather than unverified guesses.
//
//   - tombstoned       → the edge is canonical but its target no longer
//     resolves (a pruned session). Rendered as "(<kind> pruned)" so the node
//     reads as unresolvable-but-real rather than as a live neighbour whose
//     title merely failed to load — resolveTitles leaves both blank.
//
// Any other non-empty metadata falls back to a generic "(meta)" marker so a
// future signal we don't yet know about still doesn't silently render as
// asserted.
func edgeCaveat(meta map[string]string) string {
	if len(meta) == 0 {
		return ""
	}
	// Checked first: an unresolvable target is a statement about whether the
	// node on the other end exists at all, which outranks any confidence or
	// provenance signal about the edge itself.
	if kind, ok := meta[graph.EdgeMetaTombstoned]; ok {
		return fmt.Sprintf("  (%s pruned)", kind)
	}
	if score, ok := meta["similarity_score"]; ok {
		tag := meta["tag"]
		if tag == "" {
			tag = "guess"
		}
		return fmt.Sprintf("  ⚠ %s (score %s)", tag, score)
	}
	if origin, ok := meta["origin"]; ok {
		return fmt.Sprintf("  (derived: %s)", origin)
	}
	return "  (meta)"
}

// printLineageBranches renders nodes as a real tree by walking the parent
// adjacency built from each node's Parent field. Prior versions indented by
// `Depth` alone, which was wrong for branched walks: BFS order like
// [A,C,B,D] (where B is under A and D is under C) would print B immediately
// after C at indent 2, visually attaching B to C instead of A. Building a
// children-of-parent map and recursing from the pivot preserves true
// parentage no matter how BFS interleaves siblings.
func printLineageBranches(w io.Writer, pivot string, nodes []lineageNode) {
	byParent := make(map[string][]int, len(nodes))
	for i, n := range nodes {
		byParent[n.Parent] = append(byParent[n.Parent], i)
	}
	var dfs func(parent string, indentLevel int)
	dfs = func(parent string, indentLevel int) {
		for _, idx := range byParent[parent] {
			n := nodes[idx]
			indent := strings.Repeat("  ", indentLevel)
			label := n.ID
			if n.Title != "" {
				label = fmt.Sprintf("%s (%s)", n.ID, slug.Truncate(n.Title, 40))
			}
			fmt.Fprintf(w, "  %s[%s] %s%s\n", indent, n.EdgeType, label, edgeCaveat(n.Metadata))
			dfs(n.ID, indentLevel+1)
		}
	}
	// Render every node reachable from the pivot first, then any orphans that
	// landed in the walk with a missing parent entry — they become additional
	// roots rather than being dropped silently.
	dfs(pivot, 1)
	seen := map[string]bool{pivot: true}
	var collectSeen func(parent string)
	collectSeen = func(parent string) {
		for _, idx := range byParent[parent] {
			seen[nodes[idx].ID] = true
			collectSeen(nodes[idx].ID)
		}
	}
	collectSeen(pivot)
	for _, n := range nodes {
		if seen[n.ID] {
			continue
		}
		// Orphan — its Parent wasn't reachable from the pivot. Render as a
		// new root so partial walks degrade gracefully.
		label := n.ID
		if n.Title != "" {
			label = fmt.Sprintf("%s (%s)", n.ID, slug.Truncate(n.Title, 40))
		}
		fmt.Fprintf(w, "  [%s] %s%s  (orphan)\n", n.EdgeType, label, edgeCaveat(n.Metadata))
		seen[n.ID] = true
		collectSeen(n.ID)
	}
}
