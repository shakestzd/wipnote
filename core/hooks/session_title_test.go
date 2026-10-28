package hooks

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// sessionTitleProject is a temp project whose session sid holds an open
// claim on spk-0a0b0c0d "Research caching".
func sessionTitleProject(t *testing.T, sid string) string {
	t.Helper()
	projectDir := taskClaimProject(t, sid, false)
	writeCanonicalItem(t, projectDir, "spikes", "spk-0a0b0c0d", "spike", "in-progress", "Research caching")
	openCanonicalClaim(t, filepath.Join(projectDir, ".wipnote"), sid, sid, "claude-code", "spk-0a0b0c0d")
	return projectDir
}

func promptTitle(t *testing.T, projectDir, sid string) string {
	t.Helper()
	res, err := UserPrompt(&CloudEvent{SessionID: sid, CWD: projectDir, Prompt: "carry on with the work"})
	if err != nil {
		t.Fatalf("UserPrompt: %v", err)
	}
	if res.HookSpecificOutput == nil {
		return ""
	}
	return res.HookSpecificOutput.SessionTitle
}

// TestUserPrompt_SessionTitleOnceAfterClaim: after a claim marks the session,
// the next prompt sets sessionTitle to "<item-id> <title>", and only that
// one prompt does (plan-28b4a7f6 slice 15).
func TestUserPrompt_SessionTitleOnceAfterClaim(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee00007777"
	projectDir := sessionTitleProject(t, sid)
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("no claim mark, yet sessionTitle %q", got)
	}
	if err := MarkSessionTitlePending(projectDir, sid, "spk-0a0b0c0d"); err != nil {
		t.Fatal(err)
	}
	if got := promptTitle(t, projectDir, sid); got != "spk-0a0b0c0d Research caching" {
		t.Fatalf("sessionTitle = %q", got)
	}
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("second prompt renamed again: %q", got)
	}
}

// TestUserPrompt_SessionTitleNeedsOpenClaim: a mark for an item the session
// no longer holds renames nothing and is dropped.
func TestUserPrompt_SessionTitleNeedsOpenClaim(t *testing.T) {
	const sid = "aaaa4444-bbbb-cccc-dddd-eeee00008888"
	projectDir := sessionTitleProject(t, sid)
	if err := MarkSessionTitlePending(projectDir, sid, "feat-99999999"); err != nil {
		t.Fatal(err)
	}
	if got := promptTitle(t, projectDir, sid); got != "" {
		t.Fatalf("renamed for an item not held: %q", got)
	}
	if got := takePendingSessionTitle(filepath.Join(projectDir, ".wipnote"), sid); got != "" {
		t.Fatalf("stale mark kept: %q", got)
	}
}

func TestMarkSessionTitlePending_NeedsSessionAndItem(t *testing.T) {
	if err := MarkSessionTitlePending(t.TempDir(), "", "feat-1"); err != nil {
		t.Fatalf("no session must be a no-op, got %v", err)
	}
	if err := MarkSessionTitlePending(t.TempDir(), "sid", ""); err != nil {
		t.Fatalf("no item must be a no-op, got %v", err)
	}
}

// TestEmitClaudeUserPrompt_SessionTitle: Claude Code reads the title at
// hookSpecificOutput.sessionTitle beside the context
// (https://code.claude.com/docs/en/hooks, UserPromptSubmit decision control).
func TestEmitClaudeUserPrompt_SessionTitle(t *testing.T) {
	res := &HookResult{AdditionalContext: "ACTIVE: x", HookSpecificOutput: &HookSpecificOutput{SessionTitle: "feat-1 Title"}}
	out := decodeUserPrompt(t, res, time.Date(2026, 10, 4, 0, 0, 0, 0, time.UTC))
	hso := out["hookSpecificOutput"].(map[string]any)
	if hso["sessionTitle"] != "feat-1 Title" || !strings.HasSuffix(deliveredContext(t, out), "ACTIVE: x") {
		t.Fatalf("hookSpecificOutput %v", hso)
	}
	if _, ok := out["sessionTitle"]; ok {
		t.Fatalf("top-level sessionTitle: %v", out)
	}
	// Other side: no title, no field.
	plain := decodeUserPrompt(t, &HookResult{AdditionalContext: "ACTIVE: x"}, time.Now())
	if _, ok := plain["hookSpecificOutput"].(map[string]any)["sessionTitle"]; ok {
		t.Fatalf("sessionTitle without a title: %v", plain)
	}
}

// TestEmitCodex_DropsSessionTitle: Codex documents no session-title output
// (learn.chatgpt.com/docs/hooks, checked 2026-10-28), and an unknown field in
// its hookSpecificOutput drops the whole response, so the title never
// reaches Codex.
func TestEmitCodex_DropsSessionTitle(t *testing.T) {
	var buf bytes.Buffer
	res := &HookResult{AdditionalContext: "ACTIVE: x", HookSpecificOutput: &HookSpecificOutput{SessionTitle: "feat-1 Title"}}
	if err := emitCodexResponseForEvent(&buf, "UserPromptSubmit", res); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "sessionTitle") || !strings.Contains(buf.String(), "ACTIVE: x") {
		t.Fatalf("codex output %s", buf.String())
	}
}
