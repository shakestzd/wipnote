// Package hostpath is the shared host-local path detector. The precommit scan
// (`wipnote check host-paths`) and the canonical ledger writers use it, so a
// value a writer accepts is one the scan would also pass.
//
// The pattern itself lives in core/paths (HostPathPattern), which the runtime
// normalizer also uses. This package adds the CI allowance and, for write-time
// checks, the home-directory refusal.
package hostpath

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
)

// ciAllowPattern matches the GitHub Actions runner home, which matches the
// host-path pattern but is not a developer's machine.
var ciAllowPattern = regexp.MustCompile(`^/home/runner(/|$)`)

// homeRootPattern catches a home or workspace root with no trailing slash
// ("/Users/alice"), which HostPathPattern does not match because the scan
// looks for a path INSIDE a home directory.
var homeRootPattern = regexp.MustCompile(`/(?:Users|home|workspaces)/[^/\s"'<>]+`)

// Find returns every host-local path prefix in s, in order, skipping the CI
// runner home. It is the scanner contract of `wipnote check host-paths`.
func Find(s string) []string {
	var out []string
	for _, m := range paths.HostPathPattern.FindAllString(s, -1) {
		if !ciAllowPattern.MatchString(m) {
			out = append(out, m)
		}
	}
	return out
}

// Check refuses a value about to be written to a canonical record when it
// holds a host-local path, a home or workspace root, or the running user's
// home directory. field names the value in the error.
func Check(field, value string) error {
	if value == "" {
		return nil
	}
	if found := Find(value); len(found) > 0 {
		return refusal(field, found[0])
	}
	for _, m := range homeRootPattern.FindAllString(value, -1) {
		if !ciAllowPattern.MatchString(m) {
			return refusal(field, m)
		}
	}
	if home := userHome(); home != "" && containsPathPrefix(value, home) {
		return refusal(field, home)
	}
	return nil
}

// userHome returns the running user's home directory, or "" when it is unset
// or the filesystem root (which would match every absolute path).
func userHome() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	home = filepath.Clean(home)
	if home == "/" || home == "." || !filepath.IsAbs(home) {
		return ""
	}
	return home
}

// containsPathPrefix reports whether value names dir or a path under it, not
// merely a longer sibling ("/opt/home" must not match "/opt/homer").
func containsPathPrefix(value, dir string) bool {
	for rest := value; ; {
		i := strings.Index(rest, dir)
		if i < 0 {
			return false
		}
		after := rest[i+len(dir):]
		if after == "" || after[0] == '/' || after[0] == '\\' || strings.ContainsRune(" \t\"'", rune(after[0])) {
			return true
		}
		rest = after
	}
}

func refusal(field, match string) error {
	return fmt.Errorf("%s holds a host-local path (%s); record a relative path or an id instead", field, match)
}
