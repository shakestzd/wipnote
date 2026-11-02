package hooks

import (
	"strings"
	"testing"
)

func TestRouteCoders(t *testing.T) {
	inApp := map[string]string{"HC_TERMINAL_ID": "t6"}
	inTUI := map[string]string{"HERDR_ENV": "1"}
	optOut := map[string]string{"HC_TERMINAL_ID": "t6", "WIPNOTE_NATIVE_CODERS": "1"}
	cases := []struct {
		name  string
		tool  string
		agent string
		env   map[string]string
		deny  bool
	}{
		{"feature coder in the app", "Agent", "wipnote:feature-coder", inApp, true},
		{"patch coder under the old tool name", "Task", "wipnote:patch-coder", inApp, true},
		{"architect coder in the TUI", "Agent", "wipnote:architect-coder", inTUI, true},
		{"outside Herdr", "Agent", "wipnote:feature-coder", nil, false},
		{"opted out", "Agent", "wipnote:feature-coder", optOut, false},
		{"researcher", "Agent", "wipnote:researcher", inApp, false},
		{"explorer", "Agent", "Explore", inApp, false},
		{"another plugin's coder", "Agent", "other:feature-coder", inApp, false},
		{"not a subagent call", "Bash", "wipnote:feature-coder", inApp, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			event := &CloudEvent{ToolName: c.tool, ToolInput: map[string]any{"subagent_type": c.agent}}
			out := routeCoders(event, func(k string) string { return c.env[k] })
			if (out != nil) != c.deny {
				t.Fatalf("outcome %+v, want deny=%v", out, c.deny)
			}
			if c.deny {
				for _, want := range []string{"wipnote delegate spawn", "wipnote delegate wait NAME --timeout", c.agent, "brief"} {
					if !strings.Contains(out.result.Reason, want) {
						t.Errorf("reason lacks %q:\n%s", want, out.result.Reason)
					}
				}
			}
		})
	}
}

func TestRouteCoders_NoInputLetsItThrough(t *testing.T) {
	get := func(string) string { return "1" }
	if routeCoders(nil, get) != nil || routeCoders(&CloudEvent{ToolName: "Agent"}, get) != nil {
		t.Fatal("a call with no subagent name must pass")
	}
}
