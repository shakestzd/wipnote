package hooks

// DelegateRedirect returns the deny reason for a shell command that is a bare
// standalone delegation (codex exec or review, claude -p, ccx task), or "" to
// let it run (plan-28b4a7f6 slice 9). `wipnote hook` sets it to
// internal/delegate/detect.Redirect: the recogniser reuses `wipnote delegate
// run`'s argv classifier, which lives in the root module that core cannot
// import. Nil turns the redirect off.
//
// cwd is the event's working folder: the recogniser checks there whether a
// project has the devcontainer a ccx task's --container form needs.
var DelegateRedirect func(command, cwd string) string

// guardDelegateRedirect denies a bare delegation so it is re-issued through
// `wipnote delegate run` and gets a ledger row, a run folder and a pane.
func guardDelegateRedirect(st *preToolState) *guardOutcome {
	if res := DelegateRedirectResult(st.event); res != nil {
		return &guardOutcome{result: res}
	}
	return nil
}

// DelegateRedirectResult is the redirect's decision for event: a block, which
// both Claude Code and Codex receive as permissionDecision deny, or nil to go
// on. It never allows and never rewrites the call (q-intercept:
// deny-redirect; a rewrite on Codex needs allow, which would auto-approve a
// run nobody approved). It fails open: no recogniser, a non-shell tool, an
// empty command or a recogniser that panics all let the call through.
func DelegateRedirectResult(event *CloudEvent) (res *HookResult) {
	fn := DelegateRedirect
	if fn == nil || event == nil || !isShellTool(event.ToolName) {
		return nil
	}
	cmd := shellCommand(event.ToolInput)
	if cmd == "" {
		return nil
	}
	defer func() {
		if recover() != nil {
			res = nil
		}
	}()
	if reason := fn(cmd, event.CWD); reason != "" {
		return &HookResult{Decision: "block", Reason: reason}
	}
	return nil
}
