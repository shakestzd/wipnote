package hooks

import "testing"

// A Write or Edit into .wipnote/ is a store write: containsWipnoteDir once
// compared 11 bytes with the 9-byte ".wipnote/" and never matched
// (bug-66574a82).
func TestIswipnoteWrite_FileTools(t *testing.T) {
	cases := []struct {
		tool, path string
		want       bool
	}{
		{"Write", "/repo/.wipnote/delegate/mail/w1/outbox/msg-0123456789ab-result.json", true},
		{"Edit", ".wipnote/features/feat-1.html", true},
		{"MultiEdit", "/repo/.wipnote", false},
		{"Write", "/repo/internal/wipnote/x.go", false},
		{"Write", "/repo/.wipnote-cache/x", false},
		{"Read", "/repo/.wipnote/features/feat-1.html", false},
	}
	for _, c := range cases {
		ev := &CloudEvent{ToolName: c.tool, ToolInput: map[string]any{"file_path": c.path}}
		if got := iswipnoteWrite(ev); got != c.want {
			t.Errorf("%s %s: got %v, want %v", c.tool, c.path, got, c.want)
		}
	}
	patch := &CloudEvent{ToolName: "apply_patch", ToolInput: map[string]any{"patch": "*** Update File: .wipnote/bugs/bug-1.html\n"}}
	if !iswipnoteWrite(patch) {
		t.Error("apply_patch into .wipnote/ not caught")
	}
}
