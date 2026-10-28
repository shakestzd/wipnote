package main

import (
	"database/sql"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/projection"
	"github.com/shakestzd/wipnote/internal/lineage"
	"github.com/shakestzd/wipnote/internal/orchlink"
	"github.com/spf13/cobra"
)

// lineageKind classifies the routing target for a `wipnote lineage <id>`
// invocation. Routing is purely string-based: prefix → kind.
type lineageKind int

const (
	kindUnknown lineageKind = iota
	kindFeature
	kindBug
	kindSpike
	kindPlan
	kindTrack
	kindSession
	kindCommit
	kindFile
)

// String makes lineageKind printable for test failures.
func (k lineageKind) String() string {
	switch k {
	case kindFeature:
		return "feature"
	case kindBug:
		return "bug"
	case kindSpike:
		return "spike"
	case kindPlan:
		return "plan"
	case kindTrack:
		return "track"
	case kindSession:
		return "session"
	case kindCommit:
		return "commit"
	case kindFile:
		return "file"
	default:
		return "unknown"
	}
}

// lineageHexRe matches commit-shaped hex strings (7-40 chars).
var lineageHexRe = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// detectLineageKind inspects a CLI argument and returns its routing kind.
// Order matters: ID prefixes win over file path heuristics so an exotic file
// named "feat-x" is still parsed as a work item by intent.
//
// Note: session-ID routing is prefix-only (no length/hex constraint) because
// upstream generators emit multiple schemes — real sessions are `sess-<hex8>`
// but tests and ingest tooling also produce `sess-root-0001`, `sess-orch-abc`,
// etc. Commit-ID routing uses the stricter hex regex because SHAs have a
// fixed alphabet and any accidental collision there would be a bug.
func detectLineageKind(arg string) lineageKind {
	switch {
	case strings.HasPrefix(arg, "feat-"):
		return kindFeature
	case strings.HasPrefix(arg, "bug-"):
		return kindBug
	case strings.HasPrefix(arg, "spk-"):
		return kindSpike
	case strings.HasPrefix(arg, "plan-"):
		return kindPlan
	case strings.HasPrefix(arg, "trk-"):
		return kindTrack
	case strings.HasPrefix(arg, "sess-"):
		return kindSession
	}
	if lineageHexRe.MatchString(arg) {
		return kindCommit
	}
	if strings.ContainsAny(arg, "/.") {
		return kindFile
	}
	return kindUnknown
}

// lineageOpts is the flag bundle for `wipnote lineage`.
//
// depthSet and timelineSet record whether the user explicitly passed the
// corresponding flag on the command line. The commit and file routes reject
// those flags instead of silently ignoring them, so we can't rely on raw
// values — depth defaults to 5 and timeline defaults to false, both of which
// could collide with a deliberate user input.
type lineageOpts struct {
	depth       int
	jsonOut     bool
	timeline    bool
	depthSet    bool
	timelineSet bool
}

// lineageNode is one hop in a forward or backward chain. It is an alias to the
// shared internal/lineage.Node so the BFS walk has a single source of truth
// while the CLI's rendering and --json code keeps its existing field access.
type lineageNode = lineage.Node

// lineageJSON is the stable schema emitted by `wipnote lineage --json`.
//
//	{
//	  "root":     "<id>",
//	  "kind":     "feature|bug|...",
//	  "forward":  [{id,type,title,edge_type,depth,timestamp?,metadata?}, ...],
//	  "backward": [{id,type,title,edge_type,depth,timestamp?,metadata?}, ...],
//	  "agent_tree": "<indented text>",  // only for session roots
//	  "orch_sessions": [...],           // work items served by orch sessions
//	  "orch_error": "<why>"             // the orch registry exists but is unreadable
//	}
//
// Forward edges follow `from_node_id = root` outward; backward edges follow
// `to_node_id = root` inward. Each list is depth-ordered (BFS). For session
// roots the agent spawn tree is included as preformatted text so the --json
// output carries the same information as the human-readable view. metadata
// is the raw graph_edges.metadata JSON (e.g. similarity_score/tag for a dedup
// guess, origin for a mechanically derived edge) and is omitted entirely when
// the edge carries none — machine consumers get the same asserted-vs-derived
// signal the tree renderer's edgeCaveat surfaces for humans.
type lineageJSON struct {
	Root      string        `json:"root"`
	Kind      string        `json:"kind"`
	Forward   []lineageNode `json:"forward"`
	Backward  []lineageNode `json:"backward"`
	AgentTree string        `json:"agent_tree,omitempty"`
	// OrchSessions and OrchError come from lineage_orch.go; both are omitted
	// when orch is not in use or no orch session serves the root.
	OrchSessions []orchlink.Session `json:"orch_sessions,omitempty"`
	OrchError    string             `json:"orch_error,omitempty"`
	// Runs are the background runs recorded for a work item (lineage_runs.go).
	Runs []lineageRun `json:"runs,omitempty"`
}

// allLineageRels lists all 10 relationship types we traverse. It is the shared
// list owned by internal/lineage; we do NOT subset because any of these can
// carry causal meaning depending on the slice in question.
var allLineageRels = lineage.AllRels

func isCanonicalLineageKind(kind lineageKind) bool {
	switch kind {
	case kindFeature, kindBug, kindSpike, kindPlan, kindTrack, kindSession:
		return true
	default:
		return false
	}
}

// newLineageCmd registers `wipnote lineage <id>` — the headline unified
// causal chain command. It auto-detects the input type, walks graph_edges in
// both directions across all 10 relationship types, and renders a tree.
func newLineageCmd() *cobra.Command {
	opts := lineageOpts{depth: 5}
	cmd := &cobra.Command{
		Use:   "lineage <id>",
		Short: "Walk the causal chain for any work item, session, commit, or file",
		Long: `Auto-detects the ID type and renders the bidirectional causal chain.

Supported inputs:
  feat-/bug-/spk-/plan-/trk- ID  — graph walk across all 10 edge types
  sess-<id>                      — graph walk plus agent spawn tree
  <commit-sha>                   — git commit attribution
  <file/path.go>                 — file-to-feature attribution

Examples:
  wipnote lineage feat-48b3783c
  wipnote lineage plan-3b0d5133 --depth 8
  wipnote lineage sess-abc123 --json
  wipnote lineage feat-48b3783c --timeline`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			opts.depthSet = cmd.Flags().Changed("depth")
			opts.timelineSet = cmd.Flags().Changed("timeline")
			if isCanonicalLineageKind(detectLineageKind(args[0])) {
				return runLineageProjection(os.Stdout, dir, args[0], opts)
			}
			db, err := openReadOnlyDB(dir)
			if err != nil {
				return err
			}
			defer db.Close()
			return runLineage(os.Stdout, db, args[0], opts)
		},
	}
	cmd.Flags().IntVar(&opts.depth, "depth", 5, "maximum hop count for graph walk")
	cmd.Flags().BoolVar(&opts.jsonOut, "json", false, "emit structured JSON output")
	cmd.Flags().BoolVar(&opts.timeline, "timeline", false, "sort results chronologically instead of as a tree")
	return cmd
}

// runLineage is the testable entry point. It dispatches based on
// detectLineageKind, walks the graph in both directions, and renders.
//
// Commit SHAs and file paths short-circuit to the existing attribution
// primitives (TraceCommit / TraceFile) because graph_edges does not store
// commit or file nodes — a bfsWalk rooted at a commit or file would always
// return empty. Work-item, plan, track, and session kinds go through the
// bidirectional graph walker.
func runLineage(w io.Writer, db *sql.DB, arg string, opts lineageOpts) error {
	if opts.depth <= 0 {
		opts.depth = 5
	}
	kind := detectLineageKind(arg)

	switch kind {
	case kindCommit:
		return runLineageCommit(w, db, arg, opts)
	case kindFile:
		return runLineageFile(w, db, arg, opts)
	}

	forward, err := lineage.ForwardWalk(db, arg, allLineageRels, opts.depth)
	if err != nil {
		return fmt.Errorf("forward walk: %w", err)
	}
	backward, err := lineage.BackwardWalk(db, arg, allLineageRels, opts.depth)
	if err != nil {
		return fmt.Errorf("backward walk: %w", err)
	}

	if opts.timeline {
		lineage.AnnotateTimestamps(db, forward)
		lineage.AnnotateTimestamps(db, backward)
	}

	// Session roots carry an agent spawn tree as a secondary view. Render it
	// once so both the JSON and text outputs can include it.
	var agentTree string
	if kind == kindSession {
		if tree, treeErr := RenderAgentTree(db, arg); treeErr == nil {
			agentTree = tree
		}
	}

	if opts.jsonOut {
		return renderLineageJSON(w, arg, kind, forward, backward, agentTree, nil, nil)
	}

	if err := renderLineageTree(w, db, arg, kind, forward, backward, opts.timeline); err != nil {
		return err
	}

	if strings.TrimSpace(agentTree) != "" {
		fmt.Fprintln(w)
		fmt.Fprintln(w, "Agent spawn chain:")
		fmt.Fprint(w, agentTree)
	}
	return nil
}

func runLineageProjection(w io.Writer, wipnoteDir, arg string, opts lineageOpts) error {
	if opts.depth <= 0 {
		opts.depth = 5
	}
	kind := detectLineageKind(arg)
	snap, err := projection.Load(wipnoteDir)
	if err != nil {
		return fmt.Errorf("load canonical projection: %w", err)
	}
	forward := lineage.ForwardProjectionWalk(snap, arg, allLineageRels, opts.depth)
	backward := lineage.BackwardProjectionWalk(snap, arg, allLineageRels, opts.depth)
	orch := loadLineageOrch(arg, kind, forward)
	runs := loadLineageRuns(wipnoteDir, arg, kind)
	if opts.jsonOut {
		return renderLineageJSON(w, arg, kind, forward, backward, "", orch, runs)
	}
	if err := renderLineageProjectionTree(w, snap, arg, kind, forward, backward, opts.timeline); err != nil {
		return err
	}
	printLineageResearch(w, wipnoteDir, arg)
	printLineageOrch(w, orch)
	printLineageRuns(w, runs)
	return nil
}
