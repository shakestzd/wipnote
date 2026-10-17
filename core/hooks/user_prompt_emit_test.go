package hooks

import (
	"bytes"
	"encoding/json"
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

// decodeUserPrompt emits result and decodes the Claude wire JSON.
func decodeUserPrompt(t *testing.T, result *HookResult, now time.Time) map[string]any {
	t.Helper()
	var buf bytes.Buffer
	if err := EmitClaudeUserPrompt(&buf, result, now); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("output is not one JSON object: %v\n%s", err, buf.String())
	}
	if _, ok := out["additionalContext"]; ok {
		t.Fatalf("top-level additionalContext is dropped by Claude Code; got %s", buf.String())
	}
	return out
}

// deliveredContext returns hookSpecificOutput.additionalContext, asserting the
// event name Claude Code requires alongside it.
func deliveredContext(t *testing.T, out map[string]any) string {
	t.Helper()
	hso, _ := out["hookSpecificOutput"].(map[string]any)
	if hso == nil {
		t.Fatalf("no hookSpecificOutput in %v", out)
	}
	if hso["hookEventName"] != "UserPromptSubmit" {
		t.Fatalf("hookEventName = %v, want UserPromptSubmit", hso["hookEventName"])
	}
	ctx, _ := hso["additionalContext"].(string)
	return ctx
}

func TestEmitClaudeUserPrompt_ContextOnly(t *testing.T) {
	now := time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC)
	ctx := deliveredContext(t, decodeUserPrompt(t, &HookResult{AdditionalContext: "guide"}, now))
	if want := "Current time: 2026-10-04T00:00:00+0000 (UTC)\nguide"; ctx != want {
		t.Fatalf("context = %q, want %q", ctx, want)
	}
}

func TestEmitClaudeUserPrompt_EmptyResultStillGetsTimestamp(t *testing.T) {
	for _, res := range []*HookResult{nil, {}} {
		ctx := deliveredContext(t, decodeUserPrompt(t, res, time.Now()))
		if !strings.HasPrefix(ctx, "Current time: ") {
			t.Fatalf("context = %q", ctx)
		}
	}
}

func TestEmitClaudeUserPrompt_DecisionKeepsContextChannel(t *testing.T) {
	out := decodeUserPrompt(t, &HookResult{Decision: "block", Reason: "no", AdditionalContext: "why"}, time.Now())
	if out["decision"] != "block" || out["reason"] != "no" {
		t.Fatalf("decision/reason lost: %v", out)
	}
	if ctx := deliveredContext(t, out); !strings.HasPrefix(ctx, "Current time: ") || !strings.HasSuffix(ctx, "\nwhy") {
		t.Fatalf("context = %q", ctx)
	}
}

// The context-only and decision cases must both be capped by the
// UserPromptSubmit budget, timestamp included.
func TestEmitClaudeUserPrompt_ContextIsCapped(t *testing.T) {
	long := strings.Repeat("x", promptEventContextCap*2)
	for name, res := range map[string]*HookResult{
		"context-only": {AdditionalContext: long},
		"decision":     {Decision: "block", Reason: "no", AdditionalContext: long},
	} {
		ctx := deliveredContext(t, decodeUserPrompt(t, res, time.Now()))
		if n := len([]rune(ctx)); n != promptEventContextCap {
			t.Errorf("%s: delivered %d runes, want cap %d", name, n, promptEventContextCap)
		}
		if !strings.HasPrefix(ctx, "Current time: ") || !strings.HasSuffix(ctx, contextTruncMarker) {
			t.Errorf("%s: want timestamp kept and truncation marked, got %q…%q", name, ctx[:20], ctx[len(ctx)-20:])
		}
	}
}
