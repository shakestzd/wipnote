package main

import (
	"database/sql"
	"fmt"
	"path/filepath"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

func collectSessionIDs(database *sql.DB, validIDs map[string]bool) {
	rows, err := database.Query("SELECT session_id FROM sessions")
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			validIDs[id] = true
		}
	}
}

// collectPlanIDs registers every plan ID in validIDs. Plans are canonical
// nodes, but unlike tracks/features/bugs/spikes they have no row in any node
// table, so no indexing pass ever registers them. validIDs is a target-validity
// whitelist rather than a node table (session IDs live in it for the same
// reason), and indexNodeEdges consults it — so an
// unregistered plan ID means every edge touching a plan is dropped on rebuild.
//
// HTML is the canonical node file, so its parsed article ID wins; the YAML stem
// is registered too because reindexPlanEdges emits edges keyed off the YAML and
// a plan can exist as YAML before its HTML has been rendered.
func collectPlanIDs(wipnoteDir string, validIDs map[string]bool) {
	htmlFiles, _ := filepath.Glob(filepath.Join(wipnoteDir, "plans", "*.html"))
	for _, f := range htmlFiles {
		node, err := htmlparse.ParseFile(f)
		if err != nil || node.ID == "" {
			continue
		}
		validIDs[node.ID] = true
	}

	yamlFiles, _ := filepath.Glob(filepath.Join(wipnoteDir, "plans", "*.yaml"))
	for _, f := range yamlFiles {
		if id := strings.TrimSuffix(filepath.Base(f), ".yaml"); id != "" {
			validIDs[id] = true
		}
	}
}

func reindexEdges(database *sql.DB, wipnoteDir string, validIDs map[string]bool) {
	dirs := []struct {
		subdir   string
		nodeType string
	}{
		{"tracks", "track"},
		{"features", "feature"},
		{"bugs", "bug"},
		{"spikes", "spike"},
		// Plans declare edges in their own <nav data-graph-edges> block
		// (contains / blocks / blocked_by / relates_to / implemented_in).
		// They are only scannable because collectPlanIDs registered their IDs
		// — the loop below gates the SOURCE on validIDs too (bug-d5eaf6a4).
		{"plans", "plan"},
	}
	for _, d := range dirs {
		pattern := filepath.Join(wipnoteDir, d.subdir, "*.html")
		files, _ := filepath.Glob(pattern)
		for _, f := range files {
			node, err := htmlparse.ParseFile(f)
			if err != nil || !validIDs[node.ID] {
				continue
			}
			indexNodeEdges(database, node, d.nodeType, validIDs)
		}
	}
}

// indexNodeEdges inserts every graph edge declared by node, applying the
// target-validity gate. Shared by reindexEdges (file-backed) and
// reindexWorkitemLedger (archive-backed) so archived items keep their lineage
// edges in graph_edges and remain traversable by wipnote lineage/trace.
//
// Both callers gate the SOURCE on validIDs before calling, so every edge that
// reaches here is declared by a canonical HTML node. That is what licenses the
// tombstone half of the gate: the declaration is git-tracked and permanent even
// when the session it names has been pruned, so graph.EdgeTargetTombstoned
// keeps the edge with a marker rather than erasing the provenance record
// (bug-10e166d8). A target that is neither valid nor session-shaped is still a
// genuine dangling reference and is still dropped.
func indexNodeEdges(database *sql.DB, node *models.Node, fromNodeType string, validIDs map[string]bool) {
	for _, edges := range node.Edges {
		for _, e := range edges {
			props := e.Properties
			switch graph.ClassifyEdgeTarget(e.TargetID, validIDs) {
			case graph.EdgeTargetDangling:
				continue
			case graph.EdgeTargetTombstoned:
				props = graph.MarkEdgeTombstoned(props)
			case graph.EdgeTargetLive:
				// Unchanged: a live target indexes exactly as it always has.
			}
			edgeID := fmt.Sprintf("%s-%s-%s", node.ID, string(e.Relationship), e.TargetID)
			_ = dbpkg.InsertEdge(
				database,
				edgeID, node.ID, fromNodeType,
				e.TargetID, inferNodeTypeFromID(e.TargetID),
				string(e.Relationship),
				props,
			)
		}
	}
}

func inferNodeTypeFromID(id string) string {
	switch {
	case len(id) > 5 && id[:5] == "feat-":
		return "feature"
	case len(id) > 4 && id[:4] == "bug-":
		return "bug"
	case len(id) > 4 && id[:4] == "spk-":
		return "spike"
	case len(id) > 4 && id[:4] == "trk-":
		return "track"
	case len(id) > 5 && id[:5] == "plan-":
		return "plan"
	case len(id) > 5 && id[:5] == "spec-":
		return "spec"
	case len(id) > 5 && id[:5] == "sess-":
		return "session"
	case len(id) > 6 && id[:6] == "recap-":
		return "recap"
	default:
		return "unknown"
	}
}

// fixImplementedInEdges corrects implemented_in edges that have to_node_type='unknown'.
// Session IDs are UUIDs (not prefixed), so inferNodeTypeFromID returns 'unknown' by
// default. This function updates all implemented_in edges with unknown target types
// by re-inferring the correct type from the target ID.
func fixImplementedInEdges(database *sql.DB) {
	// Fetch all implemented_in edges with unknown target type.
	rows, err := database.Query(`
		SELECT id, to_node_id FROM graph_edges
		WHERE relationship_type = 'implemented_in' AND to_node_type = 'unknown'
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	var toFix []struct {
		id       string
		toNodeID string
	}
	for rows.Next() {
		var edge struct {
			id       string
			toNodeID string
		}
		if err := rows.Scan(&edge.id, &edge.toNodeID); err != nil {
			continue
		}
		toFix = append(toFix, edge)
	}

	// Update each edge with the correct inferred type.
	for _, edge := range toFix {
		correctType := inferNodeTypeFromID(edge.toNodeID)
		_, _ = database.Exec(
			`UPDATE graph_edges SET to_node_type = ? WHERE id = ?`,
			correctType, edge.id,
		)
	}
}
