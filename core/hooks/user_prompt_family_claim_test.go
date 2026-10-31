package hooks

import (
	"path/filepath"
	"testing"

	"github.com/shakestzd/wipnote/core/agent"
)

// TestUserPrompt_SeesClaimHeldByFamilyMember pins bug-179ff925 (GH-#214).
//
// In a multi-agent session the work item is often started by a descendant
// that owns its own session ID (a Codex subagent, a delegate worker, a resumed
// continuation). `wipnote feature start` shards that claim under the holder's
// own session ID. The orchestrator's per-turn lookup read only its own shard
// and accepted only its own session ID, so every prompt reported "no active
// work item" although the family held an open claim.
func TestUserPrompt_SeesClaimHeldByFamilyMember(t *testing.T) {
	const (
		root    = "aaaa1111-bbbb-cccc-dddd-eeee00000001"
		child   = "aaaa1111-bbbb-cccc-dddd-eeee00000002"
		sibling = "aaaa1111-bbbb-cccc-dddd-eeee00000003"
		other   = "aaaa1111-bbbb-cccc-dddd-eeee00000009"
		item    = "bug-0a0b0c0d"
		want    = "ACTIVE: bug-0a0b0c0d — Fix the cache"
	)
	for _, tc := range []struct {
		name, prompter, shard, holder string
		want                          string
	}{
		{"child claim in child shard, root prompts", root, child, child, want},
		{"child claim in root shard, root prompts", root, root, child, want},
		{"sibling claim, other child prompts", sibling, child, child, want},
		{"unrelated session claim does not leak", root, other, other, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clearNestedEnv(t)
			projectDir := taskClaimProject(t, root, false)
			wipnoteDir := filepath.Join(projectDir, ".wipnote")
			writeCanonicalItem(t, projectDir, "bugs", item, "bug", "in-progress", "Fix the cache")
			for _, s := range []string{child, sibling} {
				if err := agent.RegisterSessionFamily(projectDir, s, root); err != nil {
					t.Fatalf("register family: %v", err)
				}
			}
			openCanonicalClaim(t, wipnoteDir, tc.shard, tc.holder, "codex", item)

			res, err := UserPrompt(&CloudEvent{SessionID: tc.prompter, CWD: projectDir, Prompt: "implement the cache fix"})
			if err != nil {
				t.Fatalf("UserPrompt: %v", err)
			}
			if res.AdditionalContext != tc.want {
				t.Errorf("context = %q, want %q", res.AdditionalContext, tc.want)
			}
		})
	}
}
