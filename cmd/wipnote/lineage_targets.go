package main

import (
	"database/sql"
	"fmt"
	"io"
	"strings"

	dbpkg "github.com/shakestzd/wipnote/core/db"
	"github.com/shakestzd/wipnote/core/slug"
)

// runLineageCommit dispatches a commit SHA to the existing TraceCommit
// primitive and renders the result. Commits are not graph_edges nodes, so a
// bidirectional bfsWalk would return empty — this is the correct surface.
func runLineageCommit(w io.Writer, db *sql.DB, sha string, opts lineageOpts) error {
	if opts.timelineSet || opts.depthSet {
		return fmt.Errorf("--timeline and --depth are not supported for commit inputs; use `wipnote lineage <work-item-id>` for graph traversal")
	}
	commits, err := dbpkg.TraceCommit(db, sha)
	if err != nil {
		return fmt.Errorf("trace commit: %w", err)
	}
	if opts.jsonOut {
		out := struct {
			Root    string              `json:"root"`
			Kind    string              `json:"kind"`
			Commits []dbpkg.TraceResult `json:"commits"`
		}{Root: sha, Kind: kindCommit.String(), Commits: commits}
		return writeJSON(w, out)
	}
	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Lineage: %s  [commit]\n", slug.Truncate(sha, 10))
	fmt.Fprintln(w, sep)
	if len(commits) == 0 {
		fmt.Fprintln(w, "  (no matching commit — run 'wipnote ingest commits')")
		return nil
	}
	for _, c := range commits {
		fmt.Fprintf(w, "  Commit    %s\n", slug.Truncate(c.CommitHash, 10))
		if c.Message != "" {
			fmt.Fprintf(w, "  Message   %s\n", slug.Truncate(c.Message, 55))
		}
		if c.SessionID != "" {
			fmt.Fprintf(w, "  Session   %s\n", c.SessionID)
		}
		if c.FeatureID != "" {
			fmt.Fprintf(w, "  Feature   %s\n", c.FeatureID)
		}
		if c.TrackID != "" {
			fmt.Fprintf(w, "  Track     %s\n", c.TrackID)
		}
	}
	return nil
}

// runLineageFile dispatches a file path to the existing TraceFile primitive
// and renders the result. Same rationale as runLineageCommit: files are not
// graph_edges nodes.
func runLineageFile(w io.Writer, db *sql.DB, filePath string, opts lineageOpts) error {
	if opts.timelineSet || opts.depthSet {
		return fmt.Errorf("--timeline and --depth are not supported for file inputs; use `wipnote lineage <work-item-id>` for graph traversal")
	}
	results, err := dbpkg.TraceFile(db, filePath)
	if err != nil {
		return fmt.Errorf("trace file: %w", err)
	}
	if opts.jsonOut {
		out := struct {
			Root     string                  `json:"root"`
			Kind     string                  `json:"kind"`
			Features []dbpkg.FileTraceResult `json:"features"`
		}{Root: filePath, Kind: kindFile.String(), Features: results}
		return writeJSON(w, out)
	}
	sep := strings.Repeat("─", 60)
	fmt.Fprintln(w, sep)
	fmt.Fprintf(w, "  Lineage: %s  [file]\n", filePath)
	fmt.Fprintln(w, sep)
	if len(results) == 0 {
		fmt.Fprintln(w, "  (no features touch this file — run 'wipnote reindex')")
		return nil
	}
	fmt.Fprintf(w, "\n  Features (%d):\n", len(results))
	for _, r := range results {
		status := r.Status
		if status == "" {
			status = "unknown"
		}
		fmt.Fprintf(w, "    %s  [%s]  %s\n", r.FeatureID, status, slug.Truncate(r.Title, 40))
		if r.TrackID != "" {
			fmt.Fprintf(w, "      Track: %s\n", r.TrackID)
		}
	}
	return nil
}
