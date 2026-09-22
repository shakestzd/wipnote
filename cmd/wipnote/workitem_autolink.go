package main

import (
	"fmt"
	"io"

	"github.com/shakestzd/wipnote/core/workitem"
)

// autoLinkMessageDerivedCommits persists a committed_in edge for every commit
// whose message names itemID under wipnote's convention (parenthesised id,
// Refs:/Fixes: trailer, or a bare id token — see parseTrailers) and that the
// artifact does not already link (bug-0816b822 / GH-#162).
//
// The provenance gate already ACCEPTS message-derived commits, but until now
// it never wrote them down, so lineage for worktree work stayed empty unless
// an operator ran `link-commit` by hand after the merge. Writing the edge at
// completion makes the artifact carry the provenance the gate just verified.
// Idempotent (hasCommitEdge), never fatal: a failure to resolve one hash is
// reported on w and the completion proceeds. Returns the number of edges
// written.
func autoLinkMessageDerivedCommits(w io.Writer, col *workitem.Collection, repoRoot, itemID string) int {
	node, err := col.Get(itemID)
	if err != nil || repoRoot == "" || !isGitRepo(repoRoot) {
		return 0
	}
	linked := 0
	for _, hash := range canonicalLinkedCommits(repoRoot, itemID, node) {
		if hasCommitEdge(node, hash) {
			continue
		}
		fullHash, msg, added, linkErr := writeCommitEdge(col, node, itemID, repoRoot, hash)
		if linkErr != nil {
			fmt.Fprintf(w, "auto-link warning: %s: %v\n", itemID, linkErr)
			continue
		}
		if added {
			linked++
			fmt.Fprintf(w, "linked commit %s → %s (%s)\n", truncate(fullHash, 12), itemID, msg)
		}
	}
	return linked
}
