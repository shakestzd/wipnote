package hooks

import (
	"testing"
	"time"
)

func TestDurationMS_ParsesFromPayloads(t *testing.T) {
	cases := []struct {
		name    string
		harness Harness
		raw     string
		want    time.Duration
	}{
		{"claude int", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":1500}`, 1500 * time.Millisecond},
		{"claude float", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":12.9}`, 12 * time.Millisecond},
		{"claude string", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":"250"}`, 250 * time.Millisecond},
		{"claude absent", HarnessClaude, `{"session_id":"s","tool_name":"Bash"}`, 0},
		{"claude null", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":null}`, 0},
		{"claude garbage tolerated", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":"soon"}`, 0},
		{"claude negative ignored", HarnessClaude, `{"session_id":"s","tool_name":"Bash","duration_ms":-5}`, 0},
		{"codex int", HarnessCodex, `{"session_id":"s","hook_event_name":"PostToolUse","tool_name":"Bash","tool_use_id":"c1","duration_ms":42}`, 42 * time.Millisecond},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := ParseEventForHarness(tc.harness, []byte(tc.raw))
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			if ev.ToolName != "Bash" {
				t.Fatalf("payload not decoded (tool_name=%q)", ev.ToolName)
			}
			if got := ev.DurationMS.Duration(); got != tc.want {
				t.Errorf("duration = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestToolCallEventID_Deterministic(t *testing.T) {
	a := toolCallEventID("sess-1", "toolu_A")
	if a == "" || a != toolCallEventID("sess-1", "toolu_A") {
		t.Fatalf("id not deterministic: %q", a)
	}
	if a == toolCallEventID("sess-1", "toolu_B") {
		t.Error("different tool_use_id must yield a different id")
	}
	if a == toolCallEventID("sess-2", "toolu_A") {
		t.Error("different session must yield a different id")
	}
	if toolCallEventID("sess-1", "") != "" || toolCallEventID("", "toolu_A") != "" {
		t.Error("empty input must yield empty id")
	}
	if r := toolCallEventIDOrRandom("sess-1", ""); r == "" || r == toolCallEventIDOrRandom("sess-1", "") {
		t.Error("random fallback must be non-empty and unique")
	}
}
