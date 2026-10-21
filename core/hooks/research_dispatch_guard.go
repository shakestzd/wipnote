package hooks

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/shakestzd/wipnote/core/htmlparse"
	"github.com/shakestzd/wipnote/core/research"
)

// Research-first dispatch guard (docs/design/research-gate.md). Before the
// orchestrator dispatches an implementer subagent (a *-coder role) for a
// gated work item with no research record, advise (warn mode) or deny (block
// mode). It answers from file state only — the item's canonical HTML and
// .wipnote/config.json — per the hook-state rule in
// .claude/rules/plugin-development.md.

// isImplementerDispatch reports whether a Task/Agent call dispatches an
// implementer. Research, exploration and test agents are never gated: they
// are how the research record gets filled.
func isImplementerDispatch(event *CloudEvent) bool {
	if event == nil || (event.ToolName != "Task" && event.ToolName != "Agent") {
		return false
	}
	subagent, _ := event.ToolInput["subagent_type"].(string)
	return strings.Contains(strings.ToLower(subagent), "coder")
}

// dispatchWorkItem picks the work item a dispatch is for: an id named in the
// prompt or description wins, then the agent's claim, then the active feature.
func dispatchWorkItem(event *CloudEvent, ctx *toolUseContext) string {
	for _, key := range []string{"prompt", "description"} {
		s, _ := event.ToolInput[key].(string)
		// Every id is scanned: a spike, track or plan named first ("using
		// spk-… implement feat-…") must not hide the feature being built.
		for _, id := range workItemIDInTextPattern.FindAllString(s, -1) {
			if strings.HasPrefix(id, "feat-") || strings.HasPrefix(id, "bug-") {
				return id
			}
		}
	}
	if ctx.ClaimedItem != "" {
		return ctx.ClaimedItem
	}
	return ctx.FeatureID
}

// checkResearchDispatchGuard returns the advisory or deny message for an
// implementer dispatch, and whether to deny. "" means no opinion.
func checkResearchDispatchGuard(event *CloudEvent, ctx *toolUseContext) (msg string, deny bool) {
	if ctx == nil || ctx.ProjectDir == "" || !isImplementerDispatch(event) {
		return "", false
	}
	id := dispatchWorkItem(event, ctx)
	if id == "" {
		return "", false
	}
	cfg := research.LoadConfig(ctx.ProjectDir)
	if cfg.Mode == research.ModeOff {
		return "", false
	}
	for _, sub := range []string{"features", "bugs"} {
		path := filepath.Join(ctx.ProjectDir, ".wipnote", sub, id+".html")
		if _, err := os.Stat(path); err != nil {
			continue
		}
		node, err := htmlparse.ParseFile(path)
		if err != nil {
			return "", false
		}
		return research.CheckDispatch(cfg, node)
	}
	return "", false
}
