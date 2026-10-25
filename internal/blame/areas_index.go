package blame

import (
	"context"
	"database/sql"
	"fmt"
	"sort"
	"time"
)

// fileIndex answers Query(ctx, db, rel, QueryOptions{}) for every tracked file
// of one WalkAreas from a single grouped scan of feature_files (feat-c30d0792).
//
// Query matches a stored path p to rel when p == rel (case-sensitive) or
// p LIKE '%/rel' — any prefix ending in a literal '/', with SQLite's LIKE
// folding ASCII case only. Issued once per tracked file, that LIKE is a full
// table scan each time, so a large tree did not finish in half an hour. Here
// each stored path is resolved the other way round: its suffixes after every
// '/' are looked up in the tracked-file set, so the cost is one scan plus a
// map lookup per path segment.
type fileIndex struct {
	byFile map[string]map[string]*FeatureRow // rel → feature id → aggregated row
}

// indexRow is one (feature, stored path) group from the scan.
type indexRow struct {
	featureID, path, title, trackID, trackTitle string
	touches                                     int
	lastSeen                                    sql.NullString
}

const indexQuery = `
	SELECT ff.feature_id, ff.file_path,
	       COALESCE(f.title, ''), COALESCE(f.track_id, ''), COALESCE(t.title, ''),
	       COUNT(*), MAX(ff.last_seen)
	FROM feature_files ff
	LEFT JOIN features f ON f.id = ff.feature_id
	LEFT JOIN tracks t ON t.id = f.track_id
	GROUP BY ff.feature_id, ff.file_path`

// loadFileIndex scans feature_files once and attributes each row to the
// tracked files it would have matched under Query.
func loadFileIndex(ctx context.Context, database *sql.DB, files []string) (*fileIndex, error) {
	exact := make(map[string]bool, len(files))
	folded := make(map[string][]string, len(files))
	for _, rel := range files {
		exact[rel] = true
		key := asciiLower(rel)
		folded[key] = append(folded[key], rel)
	}
	rows, err := queryRows(ctx, database, indexQuery)
	if err != nil {
		return nil, fmt.Errorf("blame index query: %w", err)
	}
	defer rows.Close()
	idx := &fileIndex{byFile: map[string]map[string]*FeatureRow{}}
	for rows.Next() {
		var r indexRow
		if err := rows.Scan(&r.featureID, &r.path, &r.title, &r.trackID, &r.trackTitle, &r.touches, &r.lastSeen); err != nil {
			return nil, fmt.Errorf("scan blame index row: %w", err)
		}
		for rel := range matchedFiles(r.path, exact, folded) {
			idx.add(rel, r)
		}
	}
	return idx, rows.Err()
}

// matchedFiles returns the tracked files stored path p matches: p itself when
// tracked, and every tracked file equal (ASCII case-folded) to a suffix of p
// that follows a '/'.
func matchedFiles(p string, exact map[string]bool, folded map[string][]string) map[string]bool {
	out := map[string]bool{}
	if exact[p] {
		out[p] = true
	}
	for i := 0; i < len(p); i++ {
		if p[i] != '/' {
			continue
		}
		for _, rel := range folded[asciiLower(p[i+1:])] {
			out[rel] = true
		}
	}
	return out
}

// add folds one (feature, stored path) group into rel's per-feature row,
// mirroring Query's GROUP BY feature_id: touches sum, last_seen is the max.
func (x *fileIndex) add(rel string, r indexRow) {
	feats := x.byFile[rel]
	if feats == nil {
		feats = map[string]*FeatureRow{}
		x.byFile[rel] = feats
	}
	fr := feats[r.featureID]
	if fr == nil {
		fr = &FeatureRow{ID: r.featureID, Title: r.title, TrackID: r.trackID, TrackTitle: r.trackTitle}
		feats[r.featureID] = fr
	}
	fr.TouchCount += r.touches
	if r.lastSeen.Valid {
		if t := parseLastSeen(r.lastSeen.String); t.After(fr.LastSeen) {
			fr.LastSeen = t
		}
	}
}

// result returns rel's blame Result, shaped exactly as Query returns it.
func (x *fileIndex) result(rel string) *Result {
	rows := make([]FeatureRow, 0, len(x.byFile[rel]))
	total := 0
	for _, fr := range x.byFile[rel] {
		rows = append(rows, *fr)
		total += fr.TouchCount
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].TouchCount != rows[j].TouchCount {
			return rows[i].TouchCount > rows[j].TouchCount
		}
		if !rows[i].LastSeen.Equal(rows[j].LastSeen) {
			return rows[i].LastSeen.After(rows[j].LastSeen)
		}
		return rows[i].ID < rows[j].ID
	})
	tracks := rollupTracks(rows)
	if tracks == nil {
		tracks = []TrackRollup{}
	}
	return &Result{Path: rel, Features: rows, Tracks: tracks, TotalTouchCount: total}
}

// parseLastSeen parses a feature_files.last_seen value the way Query does.
func parseLastSeen(s string) time.Time {
	t, _ := time.Parse("2006-01-02 15:04:05", s)
	if t.IsZero() {
		t, _ = time.Parse(time.RFC3339, s)
	}
	return t
}

// asciiLower lower-cases ASCII letters only, matching SQLite's default LIKE.
func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if 'A' <= c && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}
