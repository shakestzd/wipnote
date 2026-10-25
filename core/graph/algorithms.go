package graph

import (
	"database/sql"
	"fmt"
)

// resolveNodeIDs converts a list of IDs to NodeResults with metadata.
func resolveNodeIDs(db *sql.DB, archSrc ArchSource, ids []string) ([]NodeResult, error) {
	q := (&QueryBuilder{db: db}).WithArch(archSrc)
	return q.resolveNodes(ids)
}

// FormatNodeLabel returns "id (title)" or just "id" if title is empty.
func FormatNodeLabel(id string, results map[string]NodeResult) string {
	if r, ok := results[id]; ok && r.Title != "" {
		title := r.Title
		if len(title) > 40 {
			title = title[:39] + "…"
		}
		return fmt.Sprintf("%s (%s)", id, title)
	}
	return id
}

// ResolveToMap resolves a set of IDs and returns a lookup map.
// archSrc supplies architecture cards; pass nil when arch IDs cannot occur.
func ResolveToMap(db *sql.DB, archSrc ArchSource, ids []string) map[string]NodeResult {
	m := make(map[string]NodeResult, len(ids))
	results, err := resolveNodeIDs(db, archSrc, ids)
	if err != nil {
		return m
	}
	for _, r := range results {
		m[r.ID] = r
	}
	return m
}

// AllUniqueIDs extracts all unique IDs from a list of cycles.
func AllUniqueIDs(cycles [][]string) []string {
	seen := make(map[string]bool)
	var ids []string
	for _, cycle := range cycles {
		for _, id := range cycle {
			if !seen[id] {
				seen[id] = true
				ids = append(ids, id)
			}
		}
	}
	return ids
}
