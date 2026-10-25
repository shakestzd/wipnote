package main

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	dbpkg "github.com/shakestzd/wipnote/core/db"
)

// reindexRecaps ingests every committed recap artifact from .wipnote/recaps/*.html
// into the recaps SQLite read index. Returns (total, upserted, errCount).
// This is a full reindex: rows for deleted/renamed recaps are purged so the
// table stays consistent with the canonical HTML files. The HTML stays
// authoritative — this table is a derived read index.
func reindexRecaps(database *sql.DB, wipnoteDir, projectDir string, verbose bool) (int, int, int) {
	recapsDir := filepath.Join(wipnoteDir, "recaps")
	entries, err := os.ReadDir(recapsDir)
	if err != nil {
		if os.IsNotExist(err) {
			// No recaps dir — purge any stale rows.
			_, _ = database.Exec(`DELETE FROM recaps`)
			return 0, 0, 0
		}
		if verbose {
			fmt.Printf("reindex recaps: read dir error: %s: %v\n", recapsDir, err)
		}
		return 0, 0, 1
	}

	diskIDs := make(map[string]struct{})
	var total, upserted, errCount int
	paths := recapPaths(recapsDir, entries)
	batch := batchGitFileTimestamps(projectDir, paths)

	for _, path := range paths {
		total++
		name := filepath.Base(path)
		id := strings.TrimSuffix(name, ".html")
		row, parseErr := parseRecapHTML(path, id)
		if parseErr != nil {
			errCount++
			if verbose {
				fmt.Printf("reindex recaps: error: %s: %v\n", name, parseErr)
			}
			continue
		}
		diskIDs[id] = struct{}{}

		c, u := recapTimestamps(batch, projectDir, path)
		row.CreatedAt, row.UpdatedAt = keepTime(c, row.CreatedAt), keepTime(u, row.UpdatedAt)

		if upsertErr := dbpkg.UpsertRecap(database, row); upsertErr != nil {
			errCount++
			if verbose {
				fmt.Printf("reindex recaps: upsert error: %s: %v\n", name, upsertErr)
			}
			continue
		}
		upserted++
	}

	purgeStaleRecaps(database, diskIDs, verbose)
	return total, upserted, errCount
}

// parseRecapHTML reads the data-recap-* attributes off the artifact's <body>
// element. The recap id is taken from the filename (it is not embedded in the
// HTML). work_item_id is derived from the id for work-item recaps.
func parseRecapHTML(path, id string) (*dbpkg.RecapRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		return nil, err
	}
	body := doc.Find("body").First()
	if body.Length() == 0 {
		return nil, fmt.Errorf("recap %s: no <body> element", id)
	}

	row := &dbpkg.RecapRow{
		ID:         id,
		Kind:       attrOrEmpty(body, "data-recap-kind"),
		Input:      attrOrEmpty(body, "data-recap-input"),
		GitRange:   attrOrEmpty(body, "data-recap-git-range"),
		Grounded:   attrOrEmpty(body, "data-recap-grounded") == "1",
		Title:      attrOrEmpty(body, "data-recap-title"),
		Outcome:    attrOrEmpty(body, "data-recap-outcome"),
		WorkItemID: recapWorkItemID(id),
	}
	return row, nil
}

// recapWorkItemID extracts the linked work-item id from a work-item recap id of
// the form recap-<workitem-id>. Range (recap-r-...) and session (recap-s-...)
// recaps have no work item, so they return "".
func recapWorkItemID(recapID string) string {
	rest, ok := strings.CutPrefix(recapID, "recap-")
	if !ok {
		return ""
	}
	if strings.HasPrefix(rest, "r-") || strings.HasPrefix(rest, "s-") {
		return ""
	}
	if isWorkItemID(rest) {
		return rest
	}
	return ""
}

// purgeStaleRecaps deletes recaps rows whose ids are not present on disk.
func purgeStaleRecaps(database *sql.DB, diskIDs map[string]struct{}, verbose bool) {
	rows, err := database.Query(`SELECT id FROM recaps`)
	if err != nil {
		return
	}
	defer rows.Close()
	var stale []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil {
			if _, ok := diskIDs[id]; !ok {
				stale = append(stale, id)
			}
		}
	}
	rows.Close()
	for _, id := range stale {
		if _, err := database.Exec(`DELETE FROM recaps WHERE id = ?`, id); err == nil && verbose {
			fmt.Printf("reindex recaps: purged stale row: %s\n", id)
		}
	}
}

// recapPaths lists the recap HTML files among entries, so their git
// timestamps resolve in one batch instead of two git processes per recap.
func recapPaths(recapsDir string, entries []os.DirEntry) []string {
	var paths []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".html") {
			paths = append(paths, filepath.Join(recapsDir, e.Name()))
		}
	}
	return paths
}

// recapTimestamps returns a recap's git (created, updated) times from batch,
// falling back to per-file git calls, and nil for a time git does not know.
func recapTimestamps(batch map[string]fileTimestamps, projectDir, path string) (created, updated *time.Time) {
	c, u, ok := timestampsFromBatch(batch, path, time.Time{}, time.Time{})
	if !ok {
		c, u = applyGitTimestamps(projectDir, path, time.Time{}, time.Time{})
	}
	if !c.IsZero() {
		created = &c
	}
	if !u.IsZero() {
		updated = &u
	}
	return created, updated
}

// keepTime returns git when git knows the time, else the parsed value.
func keepTime(git, parsed *time.Time) *time.Time {
	if git != nil {
		return git
	}
	return parsed
}
