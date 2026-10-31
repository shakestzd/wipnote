package hooks

import (
	"path/filepath"

	"github.com/shakestzd/wipnote/core/agent"
	"github.com/shakestzd/wipnote/core/claimledger"
)

// canonicalFamilyActiveWorkItem is the work item the session family of
// sessionID is working on, for the per-turn active-item line (bug-179ff925).
//
// canonicalActiveWorkItem answers only "does THIS session, or its family root,
// hold a claim". That is right for the write guards, but in a multi-agent
// session the claim is often opened by a descendant that owns its own session
// ID (a Codex subagent, a delegate worker, a resumed continuation): `wipnote
// feature start` shards it under the holder's session, and the episode names
// the holder, so the orchestrator's lookup never matched it and every prompt
// reported no active item. This falls back to the newest open claim held by
// any session registered in the same family.
func canonicalFamilyActiveWorkItem(projectDir, sessionID string) string {
	if item := canonicalActiveWorkItem(projectDir, sessionID); item != "" {
		return item
	}
	members := sessionFamilyMembers(projectDir, sessionID)
	if len(members) < 2 {
		return "" // no other session in the family: the lookup above was complete
	}
	store := claimledger.NewStore(filepath.Join(projectDir, ".wipnote"))
	var newest claimledger.Episode
	for shard := range members {
		episodes, err := store.ReadShard(shard)
		if err != nil {
			continue
		}
		for _, e := range episodes {
			if e.IsOpen() && e.WorkItemID != "" && members[e.SessionID] && e.StartedAt.After(newest.StartedAt) {
				newest = e
			}
		}
	}
	return newest.WorkItemID
}

// sessionFamilyMembers returns the set of sessions in sessionID's family: the
// family root plus every session the family index registers under it. A
// session with no family entry is its own root.
func sessionFamilyMembers(projectDir, sessionID string) map[string]bool {
	if projectDir == "" || sessionID == "" {
		return nil
	}
	family := agent.SessionFamilyFor(projectDir, sessionID)
	if family == "" {
		family = sessionID
	}
	members := map[string]bool{family: true, sessionID: true}
	idx, _ := agent.ReadSessionFamilyIndex(projectDir)
	for s, f := range idx {
		if f == family {
			members[s] = true
		}
	}
	return members
}
