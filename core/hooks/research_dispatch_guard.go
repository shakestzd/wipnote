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
// gated work item with no complete research record, advise (warn mode) or
// deny (block mode). It answers from file state only — the item's canonical
// HTML and .wipnote/config.json — per the hook-state rule in
// .claude/rules/plugin-development.md.

// dispatchTools are the subagent-spawn tools the guard inspects: Claude Code's
// Task/Agent and Codex's native spawn tools (the same set
// hasPriorDelegationEvidence treats as a spawn). Codex's execute skill calls
// spawn_agent(agent_type=..., description=..., message=...).
var dispatchTools = map[string]bool{
	"Task": true, "Agent": true,
	"spawn_agent": true, "spawn_agents": true, "multi_agent.spawn": true,
	"multi_agent_v1.spawn_agent": true, "multi_agent_v1.spawn_agents": true,
}

// implementerDispatches returns the spawn requests in a dispatch call that
// target an implementer. Research, exploration and test agents are never
// gated: they are how the research record gets filled. A plural spawn tool
// nests one request per agent, so every map in the input is examined.
func implementerDispatches(event *CloudEvent) []map[string]any {
	if event == nil || !dispatchTools[event.ToolName] {
		return nil
	}
	var out []map[string]any
	var walk func(v any)
	walk = func(v any) {
		switch t := v.(type) {
		case map[string]any:
			for _, key := range []string{"subagent_type", "agent_type"} {
				if s, _ := t[key].(string); strings.Contains(strings.ToLower(s), "coder") {
					out = append(out, t)
					break
				}
			}
			for _, child := range t {
				walk(child)
			}
		case []any:
			for _, child := range t {
				walk(child)
			}
		}
	}
	walk(event.ToolInput)
	return out
}

// dispatchWorkItems returns every feature and bug an implementer dispatch
// names in its prompt, description or message, so a contextual item named
// first ("use feat-A's API while implementing feat-B") cannot hide the target.
// With no id in the text it falls back to the agent's claim, then the active
// feature.
func dispatchWorkItems(requests []map[string]any, ctx *toolUseContext) []string {
	seen := map[string]bool{}
	var ids []string
	for _, req := range requests {
		for _, key := range []string{"prompt", "description", "message"} {
			s, _ := req[key].(string)
			for _, id := range workItemIDInTextPattern.FindAllString(s, -1) {
				if (strings.HasPrefix(id, "feat-") || strings.HasPrefix(id, "bug-")) && !seen[id] {
					seen[id] = true
					ids = append(ids, id)
				}
			}
		}
	}
	if len(ids) > 0 {
		return ids
	}
	if ctx.ClaimedItem != "" {
		return []string{ctx.ClaimedItem}
	}
	if ctx.FeatureID != "" {
		return []string{ctx.FeatureID}
	}
	return nil
}

// checkResearchDispatchGuard returns the advisory or deny message for an
// implementer dispatch, and whether to deny. "" means no opinion. Every
// referenced item is checked; the dispatch is denied if any gated one is
// incomplete.
func checkResearchDispatchGuard(event *CloudEvent, ctx *toolUseContext) (msg string, deny bool) {
	if ctx == nil || ctx.ProjectDir == "" {
		return "", false
	}
	requests := implementerDispatches(event)
	if len(requests) == 0 {
		return "", false
	}
	cfg := research.LoadConfig(ctx.ProjectDir)
	if cfg.Mode == research.ModeOff {
		return "", false
	}
	var msgs []string
	for _, id := range dispatchWorkItems(requests, ctx) {
		m, d := checkDispatchItem(cfg, ctx.ProjectDir, id)
		if m != "" {
			msgs = append(msgs, m)
		}
		deny = deny || d
	}
	return strings.Join(msgs, "\n"), deny
}

func checkDispatchItem(cfg research.Config, projectDir, id string) (string, bool) {
	for _, sub := range []string{"features", "bugs"} {
		path := filepath.Join(projectDir, ".wipnote", sub, id+".html")
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
