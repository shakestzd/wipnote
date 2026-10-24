package hooks

import (
	"strings"
	"testing"
)

func TestResearchGuardVerdict_YoloBlocksAttendedAdvises(t *testing.T) {
	warn := "Commit blocked: staged dependency-manifest change(s) (go.mod) but no web research."
	if block, adv := researchGuardVerdict(warn, true); !block || adv != "" {
		t.Errorf("yolo: want block with no advice, got block=%v advice=%q", block, adv)
	}
	block, adv := researchGuardVerdict(warn, false)
	if block {
		t.Error("attended: must not block")
	}
	if strings.Contains(adv, "Commit blocked") || !strings.HasPrefix(adv, "Research is required before committing: ") {
		t.Errorf("attended advice wording wrong: %q", adv)
	}
	if block, adv := researchGuardVerdict("", true); block || adv != "" {
		t.Error("empty warning must pass")
	}
}

// A yolo subagent whose own IsYoloMode is false but whose parent is yolo must
// still be blocked by the research guards (bug-0ed4e469 signal).
func TestResearchGuard_SubagentWithYoloParentBlocks(t *testing.T) {
	hgDir, childSessID, database := setupYoloParentDB(t, true)
	ctx := &toolUseContext{IsYoloMode: false, IsSubagent: true, HgDir: hgDir, SessionID: childSessID}
	yoloContext := resilientYoloContext(database, ctx)
	if block, _ := researchGuardVerdict("Research is required before writing code.", yoloContext); !block {
		t.Error("subagent with yolo parent must be blocked")
	}
}
