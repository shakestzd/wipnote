package hooks

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/question"
)

// stopQuestionNudge blocks a Stop once when the reply ends with a prose
// question while owner questions are still open on in-progress items the
// stopping session claimed (feat-7314623e). Another session's items do not
// count, and a session with no open claim is never blocked. Assumed
// questions do not count: that work went ahead on purpose. The reason names
// at most nudgeListMax ids. The question belongs in the harness question
// tool, recorded with `question answer`. It returns nil to allow.
//
// The cheap checks run first; the claim ledger and the claimed items' files
// are read only when the reply really ends with a question, and the read
// stops at nudgeBudget, allowing the stop. It allows when stop_hook_active is
// set (the harness already continued once because of a Stop block), in a
// delegate worker session (no owner is there to answer), and on Antigravity,
// which gets the prompt rule only.
func stopQuestionNudge(event *CloudEvent, projectDir, sessionID string) *HookResult {
	if event == nil || event.StopHookActive || event.Harness == HarnessAntigravity || sessionID == "" {
		return nil
	}
	if os.Getenv(DelegateRunEnv) != "" || !endsWithProseQuestion(event.LastAssistantMessage) {
		return nil
	}
	deadline := time.Now().Add(nudgeBudget)
	claimed := sessionClaimedItems(filepath.Join(projectDir, ".wipnote"), sessionID)
	if len(claimed) == 0 {
		return nil
	}
	ids, complete := question.OpenIDsOnItems(projectDir, claimed, deadline)
	if !complete || len(ids) == 0 {
		return nil
	}
	return &HookResult{
		Decision: "block",
		Reason: fmt.Sprintf("Open question records exist (%s) and your reply ends with a question in prose. "+
			"Ask open questions with the question tool, at most four per call, recommended option first, "+
			"then record each answer with `wipnote question answer`.", listIDs(ids, nudgeListMax)),
	}
}

// nudgeBudget bounds the store read of one Stop; a variable for tests.
var nudgeBudget = 200 * time.Millisecond

// sessionClaimedItems returns the work items with an open claim held by
// sessionID or by its family root, read from the canonical claim ledger as
// the write guard does (canonicalOpenClaim). An unreadable ledger yields
// none: the nudge then allows.
func sessionClaimedItems(wipnoteDir, sessionID string) []string {
	var items []string
	seen := map[string]bool{}
	// accepts collects every match and returns false, so the scan visits all
	// open episodes instead of stopping at the newest.
	scanOpenClaims(wipnoteDir, sessionID, func(e claimledger.Episode, root string) bool {
		if (e.SessionID == sessionID || e.SessionID == root) && !seen[e.WorkItemID] {
			seen[e.WorkItemID] = true
			items = append(items, e.WorkItemID)
		}
		return false
	})
	return items
}

// nudgeListMax bounds the question ids the nudge names.
const nudgeListMax = 5

// listIDs joins the first max ids and counts the rest: "a, b and 3 more".
func listIDs(ids []string, max int) string {
	if len(ids) <= max {
		return strings.Join(ids, ", ")
	}
	return fmt.Sprintf("%s and %d more", strings.Join(ids[:max], ", "), len(ids)-max)
}

// endsWithProseQuestion reports whether the last non-empty line outside a
// fenced code block ends with "?". A reply whose last non-empty line is inside
// or closes a fence does not count.
func endsWithProseQuestion(msg string) bool {
	if !strings.Contains(msg, "?") {
		return false
	}
	inFence, last := false, ""
	for _, line := range strings.Split(msg, "\n") {
		t := strings.TrimSpace(line)
		if strings.HasPrefix(t, "```") || strings.HasPrefix(t, "~~~") {
			inFence = !inFence
			last = "" // a fence line is the last non-empty line, and it is not prose
			continue
		}
		if t != "" && !inFence {
			last = t
		} else if t != "" {
			last = ""
		}
	}
	return strings.HasSuffix(last, "?")
}
