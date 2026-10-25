package main

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/PuerkitoBio/goquery"
	"github.com/shakestzd/wipnote/core/slug"
)

// tokenizeQuery splits a free-form keyword query into whitespace-separated
// tokens, drops duplicates, and skips tokens shorter than 2 characters (which
// would otherwise generate noise from stop-word-length matches). Preserves
// original casing — ripgrep runs case-insensitive.
func tokenizeQuery(query string) []string {
	fields := strings.Fields(query)
	seen := make(map[string]bool, len(fields))
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if len(f) < 2 {
			continue
		}
		key := strings.ToLower(f)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, f)
	}
	return out
}

// searchWithRipgrep runs rg --json over .wipnote/*.html for the given query
// and adds scores/citations to the scores map.
func searchWithRipgrep(hgDir, query string, scores map[string]*relevantResult) error {
	args := []string{
		"--json",
		"--type", "html",
		"--ignore-case",
		"--max-count", "20",
		query,
		hgDir,
	}
	out, err := exec.Command("rg", args...).Output()
	if err != nil {
		// rg exits 1 when no matches — that's fine. Other errors pass through.
		if exitErr, ok := err.(*exec.ExitError); ok && exitErr.ExitCode() == 1 {
			return nil
		}
		return err
	}

	// Parse NDJSON output from rg --json.
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var msg rgMessage
		if err := json.Unmarshal([]byte(line), &msg); err != nil {
			continue
		}
		if msg.Type != "match" {
			continue
		}
		if err := processRipgrepMatch(msg, scores); err != nil {
			continue
		}
	}
	return nil
}

// rgMessage is a minimal ripgrep --json message.
type rgMessage struct {
	Type string      `json:"type"`
	Data rgMatchData `json:"data"`
}

type rgMatchData struct {
	Path       rgText `json:"path"`
	Lines      rgText `json:"lines"`
	LineNumber int    `json:"line_number"`
}

type rgText struct {
	Text string `json:"text"`
}

// processRipgrepMatch extracts the work-item ID from the matched HTML file,
// filters out hits that land in HTML markup, and updates scores.
func processRipgrepMatch(msg rgMessage, scores map[string]*relevantResult) error {
	filePath := msg.Data.Path.Text
	lineText := strings.TrimSpace(msg.Data.Lines.Text)
	lineNum := msg.Data.LineNumber

	// Skip lines that look like pure HTML tag markup (data- attribute lines, tags only).
	// Accept lines with meaningful text content outside angle brackets.
	if isMarkupOnlyLine(lineText) {
		return nil
	}

	// Parse the file to get work-item metadata.
	node, err := parseHTMLWorkItem(filePath)
	if err != nil || node == nil {
		return err
	}

	r := getOrCreate(scores, node)
	r.Score += weightFileMention
	r.Citations = append(r.Citations, citation{
		File:    filePath,
		Line:    lineNum,
		Snippet: slug.Truncate(lineText, 80),
	})
	return nil
}

// htmlTagPattern matches an HTML tag — compiled once and reused on the
// per-ripgrep-line hot path inside isMarkupOnlyLine.
var htmlTagPattern = regexp.MustCompile(`<[^>]+>`)

// isMarkupOnlyLine returns true for lines that consist entirely of HTML tags/attributes
// with no visible text content — these are low-signal ripgrep hits.
func isMarkupOnlyLine(line string) bool {
	if !strings.HasPrefix(line, "<") {
		return false
	}
	stripped := htmlTagPattern.ReplaceAllString(line, "")
	return strings.TrimSpace(stripped) == ""
}

// parseHTMLWorkItem opens an HTML file and extracts id, type, title, status.
func parseHTMLWorkItem(filePath string) (*relevantResult, error) {
	f, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	doc, err := goquery.NewDocumentFromReader(f)
	if err != nil {
		return nil, err
	}

	article := doc.Find("article[id]").First()
	if article.Length() == 0 {
		// Recap artifacts have no <article id> — they carry data-recap-* on <body>.
		// Surface them so find/relevant can return committed recaps too.
		if body := doc.Find("body[data-recap-kind]").First(); body.Length() > 0 {
			return recapResultFromFile(filePath, body), nil
		}
		return nil, nil // not a work item file
	}

	id, _ := article.Attr("id")
	if id == "" {
		return nil, nil
	}

	title := strings.TrimSpace(doc.Find("header h1").First().Text())
	if title == "" {
		title = strings.TrimSpace(doc.Find("title").First().Text())
	}

	return &relevantResult{
		ID:     id,
		Type:   attrOrEmpty(article, "data-type"),
		Title:  title,
		Status: attrOrEmpty(article, "data-status"),
	}, nil
}

// recapResultFromFile builds a relevantResult for a recap artifact. The id is
// derived from the filename (recaps embed their id in the path, not the HTML);
// type is "recap" so callers can distinguish recaps from work items.
func recapResultFromFile(filePath string, body *goquery.Selection) *relevantResult {
	id := strings.TrimSuffix(filepath.Base(filePath), ".html")
	title := attrOrEmpty(body, "data-recap-title")
	if title == "" {
		title = attrOrEmpty(body, "data-recap-outcome")
	}
	return &relevantResult{
		ID:     id,
		Type:   "recap",
		Title:  title,
		Status: "",
	}
}

// attrOrEmpty returns an attribute value or empty string.
func attrOrEmpty(sel *goquery.Selection, name string) string {
	v, _ := sel.Attr(name)
	return v
}

// searchViaGitFileHistory uses git log --follow to find commits touching the
// given file, then attributes each commit to the work items its full message
// (%B: subject and body) names, as searchViaGitSHA does.
//
// Uses ASCII record separator (\x1e) between commits and unit separator (\x00)
// between fields so that multi-line commit messages parse correctly.
func searchViaGitFileHistory(projectDir, hgDir, filePath string, scores map[string]*relevantResult) error {
	out, err := exec.Command(
		"git", "-C", projectDir,
		"log", "--follow", "--format=%x1e%H%x00%aI%x00%s%x00%B",
		"--", filePath,
	).Output()
	if err != nil {
		return nil
	}

	for _, block := range strings.Split(string(out), "\x1e") {
		block = strings.TrimSpace(block)
		if block == "" {
			continue
		}
		parts := strings.SplitN(block, "\x00", 4)
		if len(parts) < 4 {
			continue
		}
		sha, date, subject, body := parts[0], parts[1], parts[2], parts[3]

		for _, itemID := range extractCommitItems(body) {
			filePath := resolveItemHTMLPath(hgDir, itemID)
			var node *relevantResult
			if filePath != "" {
				node, _ = parseHTMLWorkItem(filePath)
			}
			if node == nil {
				node = &relevantResult{ID: itemID}
			}
			r := getOrCreate(scores, node)
			r.Score += weightCommitItem
			r.Citations = append(r.Citations, citation{
				SHA:     sha[:min(7, len(sha))],
				Date:    date,
				Subject: slug.Truncate(subject, 72),
			})
		}
	}
	return nil
}

// searchViaGitSHA looks up a specific commit SHA and extracts wipnote-item
// trailers to attribute work items, then augments with ripgrep of item IDs.
//
// Uses \x00 field separators so that multi-line commit bodies parse correctly.
func searchViaGitSHA(projectDir, hgDir, sha string, scores map[string]*relevantResult) error {
	out, err := exec.Command(
		"git", "-C", projectDir,
		"log", "-1", "--format=%H%x00%aI%x00%s%x00%B",
		sha,
	).Output()
	if err != nil {
		return nil
	}

	block := strings.TrimRight(string(out), "\n")
	if block == "" {
		return nil
	}
	parts := strings.SplitN(block, "\x00", 4)
	if len(parts) < 4 {
		return nil
	}
	fullSHA, date, subject, body := parts[0], parts[1], parts[2], parts[3]

	for _, itemID := range extractCommitItems(body) {
		filePath := resolveItemHTMLPath(hgDir, itemID)
		var node *relevantResult
		if filePath != "" {
			node, _ = parseHTMLWorkItem(filePath)
		}
		if node == nil {
			node = &relevantResult{ID: itemID}
		}
		r := getOrCreate(scores, node)
		r.Score += weightCommitItem
		r.Citations = append(r.Citations, citation{
			SHA:     fullSHA[:min(7, len(fullSHA))],
			Date:    date,
			Subject: slug.Truncate(subject, 72),
		})
	}

	// Also search the HTML files for the SHA as a keyword.
	_ = searchWithRipgrep(hgDir, sha, scores)
	return nil
}

// extractCommitItems returns the work item IDs a commit message references:
// Refs:/Fixes: trailers and inline ids, via parseTrailers.
func extractCommitItems(body string) []string {
	return parseTrailers(body)
}

// resolveItemHTMLPath finds the HTML file for a work-item ID by checking known subdirs.
func resolveItemHTMLPath(hgDir, id string) string {
	subdirs := []string{"features", "bugs", "spikes", "tracks", "plans", "specs", "recaps"}
	for _, sub := range subdirs {
		p := filepath.Join(hgDir, sub, id+".html")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// getOrCreate returns an existing result or inserts a new one in the map.
func getOrCreate(scores map[string]*relevantResult, node *relevantResult) *relevantResult {
	if existing, ok := scores[node.ID]; ok {
		// Update metadata if missing.
		if existing.Title == "" && node.Title != "" {
			existing.Title = node.Title
		}
		if existing.Type == "" && node.Type != "" {
			existing.Type = node.Type
		}
		if existing.Status == "" && node.Status != "" {
			existing.Status = node.Status
		}
		return existing
	}
	r := &relevantResult{
		ID:     node.ID,
		Type:   node.Type,
		Title:  node.Title,
		Status: node.Status,
	}
	scores[node.ID] = r
	return r
}
