package hooks

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestTimestampLine_MatchesDateFormat(t *testing.T) {
	loc := time.FixedZone("EDT", -4*3600)
	got := TimestampLine(time.Date(2026, 10, 4, 9, 8, 7, 0, loc))
	want := "Current time: 2026-10-04T09:08:07-0400 (EDT)"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestEmitClaudeUserPrompt_ContextOnlyIsPlainText(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	var buf bytes.Buffer
	if err := EmitClaudeUserPrompt(&buf, &HookResult{AdditionalContext: "guide"}, now); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "Current time: 2026-10-04T00:00:00+0000 (UTC)\n") || !strings.Contains(out, "guide") {
		t.Fatalf("unexpected output %q", out)
	}
	if strings.Contains(out, "{") {
		t.Fatalf("context-only result must be plain text, got %q", out)
	}
}

func TestEmitClaudeUserPrompt_EmptyResultStillGetsTimestamp(t *testing.T) {
	var buf bytes.Buffer
	if err := EmitClaudeUserPrompt(&buf, &HookResult{}, time.Now()); err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(buf.String(), "Current time: ") {
		t.Fatalf("got %q", buf.String())
	}
}

func TestEmitClaudeUserPrompt_DecisionStaysJSON(t *testing.T) {
	var buf bytes.Buffer
	res := &HookResult{Decision: "block", Reason: "no"}
	if err := EmitClaudeUserPrompt(&buf, res, time.Now()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, "{") || !strings.Contains(out, `"decision":"block"`) || !strings.Contains(out, "Current time: ") {
		t.Fatalf("got %q", out)
	}
}
