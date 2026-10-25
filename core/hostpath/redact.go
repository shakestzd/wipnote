package hostpath

import (
	"regexp"
	"strings"

	"github.com/shakestzd/wipnote/core/paths"
)

// redactToken replaces the machine-specific part of a host-local path.
const redactToken = "[host-path]"

// bareRootPattern matches a home or workspace root with no trailing slash
// ("/Users/alice") that starts the text or follows a separator, so a
// repo-relative path such as "docs/home/notes" is not mistaken for one.
var bareRootPattern = regexp.MustCompile("(^|[\\s\"'(<=:,;`])(/(?:Users|home|workspaces)/[^/\\s\"'<>(),;:`]+)")

// Redact replaces every host-local path prefix in s with "[host-path]",
// keeping the portable rest of the path ("/Users/alice/proj/x.go" becomes
// "[host-path]/proj/x.go"). Unlike paths.SanitizeHostPaths it also catches a
// bare home or workspace root ("/home/bob") and, like Find, leaves the CI
// runner home alone.
func Redact(s string) string {
	s = paths.HostPathPattern.ReplaceAllStringFunc(s, func(m string) string {
		if ciAllowPattern.MatchString(m) {
			return m
		}
		return redactToken + "/"
	})
	return bareRootPattern.ReplaceAllStringFunc(s, func(m string) string {
		sub := bareRootPattern.FindStringSubmatch(m)
		if ciAllowPattern.MatchString(sub[2]) {
			return m
		}
		root := strings.TrimRight(sub[2], ".")
		return sub[1] + redactToken + sub[2][len(root):]
	})
}
