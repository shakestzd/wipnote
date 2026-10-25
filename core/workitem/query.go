package workitem

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
)

// --- Active work item lookup -------------------------------------------------

// WorkItem is a type-agnostic representation of an active work item.
// Returned by GetActiveWorkItem to avoid callers needing to know which
// collection the item belongs to.
type WorkItem struct {
	ID     string
	Type   string // "feature", "bug", or "spike"
	Title  string
	Status string
}

// GetActiveWorkItem scans features, bugs, and spikes for the first
// work item with status "in-progress" and returns it.
// Returns (nil, nil) if no active work item is found.
func GetActiveWorkItem(projectDir string) (*WorkItem, error) {
	dirs := []struct {
		path     string
		nodeType string
	}{
		{filepath.Join(projectDir, "features"), "feature"},
		{filepath.Join(projectDir, "bugs"), "bug"},
		{filepath.Join(projectDir, "spikes"), "spike"},
	}

	for _, d := range dirs {
		item, err := findActiveInDir(d.path, d.nodeType)
		if err != nil {
			return nil, err
		}
		if item != nil {
			return item, nil
		}
	}
	return nil, nil
}

// findActiveInDir scans a directory for the first in-progress node
// of the given type. Supports both flat format (id.html) and subdirectory
// format (id/index.html).
func findActiveInDir(dir, nodeType string) (*WorkItem, error) {
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		return nil, nil
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		var path string
		if entry.IsDir() {
			// Try subdirectory format: id/index.html
			path = filepath.Join(dir, entry.Name(), "index.html")
			if _, err := os.Stat(path); err != nil {
				continue
			}
		} else if !strings.HasSuffix(entry.Name(), ".html") {
			// Skip non-HTML files
			continue
		} else {
			// Flat format: id.html
			path = filepath.Join(dir, entry.Name())
		}

		node, err := htmlparse.ParseFile(path)
		if err != nil {
			continue // skip unparseable files
		}
		if node.Status != models.StatusInProgress || node.Type != nodeType {
			continue
		}
		return &WorkItem{
			ID:     node.ID,
			Type:   node.Type,
			Title:  node.Title,
			Status: string(node.Status),
		}, nil
	}
	return nil, nil
}
