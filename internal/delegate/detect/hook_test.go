package detect_test

import (
	"github.com/shakestzd/wipnote/internal/delegate/oneshot"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/detect"
)

// These tests run the real recogniser through the PreToolUse redirect, wired
// the way `wipnote hook pretooluse` wires it.

func wire(tb testing.TB) {
	tb.Helper()
	prev := hooks.DelegateRedirect
	hooks.DelegateRedirect = detect.RedirectAt
	tb.Cleanup(func() { hooks.DelegateRedirect = prev })
}

func bash(tool, cmd string) *hooks.CloudEvent {
	return &hooks.CloudEvent{ToolName: tool, ToolInput: map[string]any{"command": cmd}}
}

func TestPreToolUse_DeniesStandaloneDelegation(t *testing.T) {
	wire(t)
	cases := map[string]string{
		`codex exec -s workspace-write "implement feat-1"`: `wipnote delegate run -- codex exec -s workspace-write "implement feat-1"`,
		`claude -p "say ok" --allowedTools Read`:           `wipnote delegate run -- claude -p "say ok" --allowedTools Read`,
		`codex review --base main`:                         `wipnote delegate run -- codex review --base main`,
	}
	for cmd, want := range cases {
		for _, tool := range []string{"Bash", "exec_command"} {
			res := hooks.DelegateRedirectResult(bash(tool, cmd))
			if res == nil || res.Decision != "block" || !strings.Contains(res.Reason, want) {
				t.Errorf("%s %q: %+v, want a block naming %q", tool, cmd, res, want)
			}
		}
	}
}

func TestPreToolUse_DelegateRunPrefixNotDenied(t *testing.T) {
	wire(t)
	for _, cmd := range []string{
		`wipnote delegate run -- codex exec -s workspace-write "implement feat-1"`,
		`wipnote delegate run -- claude -p "say ok"`,
		`wipnote delegate run -- ccx task "x"`,
	} {
		if res := hooks.DelegateRedirectResult(bash("Bash", cmd)); res != nil {
			t.Errorf("%q denied: %q", cmd, res.Reason)
		}
	}
	// The retry the reason names is itself never denied.
	res := hooks.DelegateRedirectResult(bash("Bash", `codex exec "x"`))
	if res == nil {
		t.Fatal("bare codex exec not denied")
	}
	retry := `wipnote delegate run -- codex exec "x"`
	if !strings.Contains(res.Reason, retry) || hooks.DelegateRedirectResult(bash("Bash", retry)) != nil {
		t.Errorf("the named retry %q must be in the reason and pass: %q", retry, res.Reason)
	}
}

func TestPreToolUse_ChainedPassesThrough(t *testing.T) {
	wire(t)
	for _, cmd := range []string{
		`codex exec "x" 2>&1 | tail -6`, `codex exec "x"; echo done`, `a && codex exec "x"`,
		`codex exec "x" || true`, "codex exec \"x\"\necho after", `echo $(codex exec "x")`,
		`codex exec "x" &`, `ccx task "x" | head -1`, `claude -p "x" | tail -1`,
	} {
		if res := hooks.DelegateRedirectResult(bash("Bash", cmd)); res != nil {
			t.Errorf("chained %q denied: %q", cmd, res.Reason)
		}
	}
	if hooks.DelegateRedirectResult(bash("Bash", `codex exec "x"`)) == nil {
		t.Error("the same delegation standalone must be denied")
	}
}

func TestPreToolUse_NeverAllowsOrRewrites(t *testing.T) {
	wire(t)
	for _, cmd := range []string{
		`codex exec "x"`, `claude -p "x"`, `ccx task "x"`, `ls -la`, `codex exec "x" | tail`,
		`wipnote delegate run -- codex exec "x"`, `go test ./...`, `codex --help`,
	} {
		res := hooks.DelegateRedirectResult(bash("Bash", cmd))
		if res != nil && (res.Decision != "block" || res.HookSpecificOutput != nil) {
			t.Errorf("%q: %+v, want nil or a bare block", cmd, res)
		}
	}
}

func bashIn(cmd, cwd string) *hooks.CloudEvent {
	ev := bash("Bash", cmd)
	ev.CWD = cwd
	return ev
}

func TestPreToolUse_CcxTaskDeniedWithContainerForm(t *testing.T) {
	wire(t)
	res := hooks.DelegateRedirectResult(bashIn(`ccx task "fix the build"`, detect.DevcontainerProject(t, true)))
	if res == nil || res.Decision != "block" {
		t.Fatalf("bare ccx task: %+v, want a block", res)
	}
	if !strings.Contains(res.Reason, `wipnote delegate run --container -- "fix the build"`) {
		t.Errorf("reason %q lacks the --container form", res.Reason)
	}
}

// TestPreToolUse_CcxTaskRedirectsToContainer: through the hook, a bare ccx
// task of plain words in a project with its own devcontainer gets the
// --container form, which delegate run --container accepts as claude -p.
// The other sides: a ccx flag, or no devcontainer, gets the exact form.
func TestPreToolUse_CcxTaskRedirectsToContainer(t *testing.T) {
	wire(t)
	inside := detect.DevcontainerProject(t, true)
	res := hooks.DelegateRedirectResult(bashIn(`ccx task "fix the build"`, inside))
	if res == nil || res.Decision != "block" {
		t.Fatalf("bare ccx task: %+v, want a block", res)
	}
	_, line, ok := strings.Cut(res.Reason, "wipnote delegate run --container -- ")
	if !ok {
		t.Fatalf("reason %q names no --container form", res.Reason)
	}
	if line, _, _ = strings.Cut(line, "\n"); line != `"fix the build"` {
		t.Fatalf("--container form carries %q, want the task's own words", line)
	}
	if r, err := oneshot.ClassifyContainer([]string{"fix the build"}); err != nil || r.Kind != oneshot.KindClaude {
		t.Errorf("delegate run --container would take it as %v, %v; want claude -p", r.Kind, err)
	}
	for _, ev := range []*hooks.CloudEvent{
		bashIn(`ccx task "fix the build" --resume abc`, inside),
		bashIn(`ccx task "fix the build"`, detect.DevcontainerProject(t, false)),
	} {
		res := hooks.DelegateRedirectResult(ev)
		if res == nil || !strings.Contains(res.Reason, "wipnote delegate run -- ccx task") || strings.Contains(res.Reason, "--container") {
			t.Errorf("%q in %s: %+v, want only the exact form", ev.ToolInput["command"], ev.CWD, res)
		}
	}
}

// nonDelegations are ordinary Bash calls, some of which mention a tool name
// so the recogniser's fast path is not the only thing measured.
var nonDelegations = []string{
	`go test ./internal/... -run TestX -count=1`,
	`git log --grep="codex exec" --oneline -20`,
	`cat <<'EOF' > notes.md` + "\nclaude -p is how we ran it\nEOF",
	`ls -la ~/.claude/plugins && echo "ccx task later"`,
}

// TestPreToolUse_NonDelegationUnder5ms is the done-when budget: the redirect
// adds under 5 ms p95 to a Bash call that is not a delegation.
func TestPreToolUse_NonDelegationUnder5ms(t *testing.T) {
	wire(t)
	var took []time.Duration
	for i := 0; i < 400; i++ {
		ev := bash("Bash", nonDelegations[i%len(nonDelegations)])
		start := time.Now()
		if res := hooks.DelegateRedirectResult(ev); res != nil {
			t.Fatalf("%q denied: %q", nonDelegations[i%len(nonDelegations)], res.Reason)
		}
		took = append(took, time.Since(start))
	}
	slices.Sort(took)
	if p95 := took[len(took)*95/100]; p95 > 5*time.Millisecond {
		t.Errorf("p95 %v, want under 5ms", p95)
	}
}

func BenchmarkPreToolUse_NonDelegation(b *testing.B) {
	wire(b)
	events := make([]*hooks.CloudEvent, len(nonDelegations))
	for i, c := range nonDelegations {
		events[i] = bash("Bash", c)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		hooks.DelegateRedirectResult(events[i%len(events)])
	}
}
