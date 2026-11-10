package hooks

import (
	"fmt"
	"os"
	"strings"

	"github.com/shakestzd/wipnote/core/question"
)

// stopQuestionNudge blocks a Stop once when the reply ends with a prose
// question while owner-question records are still open (feat-7314623e): the
// question belongs in the harness question tool, recorded with `question
// answer`. It returns nil to allow. The cheap checks run first; the store is
// read only when the reply really ends with a question.
//
// It allows when stop_hook_active is set (the harness already continued once
// because of a Stop block), in a delegate worker session (no owner is there to
// answer), and on Antigravity, which gets the prompt rule only.
func stopQuestionNudge(event *CloudEvent, projectDir string) *HookResult {
	if event == nil || event.StopHookActive || event.Harness == HarnessAntigravity {
		return nil
	}
	if os.Getenv(DelegateRunEnv) != "" || !endsWithProseQuestion(event.LastAssistantMessage) {
		return nil
	}
	ids := question.OpenIDs(projectDir)
	if len(ids) == 0 {
		return nil
	}
	return &HookResult{
		Decision: "block",
		Reason: fmt.Sprintf("Open question records exist (%s) and your reply ends with a question in prose. "+
			"Ask open questions with the question tool, at most four per call, recommended option first, "+
			"then record each answer with `wipnote question answer`.", strings.Join(ids, ", ")),
	}
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
