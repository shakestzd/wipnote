// Package blame implements the reverse-lookup primitive: given a file path,
// return the features and tracks that touched it, rolled up with touch counts.
//
// Schema notes:
//   - feature_files(id, feature_id, file_path, operation, session_id,
//     first_seen, last_seen, created_at) — one row per (feature_id, file_path)
//   - features(id, type, title, status, priority, track_id, …)
//   - tracks(id, type, title, …)
//
// Query path: file_path → feature_files → features → tracks → rollup
// There are no lines_added/lines_removed columns, so TouchCount (number of
// feature_files rows) is used as a proxy for "how much work touched this file".
package blame

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"text/tabwriter"
	"time"
)

// QueryOptions controls optional filters on a blame query.
type QueryOptions struct {
	Since *time.Time // if non-nil, filter to feature_files rows last_seen >= Since
	Top   int        // 0 = unlimited; positive N = return only top N features
}

// FeatureRow describes a single feature that touched the queried path.
type FeatureRow struct {
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	TrackID      string    `json:"track_id"`
	TrackTitle   string    `json:"track_title"`
	TouchCount   int       `json:"touch_count"`   // proxy for lines_added
	LastSeen     time.Time `json:"last_seen"`
}

// TrackRollup aggregates all features in a track that touched the queried path.
type TrackRollup struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	FeatureCount int    `json:"feature_count"`
	TouchCount   int    `json:"touch_count"` // sum of feature TouchCount within track
}

// Result holds the complete blame output for a single file path.
type Result struct {
	Path            string        `json:"path"`
	Features        []FeatureRow  `json:"features"`
	Tracks          []TrackRollup `json:"tracks"`
	TotalTouchCount int           `json:"total_touch_count"`
}

// Query runs the blame lookup for path and returns the aggregated Result.
// Returns a Result with empty slices (not an error) when no features touched path.
// Returns an error only on DB failures or unknown path under .htmlgraph/.
func Query(ctx context.Context, db *sql.DB, path string, opts QueryOptions) (*Result, error) {
	if strings.Contains(path, ".htmlgraph/") || strings.HasPrefix(path, ".htmlgraph") {
		return nil, fmt.Errorf("path %q is under .htmlgraph/ — use `htmlgraph` CLI commands to inspect work items, not direct file paths", path)
	}

	rows, err := queryFeatureRows(ctx, db, path, opts)
	if err != nil {
		return nil, err
	}

	if opts.Top > 0 && len(rows) > opts.Top {
		rows = rows[:opts.Top]
	}

	tracks := rollupTracks(rows)

	total := 0
	for _, r := range rows {
		total += r.TouchCount
	}

	// Ensure slices marshal as [] not null in JSON.
	if rows == nil {
		rows = []FeatureRow{}
	}
	if tracks == nil {
		tracks = []TrackRollup{}
	}

	return &Result{
		Path:            path,
		Features:        rows,
		Tracks:          tracks,
		TotalTouchCount: total,
	}, nil
}

// escapeLikePattern escapes the SQL LIKE wildcards %, _, and the escape
// character itself (\) so a path containing those characters is treated as a
// literal string rather than a pattern. Pairs with `ESCAPE '\'` in the query.
func escapeLikePattern(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r == '\\' || r == '%' || r == '_' {
			b.WriteRune('\\')
		}
		b.WriteRune(r)
	}
	return b.String()
}

// queryFeatureRows fetches the raw per-feature rows for path.
//
// Path matching: feature_files currently stores absolute paths (with worktree
// prefix); see feat-79d030c4 for the canonical write-time normalization fix.
// Until that lands, blame matches both exact and suffix ("ends with /<path>"
// or "= <path>") to handle absolute, relative, and worktree-prefixed inputs.
func queryFeatureRows(ctx context.Context, db *sql.DB, path string, opts QueryOptions) ([]FeatureRow, error) {
	qb := strings.Builder{}
	args := []any{}

	qb.WriteString(`
		SELECT ff.feature_id,
		       COALESCE(f.title, ''),
		       COALESCE(f.track_id, ''),
		       COALESCE(t.title, ''),
		       COUNT(*) AS touch_count,
		       MAX(ff.last_seen) AS last_seen
		FROM feature_files ff
		LEFT JOIN features f ON f.id = ff.feature_id
		LEFT JOIN tracks t ON t.id = f.track_id
		WHERE (ff.file_path = ? OR ff.file_path LIKE ? ESCAPE '\')`)
	args = append(args, path, "%/"+escapeLikePattern(path))

	if opts.Since != nil {
		qb.WriteString(` AND ff.last_seen >= ?`)
		args = append(args, opts.Since.UTC().Format("2006-01-02 15:04:05"))
	}

	qb.WriteString(`
		GROUP BY ff.feature_id
		ORDER BY touch_count DESC, last_seen DESC`)

	sqlRows, err := db.QueryContext(ctx, qb.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("blame query for %s: %w", path, err)
	}
	defer sqlRows.Close()

	var out []FeatureRow
	for sqlRows.Next() {
		var r FeatureRow
		var lastSeenStr string
		if err := sqlRows.Scan(&r.ID, &r.Title, &r.TrackID, &r.TrackTitle, &r.TouchCount, &lastSeenStr); err != nil {
			return nil, fmt.Errorf("scan blame row: %w", err)
		}
		r.LastSeen, _ = time.Parse("2006-01-02 15:04:05", lastSeenStr)
		if r.LastSeen.IsZero() {
			r.LastSeen, _ = time.Parse(time.RFC3339, lastSeenStr)
		}
		out = append(out, r)
	}
	return out, sqlRows.Err()
}

// rollupTracks aggregates FeatureRow entries by track, sorted by total touch count desc.
func rollupTracks(rows []FeatureRow) []TrackRollup {
	byTrack := make(map[string]*TrackRollup)
	for _, r := range rows {
		tid := r.TrackID
		if tid == "" {
			tid = "(untracked)"
		}
		tr, ok := byTrack[tid]
		if !ok {
			title := r.TrackTitle
			if title == "" {
				title = "(untracked)"
			}
			tr = &TrackRollup{ID: tid, Title: title}
			byTrack[tid] = tr
		}
		tr.FeatureCount++
		tr.TouchCount += r.TouchCount
	}

	out := make([]TrackRollup, 0, len(byTrack))
	for _, tr := range byTrack {
		out = append(out, *tr)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TouchCount != out[j].TouchCount {
			return out[i].TouchCount > out[j].TouchCount
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// FormatText renders the Result as a human-readable table using tabwriter.
func FormatText(r *Result) string {
	var buf bytes.Buffer
	sep := strings.Repeat("─", 60)
	fmt.Fprintf(&buf, "%s\n", sep)
	fmt.Fprintf(&buf, "  Blame: %s\n", r.Path)
	fmt.Fprintf(&buf, "%s\n", sep)

	if len(r.Features) == 0 {
		fmt.Fprintln(&buf, "  No features have touched this file.")
		fmt.Fprintln(&buf, "  Run 'htmlgraph reindex' to rebuild file attribution.")
		return buf.String()
	}

	fmt.Fprintf(&buf, "\n  Total touches: %d  Features: %d  Tracks: %d\n\n",
		r.TotalTouchCount, len(r.Features), len(r.Tracks))

	// Tracks rollup table
	fmt.Fprintln(&buf, "  Tracks:")
	tw := tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "    ID\tTitle\tFeatures\tTouches")
	for _, t := range r.Tracks {
		fmt.Fprintf(tw, "    %s\t%s\t%d\t%d\n", t.ID, t.Title, t.FeatureCount, t.TouchCount)
	}
	tw.Flush()

	// Features table
	fmt.Fprintln(&buf, "\n  Features:")
	tw = tabwriter.NewWriter(&buf, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "    ID\tTitle\tTrack\tTouches\tLast Seen")
	for _, f := range r.Features {
		lastSeen := f.LastSeen.UTC().Format("2006-01-02")
		if f.LastSeen.IsZero() {
			lastSeen = "—"
		}
		trackLabel := f.TrackID
		if trackLabel == "" {
			trackLabel = "—"
		}
		fmt.Fprintf(tw, "    %s\t%s\t%s\t%d\t%s\n",
			f.ID, truncate(f.Title, 40), trackLabel, f.TouchCount, lastSeen)
	}
	tw.Flush()

	return buf.String()
}

// FormatJSON serialises the Result as indented JSON.
func FormatJSON(r *Result) ([]byte, error) {
	return json.MarshalIndent(r, "", "  ")
}

// FormatMarkdown renders the Result as Markdown.
func FormatMarkdown(r *Result) string {
	var buf bytes.Buffer
	fmt.Fprintf(&buf, "## File: %s\n\n", r.Path)

	if len(r.Features) == 0 {
		fmt.Fprintln(&buf, "_No features have touched this file._")
		return buf.String()
	}

	fmt.Fprintf(&buf, "**Total touches:** %d | **Features:** %d | **Tracks:** %d\n\n",
		r.TotalTouchCount, len(r.Features), len(r.Tracks))

	// Per-track sections
	for _, tr := range r.Tracks {
		fmt.Fprintf(&buf, "### Track: %s (%s)\n\n", tr.Title, tr.ID)
		fmt.Fprintln(&buf, "| Feature | Title | Touches | Last Seen |")
		fmt.Fprintln(&buf, "|---------|-------|---------|-----------|")
		for _, f := range r.Features {
			ftrackID := f.TrackID
			if ftrackID == "" {
				ftrackID = "(untracked)"
			}
			if ftrackID != tr.ID {
				continue
			}
			lastSeen := f.LastSeen.UTC().Format("2006-01-02")
			if f.LastSeen.IsZero() {
				lastSeen = "—"
			}
			fmt.Fprintf(&buf, "| %s | %s | %d | %s |\n", f.ID, f.Title, f.TouchCount, lastSeen)
		}
		fmt.Fprintln(&buf)
	}

	return buf.String()
}

// truncate shortens s to maxLen runes, appending "…" when cut.
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen-1]) + "…"
}
