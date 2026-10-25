package graph

import (
	"database/sql"
	"fmt"
	"strings"

	corearch "github.com/shakestzd/wipnote/core/arch"
)

// NodeResult holds a node ID with optional metadata from a query.
type NodeResult struct {
	ID     string
	Type   string
	Title  string
	Status string
}

// QueryBuilder chains graph traversal operations into a fluent API.
// It reads from the graph_edges table and resolves node metadata from the
// derived node indexes, except for architecture cards, which come from their
// canonical HTML store via archs.
type QueryBuilder struct {
	db    *sql.DB
	archs *archLookup
}

// WithArch attaches the architecture card source used to resolve arch nodes.
func (q *QueryBuilder) WithArch(src ArchSource) *QueryBuilder {
	q.archs = newArchLookup(src)
	return q
}

// archLookupOrEmpty returns the attached lookup, or an empty one that resolves
// no cards. Keeps resolveNodes free of nil checks.
func (q *QueryBuilder) archLookupOrEmpty() *archLookup {
	if q.archs == nil {
		q.archs = newArchLookup(nil)
	}
	return q.archs
}

// resolveNodes looks up metadata for a set of node IDs.
func (q *QueryBuilder) resolveNodes(ids []string) ([]NodeResult, error) {
	if len(ids) == 0 {
		return nil, nil
	}

	allPlaceholders := make([]string, len(ids))
	allArgs := make([]any, len(ids))
	for i, id := range ids {
		allPlaceholders[i] = "?"
		allArgs[i] = id
	}
	allInClause := strings.Join(allPlaceholders, ",")

	query := fmt.Sprintf(`
		SELECT id, type, title, status FROM features WHERE id IN (%s)
		UNION ALL
		SELECT id, type, title, status FROM tracks WHERE id IN (%s)
		UNION ALL
		SELECT commit_hash AS id, 'commit' AS type, SUBSTR(COALESCE(message,''),1,80) AS title, 'done' AS status FROM git_commits WHERE commit_hash IN (%s)
		UNION ALL
		SELECT DISTINCT file_path AS id, 'file' AS type, file_path AS title, '' AS status FROM feature_files WHERE file_path IN (%s)
		UNION ALL
		SELECT session_id AS id, 'session' AS type, COALESCE(title,'') AS title, COALESCE(status,'') AS status FROM sessions WHERE session_id IN (%s)
		UNION ALL
		SELECT DISTINCT name AS id, 'agent' AS type, name AS title, '' AS status FROM (
			SELECT agent_name AS name FROM agent_lineage_trace WHERE agent_name != ''
			UNION
			SELECT agent_assigned AS name FROM sessions WHERE agent_assigned != ''
		) WHERE name IN (%s)`,
		allInClause, allInClause, allInClause, allInClause, allInClause, allInClause)

	fullArgs := make([]any, 0, len(allArgs)*6)
	for i := 0; i < 6; i++ {
		fullArgs = append(fullArgs, allArgs...)
	}

	rows, err := q.db.Query(query, fullArgs...)
	if err != nil {
		return nil, fmt.Errorf("resolve nodes: %w", err)
	}
	defer rows.Close()

	resolved := make(map[string]NodeResult, len(ids))
	for rows.Next() {
		var r NodeResult
		if err := rows.Scan(&r.ID, &r.Type, &r.Title, &r.Status); err != nil {
			return nil, fmt.Errorf("scan node: %w", err)
		}
		resolved[r.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Architecture cards come from the canonical HTML store, not SQL.
	archs := q.archLookupOrEmpty()
	for _, id := range ids {
		slug, ok := corearch.ArchSlugFromNodeID(id)
		if !ok {
			continue
		}
		if c := archs.get(slug); c != nil {
			resolved[id] = archNodeResult(c)
		}
	}

	// Return in input order, including unresolved IDs with minimal info.
	results := make([]NodeResult, 0, len(ids))
	for _, id := range ids {
		if r, ok := resolved[id]; ok {
			results = append(results, r)
		} else {
			results = append(results, NodeResult{ID: id})
		}
	}
	return results, nil
}
