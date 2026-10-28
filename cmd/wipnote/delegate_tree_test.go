package main

import (
	"bytes"
	"regexp"
	"testing"

	"github.com/spf13/cobra"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// list's text names each run's Overview group beside its state (slice 25).
func TestDelegateList_PrintsGroup(t *testing.T) {
	var out bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	rows := []session.Row{
		{Run: "dlg-0a0000000003", Name: "w1", Harness: "codex", State: session.StateQuestion, Group: session.GroupWaitingOnYou},
		{Run: "dlg-0a0000000001", Harness: "claude-print", State: session.StateEnded, Group: session.GroupResolved},
	}
	if err := printRows(cmd, rows); err != nil {
		t.Fatal(err)
	}
	for _, re := range []string{`GROUP +STATE`, `dlg-0a0000000003 +w1 +codex +Waiting on you +question`, `dlg-0a0000000001 +claude-print +Resolved +ended`} {
		if !regexp.MustCompile(re).MatchString(out.String()) {
			t.Fatalf("list text lacks %q:\n%s", re, out.String())
		}
	}
}
