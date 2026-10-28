package main

import (
	"path/filepath"
	"time"

	"github.com/shakestzd/wipnote/core/slug"
)

// resolveSessionName returns the session name to pass to Claude Code.
//
// Rules (in priority order):
//  1. An explicitly provided sessionName is always kept as-is.
//  2. When continue_ is true the caller is resuming the most-recent session via
//     --continue; no synthesized name is emitted so it doesn't conflict with the
//     implicit resume.
//  3. When resumeID is non-empty the caller is resuming a specific session by ID;
//     synthesizing a name would rename or conflict with the existing session.
//  4. A fresh launch for a work item is named <item-id>-<slug>, cut to 32
//     (plan-28b4a7f6 slice 15), with the title read from the project's .wipnote/.
//  5. Otherwise (fresh launch, no item) a default name is synthesized via
//     defaultSessionName.
func resolveSessionName(sessionName, resumeID string, continue_ bool, projectRoot, workItem string) string {
	if sessionName != "" {
		return sessionName
	}
	if continue_ || resumeID != "" {
		return ""
	}
	if name := workItemSessionName(projectRoot, workItem); name != "" {
		return name
	}
	return defaultSessionName(projectRoot)
}

// workItemSessionName is <item-id>-<slug> for a launch's work item, or ""
// with no item. The title comes from the canonical root's .wipnote/ (the main
// repo when projectRoot is a linked worktree). An item that cannot be read is
// named by its id alone.
func workItemSessionName(projectRoot, workItem string) string {
	if workItem == "" {
		return ""
	}
	id, title := workItem, ""
	root := canonicalProjectRoot(projectRoot)
	if root == "" {
		root = projectRoot
	}
	if root != "" {
		id, title = workItemTitle(filepath.Join(root, ".wipnote"), workItem)
	}
	return slug.WorkerName(id, title)
}

// defaultSessionName builds a default session label: <project-slug>-<timestamp>.
// projectRoot may be empty, in which case the label is just the timestamp.
func defaultSessionName(projectRoot string) string {
	ts := time.Now().Format("20060102-15.04.05")
	if projectRoot == "" {
		return ts
	}
	projectSlug := slug.Make(filepath.Base(projectRoot), 30)
	if projectSlug == "" {
		return ts
	}
	return projectSlug + "-" + ts
}
