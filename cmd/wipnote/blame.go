package main

import (
	"context"
	"fmt"
	"time"

	"github.com/shakestzd/wipnote/internal/blame"
	"github.com/spf13/cobra"
)

// blameOpts holds the parsed CLI flags for `wipnote blame`.
type blameOpts struct {
	format string
	since  string
	top    int
}

// blameCmd returns the cobra command for `wipnote blame <path>`.
func blameCmd() *cobra.Command {
	var opts blameOpts

	cmd := &cobra.Command{
		Use:   "blame <path>",
		Short: "Reverse-lookup which features and tracks touched a given file",
		Long: `Queries the feature_files index and rolls up by track.

Given a file path, blame lists every feature that has touched the file,
grouped and rolled up by track, with touch counts and last-seen timestamps.

Output formats: text (default), json, markdown

Examples:
  wipnote blame internal/db/schema.go
  wipnote blame cmd/wipnote/main.go --format json
  wipnote blame internal/db/schema.go --format markdown --top 5
  wipnote blame internal/db/schema.go --since 2025-01-01`,
		Args: cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			return runBlame(args[0], opts)
		},
	}

	cmd.Flags().StringVar(&opts.format, "format", "text", "output format: text, json, or markdown")
	cmd.Flags().StringVar(&opts.since, "since", "", "filter to touches after this date (YYYY-MM-DD or RFC3339)")
	cmd.Flags().IntVar(&opts.top, "top", 0, "limit to top N features by touch count (0 = unlimited)")

	return cmd
}

// runBlame opens the database, calls blame.Query, and dispatches to the formatter.
func runBlame(path string, opts blameOpts) error {
	dir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	db, err := openDB(dir)
	if err != nil {
		return err
	}
	defer db.Close()

	qopts := blame.QueryOptions{Top: opts.top}
	if opts.since != "" {
		t, parseErr := parseSinceDate(opts.since)
		if parseErr != nil {
			return fmt.Errorf("--since: %w", parseErr)
		}
		qopts.Since = &t
	}

	result, err := blame.Query(context.Background(), db, path, qopts)
	if err != nil {
		return err
	}

	switch opts.format {
	case "json":
		data, marshalErr := blame.FormatJSON(result)
		if marshalErr != nil {
			return fmt.Errorf("format json: %w", marshalErr)
		}
		fmt.Println(string(data))
	case "markdown":
		fmt.Print(blame.FormatMarkdown(result))
	case "text", "":
		fmt.Print(blame.FormatText(result))
	default:
		return fmt.Errorf("unknown format %q: use text, json, or markdown", opts.format)
	}

	return nil
}

// parseSinceDate parses a date string as YYYY-MM-DD first, then RFC3339.
func parseSinceDate(s string) (time.Time, error) {
	if t, err := time.Parse("2006-01-02", s); err == nil {
		return t.UTC(), nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("cannot parse %q as YYYY-MM-DD or RFC3339", s)
	}
	return t.UTC(), err
}
