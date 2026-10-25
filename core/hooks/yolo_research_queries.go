package hooks

// hasRecentResearch reports whether Read/Grep/Glob (or equivalent research
// tools) ran in this session, answered from the canonical per-session tool log
// (bug-a3b17225). Hooks have no index (feat-174dc958). It fails open when the
// session is unknown or has no tool log yet: a recording gap must not block
// valid work.
func hasRecentResearch(sessionID, agentID, projectDir string) bool {
	if sessionID == "" {
		return true // fail-open: can't verify, don't block
	}
	if seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalResearchEvent); ok {
		debugLog(projectDir,
			"[wipnote] research-gate canonical: session=%s research=%v", sessionID, seen)
		return seen
	}
	debugLog(projectDir,
		"[wipnote] research-gate fail-open: no tool log for session=%s agent=%s",
		sessionID, agentID)
	return true
}

// hasRecentWebResearch reports whether web/docs research — WebSearch/WebFetch
// (and harness equivalents) or a `gh ...` Bash command — ran in this session.
// It is the web-only subset of hasRecentResearch: local reads are deliberately
// EXCLUDED, so an external-technology edit cannot be satisfied by reading a
// local file (feat-868c752b / spk-0a982f70). It fails open the same way.
func hasRecentWebResearch(sessionID, agentID, projectDir string) bool {
	if sessionID == "" {
		return true // fail-open: can't verify, don't block
	}
	if seen, ok := canonicalAnySeen(projectDir, sessionID, isCanonicalWebResearchEvent); ok {
		return seen
	}
	debugLog(projectDir,
		"[wipnote] web-research-gate fail-open: no tool log for session=%s agent=%s",
		sessionID, agentID)
	return true
}
