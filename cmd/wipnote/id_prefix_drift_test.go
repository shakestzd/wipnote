package main

import (
	"testing"

	"github.com/shakestzd/wipnote/core/workitem"
)

// TestIDPrefixCopies_Drift pins what each remaining cmd-side ID-prefix copy
// returns over the union of inputs they handle, next to the shared
// workitem.KindForID. The copies disagree (marked "!" below), so they are not
// merged yet (feat-68f18d3b, plan-7bc7114c slice 8): picking one behaviour is
// an owner decision. Core-side copies with the same drift, not callable from
// here: readsrv.collectionForID (feat/bug/spk/trk only), projection.inferType
// (adds sess-, session-shaped ids and arch nodes), hooks.inferTypeName
// (unknown prefix -> "feature").
func TestIDPrefixCopies_Drift(t *testing.T) {
	cases := []struct {
		id                        string
		kind, reindex, arch, hDir string
		lineage                   string
	}{
		{"feat-1234abcd", "feature", "feature", "feature", "features", "feature"},
		{"bug-1234abcd", "bug", "bug", "bug", "bugs", "bug"},
		{"spk-1234abcd", "spike", "spike", "spike", "spikes", "spike"},
		{"trk-1234abcd", "track", "track", "track", "tracks", "track"},
		{"plan-1234abcd", "plan", "plan", "plan", "plans", "plan"},
		{"pln-1234abcd", "plan", "unknown", "", "", "unknown"},            // ! legacy plan prefix
		{"spec-1234abcd", "spec", "spec", "spec", "", "unknown"},          // ! history and lineage skip specs
		{"spc-1234abcd", "work item", "unknown", "spec", "", "unknown"},   // ! arch only
		{"sess-1234abcd", "work item", "session", "", "", "session"},      // ! sessions
		{"recap-1234abcd", "work item", "recap", "", "", "unknown"},       // ! reindex only
		{"feat-", "feature", "unknown", "feature", "features", "feature"}, // ! reindex needs a suffix
		{"zzz-1234abcd", "work item", "unknown", "", "", "unknown"},
	}
	for _, c := range cases {
		dir, _ := subDirAndExt(c.id)
		got := []string{workitem.KindForID(c.id), inferNodeTypeFromID(c.id),
			archLinkNodeType(c.id), dir, detectLineageKind(c.id).String()}
		want := []string{c.kind, c.reindex, c.arch, c.hDir, c.lineage}
		names := []string{"KindForID", "inferNodeTypeFromID", "archLinkNodeType", "subDirAndExt", "detectLineageKind"}
		for i := range got {
			if got[i] != want[i] {
				t.Errorf("%s(%q) = %q, want %q", names[i], c.id, got[i], want[i])
			}
		}
	}
}
