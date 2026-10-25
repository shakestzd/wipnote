package main

import (
	"path/filepath"

	"github.com/shakestzd/wipnote/core/models"
)

// completionEvidence memoises the Git-derived provenance facts one completion
// needs (feat-c30d0792). The dirty-source, provenance, merged-upstream and
// dependency-research gates and the post-completion auto-link all ask the same
// two questions — which commits name the item, and which source paths they
// touched — and each used to answer them with its own `git log --all --grep`
// walk and diff-tree. Nothing on the completion path between the first gate
// and the auto-link changes either answer: no commit is made and the
// artifact's committed_in edges are not edited, so one answer serves them all.
//
// Lazily computed, so a path that never needs a fact never pays for it. One
// value per invocation; not safe for concurrent use.
type completionEvidence struct {
	repoRoot   string
	wipnoteDir string
	id         string
	node       *models.Node

	commits     []string
	haveCommits bool

	codePaths     []string
	scope         completionScope
	haveCodePaths bool
}

// newCompletionEvidence returns the evidence for completing id in the project
// whose .wipnote/ is wipnoteDir. node is the item's artifact (nil when it could
// not be read); only its committed_in and implemented_in edges are consulted.
func newCompletionEvidence(wipnoteDir, id string, node *models.Node) *completionEvidence {
	return &completionEvidence{
		repoRoot:   filepath.Dir(wipnoteDir),
		wipnoteDir: wipnoteDir,
		id:         id,
		node:       node,
	}
}

// linkedCommits returns canonicalLinkedCommits for the item, walking git once.
func (e *completionEvidence) linkedCommits() []string {
	if !e.haveCommits {
		e.commits = linkedCommitsFn(e.repoRoot, e.id, e.node)
		e.haveCommits = true
	}
	return e.commits
}

// codeBearingPaths returns canonicalCodeBearingPathsScoped for the item and
// the scope its zero-commit fallback scanned, computed once.
func (e *completionEvidence) codeBearingPaths() ([]string, completionScope) {
	if !e.haveCodePaths {
		e.codePaths, e.scope = canonicalCodeBearingPathsScoped(
			e.repoRoot, e.wipnoteDir, e.id, e.node, e.linkedCommits())
		e.haveCodePaths = true
	}
	return e.codePaths, e.scope
}
