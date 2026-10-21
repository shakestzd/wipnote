package research

import (
	"regexp"
	"slices"
	"strings"
)

// Change kinds. The first four introduce new behaviour and are gated by
// default; the rest are not.
const (
	KindDesign     = "design"
	KindUI         = "ui"
	KindCapability = "capability"
	KindDependency = "dependency"
	KindBugfix     = "bugfix"
	KindChore      = "chore"
	KindDocs       = "docs"
	KindRename     = "rename"
)

// AllKinds lists every valid change kind.
var AllKinds = []string{KindDesign, KindUI, KindCapability, KindDependency, KindBugfix, KindChore, KindDocs, KindRename}

// DefaultRequiredKinds are gated when config sets no required_kinds.
var DefaultRequiredKinds = []string{KindDesign, KindUI, KindCapability, KindDependency}

// ValidKind reports whether k is a known change kind.
func ValidKind(k string) bool { return slices.Contains(AllKinds, k) }

// kindKeywords is checked in order; the first match wins, so the specific,
// ungated kinds (rename, docs, chore) are tried before the broad gated ones.
var kindKeywords = []struct {
	kind string
	re   *regexp.Regexp
}{
	{KindRename, regexp.MustCompile(`(?i)\brename[sd]?\b`)},
	{KindDocs, regexp.MustCompile(`(?i)\b(docs|readme|documentation|changelog)\b`)},
	{KindChore, regexp.MustCompile(`(?i)\b(chore|cleanup|clean up|lint|tidy|typo)\b`)},
	{KindDependency, regexp.MustCompile(`(?i)\b(bump|upgrade|dependency|dependencies|deps)\b`)},
	{KindUI, regexp.MustCompile(`(?i)\b(ui|ux|menu|button|toolbar|header|modal|dialog|layout|dashboard|css|screen)\b`)},
	{KindDesign, regexp.MustCompile(`(?i)\b(design|redesign|architecture|schema|protocol|api)\b`)},
	{KindBugfix, regexp.MustCompile(`(?i)\b(fix|fixes|fixed|regression|crash)\b`)},
}

// InferKind guesses a change kind from the item type and title, returning the
// kind and the keyword that matched ("" for a type default). Only features are
// inferred from keywords: bugs are always bugfix (a bug that needs design work
// is reclassified explicitly with --change-kind), and every other type —
// spikes, which are the research, tracks, plans, specs — gets no kind.
// Features with no keyword match default to capability.
func InferKind(itemType, title string) (kind, matched string) {
	switch itemType {
	case "bug":
		return KindBugfix, ""
	case "feature":
	default:
		return "", ""
	}
	for _, kk := range kindKeywords {
		if m := kk.re.FindString(title); m != "" {
			return kk.kind, strings.ToLower(m)
		}
	}
	return KindCapability, ""
}

// KindOf returns the node's recorded change kind, or "".
func KindOf(props map[string]any) string {
	k, _ := props[PropKind].(string)
	return strings.TrimSpace(k)
}
