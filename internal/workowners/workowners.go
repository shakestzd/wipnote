// Package workowners parses .wipnote/WORKOWNERS files that map gitignore-style
// glob patterns to track or feature IDs. This provides static, explicit ownership
// that overrides the heuristic DB-based file ownership resolution.
//
// Format (one rule per line):
//
//	# Comment
//	cmd/wipnote/**  trk-f2a1a880
//	internal/db/*.go  feat-abc123
//	*.md              trk-docs
//
// Patterns use filepath.Match semantics with ** for recursive matching.
// The last matching rule wins (like .gitignore).
package workowners

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Rule maps a glob pattern to an owner work item ID.
type Rule struct {
	Pattern string
	OwnerID string
}

// File represents a parsed WORKOWNERS file.
type File struct {
	Rules []Rule
}

// Parse reads and parses a WORKOWNERS file.
// Returns nil (no error) if the file doesn't exist.
func Parse(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()

	var rules []Rule
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 2 {
			continue
		}
		rules = append(rules, Rule{Pattern: parts[0], OwnerID: parts[1]})
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return &File{Rules: rules}, nil
}

// Resolve returns the owner ID for a file path. Returns empty string if no
// rule matches. The last matching rule wins (like .gitignore).
func (wf *File) Resolve(filePath string) string {
	if wf == nil || len(wf.Rules) == 0 {
		return ""
	}
	var match string
	for _, r := range wf.Rules {
		if matchPattern(r.Pattern, filePath) {
			match = r.OwnerID
		}
	}
	return match
}

// matchPattern checks if filePath matches a gitignore-style pattern.
// Supports ** for recursive directory matching using segment-aware logic.
func matchPattern(pattern, filePath string) bool {
	if strings.Contains(pattern, "**") {
		return matchDoubleStar(pattern, filePath)
	}
	// No **: try exact filepath.Match, then match against basename only
	// (e.g. "*.md" matches "docs/README.md").
	if matched, _ := filepath.Match(pattern, filePath); matched {
		return true
	}
	if !strings.Contains(pattern, "/") {
		matched, _ := filepath.Match(pattern, filepath.Base(filePath))
		return matched
	}
	return false
}

// matchDoubleStar handles ** patterns by splitting into prefix/**/suffix
// segments and matching each path segment individually.
func matchDoubleStar(pattern, filePath string) bool {
	// "dir/**" — matches everything under dir/
	if strings.HasSuffix(pattern, "/**") {
		prefix := pattern[:len(pattern)-3]
		return strings.HasPrefix(filePath, prefix+"/") || filePath == prefix
	}
	// "**/name" — matches name as a complete path segment at any depth
	if strings.HasPrefix(pattern, "**/") {
		suffix := pattern[3:]
		return matchSegmentSuffix(suffix, filePath)
	}
	// "prefix/**/suffix" — prefix must match start, suffix must match end
	// with any number of intermediate segments.
	if idx := strings.Index(pattern, "/**/"); idx >= 0 {
		prefix := pattern[:idx]
		suffix := pattern[idx+4:]
		if !strings.HasPrefix(filePath, prefix+"/") {
			return false
		}
		rest := filePath[len(prefix)+1:]
		return matchSegmentSuffix(suffix, rest)
	}
	return false
}

// matchSegmentSuffix checks if suffix matches the tail segments of filePath.
// Each segment is matched individually with filepath.Match so that "*.go"
// matches "foo.go" but NOT "myfoo.goX".
func matchSegmentSuffix(suffix, filePath string) bool {
	suffixParts := strings.Split(suffix, "/")
	pathParts := strings.Split(filePath, "/")
	if len(suffixParts) > len(pathParts) {
		return false
	}
	// Try matching suffix segments against the tail of path segments.
	tail := pathParts[len(pathParts)-len(suffixParts):]
	for i, sp := range suffixParts {
		matched, _ := filepath.Match(sp, tail[i])
		if !matched {
			return false
		}
	}
	return true
}
