package main

import (
	"database/sql"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

const trailerSessionID = "trailer-ingest"

// reindexCommitTrailers walks git log and parses Refs:/Fixes: trailers to
// populate git_commits.feature_id for commits made outside of Claude Code
// sessions. Returns the count of new rows inserted.
func reindexCommitTrailers(database *sql.DB, projectDir string) (int, error) {
	// Get recent commits (limit to 500 to avoid scanning entire history).
	out, err := exec.Command(
		"git", "-C", projectDir,
		"log", "--format=%H %s%n%b%n---TRAILER-SEP---", "-500",
	).Output()
	if err != nil {
		return 0, fmt.Errorf("git log: %w", err)
	}

	total := 0
	for _, block := range splitTrailerBlocks(string(out)) {
		if block.hash == "" {
			continue
		}
		ids := parseTrailers(block.body)
		if len(ids) == 0 {
			continue
		}
		for _, featureID := range ids {
			result, insertErr := database.Exec(`
				INSERT OR IGNORE INTO git_commits
					(commit_hash, session_id, feature_id, message, timestamp)
				VALUES (?, ?, ?, ?, ?)`,
				block.hash, trailerSessionID, featureID,
				block.subject, time.Now().UTC().Format(time.RFC3339),
			)
			if insertErr == nil {
				if n, _ := result.RowsAffected(); n > 0 {
					total++
				}
			}
		}
	}
	return total, nil
}

type commitBlock struct {
	hash    string
	subject string
	body    string
}

// splitTrailerBlocks parses the git log output into individual commit blocks.
func splitTrailerBlocks(output string) []commitBlock {
	raw := strings.Split(output, "---TRAILER-SEP---")
	blocks := make([]commitBlock, 0, len(raw))
	for _, chunk := range raw {
		chunk = strings.TrimSpace(chunk)
		if chunk == "" {
			continue
		}
		lines := strings.SplitN(chunk, "\n", 2)
		if len(lines) == 0 {
			continue
		}
		// First line: "<hash> <subject>"
		firstLine := lines[0]
		spaceIdx := strings.IndexByte(firstLine, ' ')
		var hash, subject string
		if spaceIdx > 0 {
			hash = firstLine[:spaceIdx]
			subject = firstLine[spaceIdx+1:]
		} else {
			hash = firstLine
		}
		var body string
		if len(lines) > 1 {
			body = lines[1]
		}
		blocks = append(blocks, commitBlock{
			hash:    hash,
			subject: subject,
			body:    firstLine + "\n" + body,
		})
	}
	return blocks
}

// parenWorkItemRe matches parenthesized work item references in commit messages,
// e.g. "(feat-abc12345)". This is the primary wipnote commit convention.
var parenWorkItemRe = regexp.MustCompile(`\(\s*((?:feat|bug|spk|trk|pln|spc|plan|spec)-[0-9a-f]{8})\s*\)`)

// parseTrailers extracts work item IDs from a git commit message.
// Supported formats:
//
//	Refs: feat-abc123
//	Fixes: bug-def456
//	Refs: feat-abc123, feat-def456
//	fix: resolve crash (feat-abc12345)     — parenthesized convention
func parseTrailers(message string) []string {
	var ids []string
	seen := make(map[string]bool)

	// Parenthesized work item refs — the primary wipnote convention.
	for _, m := range parenWorkItemRe.FindAllStringSubmatch(message, -1) {
		id := m[1]
		if !seen[id] {
			ids = append(ids, id)
			seen[id] = true
		}
	}

	// Explicit Refs:/Fixes: trailers.
	for _, line := range strings.Split(message, "\n") {
		line = strings.TrimSpace(line)
		for _, prefix := range []string{"Refs:", "Fixes:"} {
			if !strings.HasPrefix(line, prefix) {
				continue
			}
			rest := strings.TrimPrefix(line, prefix)
			for _, part := range strings.Split(rest, ",") {
				id := strings.TrimSpace(part)
				if id == "" || seen[id] {
					continue
				}
				if isWorkItemID(id) {
					ids = append(ids, id)
					seen[id] = true
				}
			}
		}
	}
	return ids
}

// isWorkItemID returns true if s looks like a valid work item ID prefix.
func isWorkItemID(s string) bool {
	for _, prefix := range []string{"feat-", "bug-", "spk-", "trk-", "pln-", "spc-", "plan-", "spec-"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
