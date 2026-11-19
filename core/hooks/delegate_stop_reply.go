package hooks

import (
	"os"
	"strings"
)

// DelegateStopReply saves a delegate worker's final message as its task
// result (bug-9f3b580f). The review profile has no shell, so the worker
// cannot run delegate reply. The command package sets this, because core
// cannot import the delegate code; it decides by the run's profile whether to
// write anything.
var DelegateStopReply func(projectDir, run, message string)

// delegateStopReply hands the turn's final message to DelegateStopReply when
// this Stop is the worker's own, not a subagent's. It never fails and never
// panics: the Stop hook must stay fast and quiet.
func delegateStopReply(event *CloudEvent, projectDir string) {
	run := strings.TrimSpace(os.Getenv(DelegateRunEnv))
	fn := DelegateStopReply
	if fn == nil || run == "" || projectDir == "" || event == nil || isSubagentEvent(event) ||
		strings.TrimSpace(event.LastAssistantMessage) == "" {
		return
	}
	defer func() { _ = recover() }()
	fn(projectDir, run, event.LastAssistantMessage)
}
