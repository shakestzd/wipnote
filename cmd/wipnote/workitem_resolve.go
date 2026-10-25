package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/shakestzd/wipnote/core/graph"
	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/workitem"
)

// resolveNodeByUnionID resolves a partial or full ID against both live files and
// archived ledger entries. Exact matches (live or archived) win outright and are
// never ambiguous, even if they are also a prefix of other IDs. Prefix matches
// that span both live and archived sources return an ambiguity error. This ensures
// that a prefix matching one live item and one archived item correctly reports
// ambiguity rather than silently picking the live one.
func resolveNodeByUnionID(wipnoteDir, id string) (*models.Node, error) {
	// 1. Check for exact match in live files.
	path := workitem.NodePath(wipnoteDir, id)
	if path != "" {
		parsed, parseErr := htmlparse.ParseFile(path)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		return parsed, nil
	}

	// 2. Check for exact match in archived ledgers.
	archived, archErr := resolveArchivedNode(wipnoteDir, id)
	if archErr != nil {
		return nil, archErr
	}
	if archived != nil {
		return archived, nil
	}

	// 3-5. No exact match found. Collect partial matches from live files and
	// archived ledgers.
	allMatches, err := unionPrefixMatches(wipnoteDir, id)
	if err != nil {
		return nil, err
	}

	switch len(allMatches) {
	case 0:
		kind := workitem.KindForID(id)
		return nil, workitem.ErrNotFoundOnDisk(kind, id)
	case 1:
		return resolveUniqueMatch(wipnoteDir, allMatches[0])
	default:
		return nil, fmt.Errorf("ambiguous ID %q — did you mean one of: %s",
			id, strings.Join(allMatches, ", "))
	}
}

// unionPrefixMatches collects all partial matches from live files and
// archived ledgers, then combines and deduplicates them (sorted).
func unionPrefixMatches(wipnoteDir, id string) ([]string, error) {
	// 3. Collect all partial matches from live files.
	liveMatches, liveErr := partialLiveMatches(wipnoteDir, id)
	if liveErr != nil {
		return nil, liveErr
	}

	// 4. Collect all partial matches from archived ledgers.
	archivedMatches, archErr := partialArchivedMatches(wipnoteDir, id)
	if archErr != nil {
		return nil, archErr
	}

	// 5. Combine and deduplicate prefix matches.
	allMatches := append(liveMatches, archivedMatches...)
	sort.Strings(allMatches)
	return dedupMatches(allMatches), nil
}

// resolveUniqueMatch loads the single prefix match, trying the live file first,
// then the archive.
func resolveUniqueMatch(wipnoteDir, matched string) (*models.Node, error) {
	path := workitem.NodePath(wipnoteDir, matched)
	if path != "" {
		parsed, parseErr := htmlparse.ParseFile(path)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w", path, parseErr)
		}
		return parsed, nil
	}
	// Not in live files, try archive.
	archived, archErr := resolveArchivedNode(wipnoteDir, matched)
	if archErr != nil {
		return nil, archErr
	}
	if archived != nil {
		return archived, nil
	}
	// Shouldn't happen, but handle gracefully.
	kind := workitem.KindForID(matched)
	return nil, workitem.ErrNotFoundOnDisk(kind, matched)
}

// partialLiveMatches returns all live IDs that start with prefix, or an error
// if a collection directory cannot be read (except for not-found directories,
// which are skipped). This propagates unexpected scan failures so corruption
// is surfaced rather than silently treated as no-match.
func partialLiveMatches(wipnoteDir, prefix string) ([]string, error) {
	var matches []string
	dirs := []string{"features", "bugs", "spikes", "tracks", "plans", "specs"}
	for _, sub := range dirs {
		dir := filepath.Join(wipnoteDir, sub)
		entries, err := os.ReadDir(dir)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, fmt.Errorf("scan %s: %w", sub, err)
		}
		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}
			name := entry.Name()
			if !strings.HasSuffix(name, ".html") {
				continue
			}
			stem := strings.TrimSuffix(name, ".html")
			if strings.HasPrefix(stem, prefix) {
				matches = append(matches, stem)
			}
		}
	}
	return matches, nil
}

// partialArchivedMatches returns all archived IDs that start with prefix.
func partialArchivedMatches(wipnoteDir, prefix string) ([]string, error) {
	var matches []string
	for _, col := range graph.ArchiveLedgerCollections {
		entries, err := graph.ReadLedger(graph.ArchiveLedgerPath(wipnoteDir, col))
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return nil, err
		}
		for _, e := range entries {
			if strings.HasPrefix(e.ID, prefix) {
				matches = append(matches, e.ID)
			}
		}
	}
	return matches, nil
}

// dedupMatches removes consecutive duplicate strings from a sorted slice.
func dedupMatches(matches []string) []string {
	if len(matches) <= 1 {
		return matches
	}
	unique := make([]string, 1, len(matches))
	unique[0] = matches[0]
	for i := 1; i < len(matches); i++ {
		if matches[i] != matches[i-1] {
			unique = append(unique, matches[i])
		}
	}
	return unique
}
