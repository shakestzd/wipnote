package hooks

import "testing"

// bug-9f3b580f: Stop hands a delegate worker's final message to the
// registered saver, once, and only inside a delegate run.
func TestDelegateStopReply_CallsSaverInsideRun(t *testing.T) {
	var gotRun, gotMsg string
	calls := 0
	old := DelegateStopReply
	DelegateStopReply = func(_, run, msg string) { calls++; gotRun, gotMsg = run, msg }
	defer func() { DelegateStopReply = old }()

	t.Setenv(DelegateRunEnv, "dlg-f3ef83972737")
	delegateStopReply(&CloudEvent{LastAssistantMessage: "final answer"}, t.TempDir())
	if calls != 1 || gotRun != "dlg-f3ef83972737" || gotMsg != "final answer" {
		t.Fatalf("calls %d run %q msg %q", calls, gotRun, gotMsg)
	}
	// A subagent's stop is not the worker's.
	delegateStopReply(&CloudEvent{LastAssistantMessage: "x", AgentID: "agent-7"}, t.TempDir())
	// No message, or no run: nothing.
	delegateStopReply(&CloudEvent{}, t.TempDir())
	t.Setenv(DelegateRunEnv, "")
	delegateStopReply(&CloudEvent{LastAssistantMessage: "x"}, t.TempDir())
	if calls != 1 {
		t.Fatalf("saver called %d times, want 1", calls)
	}
}

// A saver that panics never reaches the hook's caller.
func TestDelegateStopReply_PanicIsSwallowed(t *testing.T) {
	old := DelegateStopReply
	DelegateStopReply = func(_, _, _ string) { panic("boom") }
	defer func() { DelegateStopReply = old }()
	t.Setenv(DelegateRunEnv, "dlg-f3ef83972737")
	delegateStopReply(&CloudEvent{LastAssistantMessage: "x"}, t.TempDir())
}
