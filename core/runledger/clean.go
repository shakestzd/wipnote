package runledger

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/hostpath"
)

// Field length caps. A command or line longer than this is cut, so one row
// stays small and a hook append stays O(row).
const (
	maxCommandLen = 1000
	maxLineLen    = 300
)

// encodedHomePattern catches a harness temp folder that encodes a home path
// into one component ("/tmp/x/-home-alice-proj/…"), which core/hostpath does
// not see as a path but which still names the user.
var encodedHomePattern = regexp.MustCompile(`[^\s"'<>]*/-(?:home|Users|workspaces)-[^/\s"'<>]*`)

// Clean makes free text safe for a committed row: the project root becomes
// ".", other host paths and the home directory are redacted.
func Clean(s, projectRoot string) string {
	if s == "" {
		return ""
	}
	if root := filepath.Clean(projectRoot); projectRoot != "" && root != "/" && root != "." {
		s = replacePathPrefix(s, root, ".")
	}
	s = encodedHomePattern.ReplaceAllString(s, "[host-path]")
	s = hostpath.Redact(s)
	if home, err := os.UserHomeDir(); err == nil && filepath.IsAbs(home) && filepath.Clean(home) != "/" {
		s = replacePathPrefix(s, filepath.Clean(home), "~")
	}
	return s
}

// replacePathPrefix replaces dir with repl wherever it names dir itself or a
// path under it, leaving longer siblings ("/srv/proj2" for "/srv/proj") alone.
func replacePathPrefix(s, dir, repl string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, dir)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		after := s[i+len(dir):]
		if after == "" || strings.ContainsRune("/\\ \t\"';&|)", rune(after[0])) {
			b.WriteString(s[:i] + repl)
		} else {
			b.WriteString(s[:i+len(dir)])
		}
		s = after
	}
}

// Truncate cuts s to n bytes on a rune boundary, marking the cut.
func Truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !isRuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "…"
}

func isRuneStart(b byte) bool { return b&0xC0 != 0x80 }

// CleanCommand is Clean plus the command length cap.
func CleanCommand(s, projectRoot string) string {
	return Truncate(Clean(s, projectRoot), maxCommandLen)
}

// Folder returns dir relative to the project root ("." for the root itself,
// "../wt-1" for a sibling worktree). A relative form that would still name a
// home directory is redacted instead.
func Folder(dir, projectRoot string) string {
	if dir == "" {
		return ""
	}
	if projectRoot != "" {
		if rel, err := filepath.Rel(projectRoot, dir); err == nil {
			rel = filepath.ToSlash(rel)
			if hostpath.Check("folder", rel) == nil {
				return rel
			}
		}
	}
	return Clean(dir, projectRoot)
}

// StoredPath returns a file path fit for a row: relative inside the project,
// otherwise only its last two components ("…/tasks/b1.output"), since a
// harness's temp folder is a host path.
func StoredPath(path, projectRoot string) string {
	if path == "" {
		return ""
	}
	if projectRoot != "" {
		if rel, err := filepath.Rel(projectRoot, path); err == nil && !strings.HasPrefix(rel, "..") {
			return filepath.ToSlash(rel)
		}
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	parts := strings.Split(strings.Trim(clean, "/"), "/")
	if len(parts) > 2 {
		parts = parts[len(parts)-2:]
	}
	return "…/" + strings.Join(parts, "/")
}
