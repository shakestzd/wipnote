package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/htmlparse"
)

// Canonical answers for guards whose only input used to be the hook's database
// handle (feat-af8d1689). The hot hooks have no handle, so these read the
// session's canonical tool log and the claim ledger instead.

// webResearchToolNames mirrors the tool_name IN (...) list in
// buildWebResearchQuery.
var webResearchToolNames = map[string]bool{
	"WebSearch": true, "WebFetch": true,
	"web_search": true, "web_fetch": true, "google_web_search": true,
	"web.search_query": true, "web.open": true, "web.find": true, "web.click": true,
}

// isCanonicalWebResearchEvent mirrors buildWebResearchQuery: a web/docs tool, or
// a `gh ...` shell command. Local reads deliberately do not count.
func isCanonicalWebResearchEvent(ev canonicalToolEvent) bool {
	if webResearchToolNames[ev.Tool] {
		return true
	}
	return isShellTool(ev.Tool) && strings.HasPrefix(strings.TrimSpace(ev.Summary), "gh ")
}

// sidecarCommandPrefixes mirrors hasPriorSidecarCommand's input_summary LIKE list.
var sidecarCommandPrefixes = []string{"gemini ", "codex exec ", "agy ", "copilot "}

// canonicalOrchestrationEvidence reads sessionID's own canonical tool log and
// returns per-tool counts plus whether a sidecar CLI was run, which are the
// inputs the orchestrator research advisory decides on. A missing log means
// nothing has been recorded yet: empty counts, no sidecar.
func canonicalOrchestrationEvidence(projectDir, sessionID string) (map[string]int, bool) {
	events, _ := readCanonicalToolEvents(projectDir, sessionID)
	counts := make(map[string]int, len(events))
	sidecar := false
	for _, ev := range events {
		counts[ev.Tool]++
		if !isShellTool(ev.Tool) {
			continue
		}
		summary := strings.TrimSpace(ev.Summary)
		for _, prefix := range sidecarCommandPrefixes {
			if strings.HasPrefix(summary, prefix) {
				sidecar = true
			}
		}
	}
	return counts, sidecar
}

// canonicalActiveWorkItem is the work item an open claim in the canonical claim
// ledger holds for sessionID (or its family root), or "".
func canonicalActiveWorkItem(projectDir, sessionID string) string {
	if projectDir == "" || sessionID == "" {
		return ""
	}
	workItem, _ := canonicalOpenClaim(filepath.Join(projectDir, ".wipnote"), sessionID)
	return workItem
}

// workItemDirs maps a work item id prefix to its canonical directory.
var workItemDirs = map[string]string{"feat": "features", "bug": "bugs", "spk": "spikes"}

// canonicalWorkItemTitle reads a work item's title from its canonical HTML, or
// "" when it cannot be found.
func canonicalWorkItemTitle(projectDir, id string) string {
	prefix, _, ok := strings.Cut(id, "-")
	dir := workItemDirs[prefix]
	if !ok || dir == "" || projectDir == "" {
		return ""
	}
	path := filepath.Join(projectDir, ".wipnote", dir, id+".html")
	if _, err := os.Stat(path); err != nil {
		return ""
	}
	node, err := htmlparse.ParseFile(path)
	if err != nil || node == nil {
		return ""
	}
	return node.Title
}
