package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Ranking weight constants — tunable.
const (
	weightFileMention  = 2.0 // each ripgrep hit inside HTML content
	weightCommitItem   = 3.0 // commit attributed to this item via wipnote-item trailer
	weightStatusActive = 1.5 // bonus for in-progress items
	weightStatusDone   = 0.2 // small weight for done items (still relevant as context)
)

// defaultRelevantLimit bounds the default result set so `wipnote relevant`
// stays agent-context-friendly (GH#151). Unbounded output is opt-in via
// --limit 0.
const defaultRelevantLimit = 15

// queryType classifies the user's query.
type queryType int

const (
	queryTypeKeyword queryType = iota
	queryTypeFile
	queryTypeSHA
)

// shaPattern matches 7–40 lowercase hex characters (git SHA prefix or full).
var shaPattern = regexp.MustCompile(`^[0-9a-f]{7,40}$`)

// knownExtensions is used to classify path-like queries without a slash.
var knownExtensions = map[string]bool{
	".go": true, ".md": true, ".html": true, ".yaml": true, ".yml": true,
	".json": true, ".sh": true, ".ts": true, ".tsx": true, ".js": true,
	".py": true, ".toml": true, ".txt": true,
}

// citation is a source reference supporting a match.
type citation struct {
	File    string `json:"file,omitempty"`
	Line    int    `json:"line,omitempty"`
	Snippet string `json:"snippet,omitempty"`
	SHA     string `json:"sha,omitempty"`
	Date    string `json:"date,omitempty"`
	Subject string `json:"subject,omitempty"`
}

// relevantResult is one ranked work-item match.
type relevantResult struct {
	ID        string     `json:"id"`
	Type      string     `json:"type"`
	Title     string     `json:"title"`
	Status    string     `json:"status"`
	Score     float64    `json:"score"`
	Citations []citation `json:"citations"`
}

// relevantOptions holds the parsed CLI flags controlling output size/shape.
type relevantOptions struct {
	format       string
	compact      bool
	limit        int
	typeFilter   string
	statusFilter string
	minScore     float64
}

func relevantCmd() *cobra.Command {
	var opts relevantOptions

	cmd := &cobra.Command{
		Use:   "relevant <query>",
		Short: "Find work items related to a file path, git SHA, or keyword",
		Long: `Retrieval-first search: one call returns ranked, cited work-item matches.

Query auto-detection:
  file path  — contains '/' or ends with a known extension, or the path exists
  git SHA    — 7–40 lowercase hex characters
  keyword    — anything else

Reads directly from .wipnote/*.html via ripgrep + git log.
SQLite is not consulted.

Output is bounded by default (top-ranked matches only) to stay agent-context
friendly. Pass --limit 0 to opt in to an unbounded dump.

Examples:
  wipnote relevant cmd/wipnote/relevant.go
  wipnote relevant "retrieval"
  wipnote relevant abc1234
  wipnote relevant "retrieval" --compact --limit 5
  wipnote relevant "retrieval" --type feature --status in-progress
  wipnote relevant "retrieval" --min-score 2.5 --limit 0`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			opts.format = effectiveFormat(opts.format, opts.compact, cmd.Flags().Changed("format"))
			return runRelevant(args[0], opts)
		},
	}

	isTTY := isTerminal()
	defaultFmt := "json"
	if isTTY {
		defaultFmt = "text"
	}
	cmd.Flags().StringVar(&opts.format, "format", defaultFmt, "Output format: json or text")
	cmd.Flags().BoolVar(&opts.compact, "compact", false, "One line per item: id, type, status, score, title")
	cmd.Flags().IntVar(&opts.limit, "limit", defaultRelevantLimit, "Max results to show (0 = unbounded)")
	cmd.Flags().StringVar(&opts.typeFilter, "type", "", "Filter by work-item type (feature, bug, spike, track, plan, spec, recap)")
	cmd.Flags().StringVar(&opts.statusFilter, "status", "", "Filter by status (e.g. todo, in-progress, done, blocked)")
	cmd.Flags().Float64Var(&opts.minScore, "min-score", 0, "Minimum relevance score threshold")
	return cmd
}

// effectiveFormat resolves the actual output format to use. When stdout is
// not a TTY, --format defaults to "json" — but if the user explicitly passed
// --compact and did NOT explicitly pass --format, a bare `--compact` in a
// piped invocation would otherwise silently produce JSON instead of the
// compact lines the user asked for. --compact only yields to an *explicit*
// --format json.
func effectiveFormat(format string, compact, formatExplicit bool) string {
	if compact && !formatExplicit && format == "json" {
		return "text"
	}
	return format
}

// isTerminal returns true when stdout is a TTY.
func isTerminal() bool {
	fi, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return (fi.Mode() & os.ModeCharDevice) != 0
}

// detectQueryType classifies query as file path, git SHA, or keyword.
func detectQueryType(query string) queryType {
	// Explicit path separators.
	if strings.Contains(query, "/") {
		return queryTypeFile
	}
	// Known file extension suffix.
	ext := strings.ToLower(filepath.Ext(query))
	if ext != "" && knownExtensions[ext] {
		return queryTypeFile
	}
	// File exists on disk.
	if _, err := os.Stat(query); err == nil {
		return queryTypeFile
	}
	// Ends with slash (directory).
	if strings.HasSuffix(query, "/") {
		return queryTypeFile
	}
	// SHA pattern.
	if shaPattern.MatchString(strings.ToLower(query)) {
		return queryTypeSHA
	}
	return queryTypeKeyword
}

// runRelevant is the main entry point for the relevant command. Output is
// bounded/scoped per GH#151: rank, filter by type/status/min-score, then cap
// at opts.limit (0 = unbounded, an explicit opt-in to the full dump).
func runRelevant(query string, opts relevantOptions) error {
	hgDir, err := findWipnoteDir()
	if err != nil {
		return err
	}

	qType := detectQueryType(query)
	results, err := runRelevantSearch(hgDir, query, qType)
	if err != nil {
		return err
	}

	results = rankResults(results, qType)
	results = filterResults(results, opts.typeFilter, opts.statusFilter, opts.minScore)

	total := len(results)
	shown := applyLimit(results, opts.limit)

	switch {
	case opts.format == "json":
		return printRelevantJSON(shown, len(shown), total)
	case opts.compact:
		printRelevantCompact(shown, len(shown), total)
		return nil
	default:
		printRelevantText(shown, len(shown), total)
		return nil
	}
}

// filterResults returns the subset of results matching all active filters.
// Empty typeFilter/statusFilter or a zero minScore skip that filter. Matches
// are exact (case-sensitive), consistent with `wipnote find`'s --status filter.
func filterResults(results []relevantResult, typeFilter, statusFilter string, minScore float64) []relevantResult {
	if typeFilter == "" && statusFilter == "" && minScore == 0 {
		return results
	}
	out := make([]relevantResult, 0, len(results))
	for _, r := range results {
		if typeFilter != "" && r.Type != typeFilter {
			continue
		}
		if statusFilter != "" && r.Status != statusFilter {
			continue
		}
		if r.Score < minScore {
			continue
		}
		out = append(out, r)
	}
	return out
}

// applyLimit returns the top `limit` results. limit <= 0 means unbounded —
// the explicit opt-in to a full dump (GH#151).
func applyLimit(results []relevantResult, limit int) []relevantResult {
	if limit <= 0 || limit >= len(results) {
		return results
	}
	return results[:limit]
}

// truncationSummary returns a one-line, non-silent notice when shown < total,
// or "" when nothing was truncated.
func truncationSummary(shown, total int) string {
	if shown >= total {
		return ""
	}
	return fmt.Sprintf("showing %d of %d — raise --limit (or pass --limit 0) to see more", shown, total)
}

// runRelevantSearch performs the multi-source retrieval pipeline and returns
// unranked (or lightly scored) results. Exported for tests.
func runRelevantSearch(hgDir, query string, qType queryType) ([]relevantResult, error) {
	projectDir := filepath.Dir(hgDir)
	scores := make(map[string]*relevantResult)

	// Step 1: ripgrep search over HTML files.
	//
	// For keyword queries we tokenize on whitespace and call searchWithRipgrep
	// once per token, accumulating scores in the same map. A literal
	// multi-token phrase like "lineage review" matches nothing even when each
	// token individually matches several items, so we score tokens
	// independently. Items that match multiple tokens naturally rise to the
	// top via repeated weightFileMention additions.
	rgTokens := []string{query}
	switch qType {
	case queryTypeSHA:
		// SHAs are single tokens by definition.
		rgTokens = []string{query}
	case queryTypeFile:
		// File-path queries use the base name as the search token.
		rgTokens = []string{filepath.Base(query)}
	case queryTypeKeyword:
		rgTokens = tokenizeQuery(query)
	}

	// rgMissing degrades gracefully instead of hard-failing the whole command:
	// once we know `rg` isn't on PATH, stop retrying per token, warn once on
	// stderr, and fall through to step 2 (git log attribution), which still
	// works for file/SHA queries without ripgrep.
	rgMissing := false
	for _, tok := range rgTokens {
		if tok == "" || rgMissing {
			continue
		}
		if err := searchWithRipgrep(hgDir, tok, scores); err != nil {
			if isCommandNotFound(err) {
				rgMissing = true
				fmt.Fprintln(os.Stderr, "warning: ripgrep (rg) not found in PATH — degraded mode: results limited to git log attribution (file/SHA queries only). Install rg for full keyword search: https://github.com/BurntSushi/ripgrep")
				continue
			}
			// Other errors are non-fatal: rg may have hit a transient issue. Continue with git-only attribution.
		}
	}

	// Step 2: git log attribution (commit trailer + file history).
	switch qType {
	case queryTypeFile:
		if err := searchViaGitFileHistory(projectDir, hgDir, query, scores); err != nil {
			// Non-fatal: git may not be available or file may be untracked.
			_ = err
		}
	case queryTypeSHA:
		if err := searchViaGitSHA(projectDir, hgDir, query, scores); err != nil {
			_ = err
		}
	}

	// Step 3: apply status bonuses to all collected items.
	for _, r := range scores {
		switch r.Status {
		case "in-progress":
			r.Score += weightStatusActive
		case "done":
			r.Score += weightStatusDone
		}
	}

	// Collect into slice.
	var out []relevantResult
	for _, r := range scores {
		out = append(out, *r)
	}
	return out, nil
}

// rankResults sorts results by score descending. For KEYWORD queries only,
// recap artifacts are ranked below work items at every score.
//
// Recaps embed the full git diff of a change as HTML, so their text matches
// almost any query term and they score near the ceiling on unrelated keyword
// searches (bug-3af1d597: every top hit for "performance", "benchmark" and
// "telemetry" was one recap file citing diff lines, pushing the actual
// features, bugs, spikes and tracks below the fold). That defeats the
// documented first step of `wipnote relevant <topic>` — finding existing
// lineage before creating a work item — because the lineage is what gets buried.
//
// The demotion is scoped to keyword queries because the same property inverts
// for the other two query kinds: for a SHA or a file path, the recap grounded
// on exactly that commit range or file IS the precise answer, and demoting it
// below every loosely-matching work item pushes it past --limit and reports
// "no recap" for a commit that has one. Diff text is noise when the query is a
// topic and signal when the query is a location.
func rankResults(results []relevantResult, qType queryType) []relevantResult {
	demoteRecaps := qType == queryTypeKeyword
	sort.SliceStable(results, func(i, j int) bool {
		if demoteRecaps {
			iRecap, jRecap := results[i].Type == "recap", results[j].Type == "recap"
			if iRecap != jRecap {
				return jRecap // non-recap first
			}
		}
		return results[i].Score > results[j].Score
	})
	return results
}

// isCommandNotFound returns true when the error indicates a missing executable.
func isCommandNotFound(err error) bool {
	if err == nil {
		return false
	}
	var execErr *exec.Error
	if ok := isExecError(err, &execErr); ok {
		return execErr.Err == exec.ErrNotFound
	}
	return strings.Contains(err.Error(), "executable file not found")
}

func isExecError(err error, target **exec.Error) bool {
	if e, ok := err.(*exec.Error); ok {
		*target = e
		return true
	}
	return false
}
