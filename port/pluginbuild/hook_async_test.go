package pluginbuild

import "testing"

// asyncSafeHandlers lists the handlers verified (by reading core/hooks) to be
// record-only: they never return a block decision, never exit 2 and never emit
// additionalContext, so running them as `async` loses nothing. Async hooks
// cannot block or inject context. Adding a handler here requires re-proving
// that for its current code.
var asyncSafeHandlers = map[string]bool{
	"posttooluse-failure": true,
	"pre-compact":         true,
	"post-compact":        true,
	"teammate-idle":       true,
	"task-created":        true,
	"instructions-loaded": true,
	"config-change":       true,
}

func TestLiveManifestAsyncOnlyOnVerifiedRecordOnlyHandlers(t *testing.T) {
	m := loadLiveManifest(t)
	for _, e := range m.Hooks.Events {
		if !e.Async {
			continue
		}
		if !asyncSafeHandlers[e.Handler] {
			t.Errorf("handler %q is async but not in asyncSafeHandlers: async hooks cannot block or inject context", e.Handler)
		}
		if e.AppliesTo("codex") || e.AppliesTo("antigravity") {
			t.Errorf("async handler %q must be claude-only (other targets drop the field)", e.Handler)
		}
		if e.Timeout == 0 {
			t.Errorf("async handler %q should still carry an explicit timeout", e.Handler)
		}
	}
}

func TestLiveManifestHotHooksHaveClaudeTimeouts(t *testing.T) {
	m := loadLiveManifest(t)
	hot := map[string]bool{"pretooluse": true, "posttooluse": true, "user-prompt": true}
	seen := map[string]bool{}
	for _, e := range m.Hooks.Events {
		if !hot[e.Handler] {
			continue
		}
		seen[e.Handler] = true
		entry := hookEntryForTarget("claude", e, "x")
		if entry.Timeout <= 0 || entry.Timeout > 15 {
			t.Errorf("hot hook %q needs a claude timeout in 1..15s, got %d", e.Handler, entry.Timeout)
		}
		if entry.Async {
			t.Errorf("hot hook %q can block or inject context and must stay synchronous", e.Handler)
		}
	}
	for h := range hot {
		if !seen[h] {
			t.Errorf("hot hook %q missing from manifest", h)
		}
	}
}

// TestClaudeExecHookEntry pins exec form for the default handler command
// (Claude Code 2.1.139+ `args`) and shell form for a custom Command, which
// may depend on shell features.
func TestClaudeExecHookEntry(t *testing.T) {
	e := HookEvent{Name: "PreToolUse", Handler: "pretooluse", Targets: []string{"claude"}, Timeout: 10}
	got := claudeExecHookEntry(e)
	if got.Command != "wipnote" || len(got.Args) != 2 || got.Args[0] != "hook" || got.Args[1] != "pretooluse" || got.Timeout != 10 {
		t.Errorf("default handler must be exec form wipnote [hook pretooluse]: %+v", got)
	}
	e.Command = "wipnote hook pretooluse || true"
	got = claudeExecHookEntry(e)
	if got.Command != e.Command || got.Args != nil {
		t.Errorf("custom command must stay shell form: %+v", got)
	}
	for _, target := range []string{"codex", "antigravity"} {
		if o := hookEntryForTarget(target, e, "cmd"); o.Args != nil {
			t.Errorf("%s entry must not carry args (shell form only): %+v", target, o)
		}
	}
}

func TestHookEntryForTargetClaudeOnlyFields(t *testing.T) {
	e := HookEvent{
		Name: "PostToolUse", Handler: "x", Targets: []string{"claude", "codex", "antigravity"},
		Timeout: 90, TimeoutByTarget: map[string]int{"claude": 15}, Async: true, If: "Bash(git *)",
	}
	c := hookEntryForTarget("claude", e, "cmd")
	if c.Timeout != 15 || !c.Async || c.If != "Bash(git *)" {
		t.Errorf("claude entry wrong: %+v", c)
	}
	for _, target := range []string{"codex", "antigravity"} {
		o := hookEntryForTarget(target, e, "cmd")
		if o.Timeout != 90 || o.Async || o.If != "" {
			t.Errorf("%s entry must keep base timeout and drop async/if: %+v", target, o)
		}
	}
}
