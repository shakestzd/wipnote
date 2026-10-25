package workitem

import "strings"

// idPrefixes is the single table mapping a work-item ID prefix to its kind
// and .wipnote/ collection directory. GenerateID mints these prefixes (see
// prefixes); "pln-" is the legacy plan prefix still found on disk.
var idPrefixes = []struct {
	prefix, kind, dir string
}{
	{"feat-", "feature", "features"},
	{"bug-", "bug", "bugs"},
	{"spk-", "spike", "spikes"},
	{"trk-", "track", "tracks"},
	{"plan-", "plan", "plans"},
	{"pln-", "plan", "plans"},
	{"spec-", "spec", "specs"},
}

// lookupIDPrefix returns the kind and collection directory for id's prefix,
// and false when the prefix is not a work-item prefix.
func lookupIDPrefix(id string) (kind, dir string, ok bool) {
	for _, p := range idPrefixes {
		if strings.HasPrefix(id, p.prefix) {
			return p.kind, p.dir, true
		}
	}
	return "", "", false
}

// KindForID names the work-item kind of id from its prefix ("feat-" ->
// "feature", "trk-" -> "track"). An unknown prefix returns "work item", the
// wording error messages use when the kind cannot be told.
func KindForID(id string) string {
	if kind, _, ok := lookupIDPrefix(id); ok {
		return kind
	}
	return "work item"
}

// collectionDirForID maps a target ID to its .wipnote/ collection
// subdirectory based on the ID prefix. Session IDs (bare UUIDs, no prefix)
// resolve to "sessions". Returns "" when the prefix is unrecognized, signaling
// callers to fall back to a bare same-directory href.
func collectionDirForID(id string) string {
	if _, dir, ok := lookupIDPrefix(id); ok {
		return dir
	}
	if isSessionID(id) {
		return "sessions"
	}
	return ""
}
