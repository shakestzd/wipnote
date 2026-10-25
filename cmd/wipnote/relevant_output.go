package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/slug"
)

// relevantResponse is the top-level JSON payload. Wrapping (rather than a
// bare array) lets callers see shown/total/truncated without parsing a
// separate stderr notice — truncation is never silent (GH#151).
type relevantResponse struct {
	Results   []relevantResult `json:"results"`
	Shown     int              `json:"shown"`
	Total     int              `json:"total"`
	Truncated bool             `json:"truncated"`
}

// printRelevantJSON outputs results as a JSON object with shown/total/truncated metadata.
func printRelevantJSON(results []relevantResult, shown, total int) error {
	if results == nil {
		results = []relevantResult{}
	}
	resp := relevantResponse{
		Results:   results,
		Shown:     shown,
		Total:     total,
		Truncated: shown < total,
	}
	return printJSON(resp)
}

// printRelevantText outputs a human-readable ranked list, followed by a
// truncation summary line when shown < total (never silent — GH#151).
func printRelevantText(results []relevantResult, shown, total int) {
	if len(results) == 0 {
		fmt.Println("No matching work items found.")
		return
	}
	fmt.Printf("%-22s  %-8s  %-11s  %5s  %s\n", "ID", "TYPE", "STATUS", "SCORE", "TITLE")
	fmt.Println(strings.Repeat("-", 80))
	for _, r := range results {
		fmt.Printf("%-22s  %-8s  %-11s  %5.1f  %s\n",
			r.ID, r.Type, r.Status, r.Score, slug.Truncate(r.Title, 36))
		for _, c := range r.Citations {
			if c.SHA != "" {
				fmt.Printf("    [%s] %s  %s\n", c.SHA, c.Date, slug.Truncate(c.Subject, 60))
			} else if c.File != "" {
				fmt.Printf("    %s:%d  %s\n", filepath.Base(c.File), c.Line, c.Snippet)
			}
		}
	}
	if summary := truncationSummary(shown, total); summary != "" {
		fmt.Printf("\n%d item(s) — %s\n", len(results), summary)
		return
	}
	fmt.Printf("\n%d item(s)\n", len(results))
}

// printRelevantCompact outputs one line per item — id, type, status, score,
// title — with no citations, for the smallest possible agent-context
// footprint. Prints a truncation summary line when shown < total.
func printRelevantCompact(results []relevantResult, shown, total int) {
	if len(results) == 0 {
		fmt.Println("No matching work items found.")
		return
	}
	for _, r := range results {
		fmt.Printf("%s  %s  %s  %.1f  %s\n",
			r.ID, r.Type, r.Status, r.Score, slug.Truncate(r.Title, 60))
	}
	if summary := truncationSummary(shown, total); summary != "" {
		fmt.Println(summary)
	}
}
