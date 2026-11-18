package main

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// TestDelegateSpawn_BriefPreambleCarriesWorkerRules checks that the preamble
// a worker gets before its task holds the rules block (feat-0d9b57bc).
func TestDelegateSpawn_BriefPreambleCarriesWorkerRules(t *testing.T) {
	info := session.Info{Name: "w1", Run: "dlg-0123456789ab", Task: "msg-0123456789ab", WorkItem: "feat-0a1b2c3d"}
	p := session.TaskPreamble(info, "/mail/w1")
	for _, want := range []string{
		"## Worker rules",
		"Write one failing test for each acceptance criterion",
		"After a second failed fix for one problem, write down what both fixes assumed",
		"Fix failures in files you changed",
		"a command and its real output for each done-when line",
		"For a choice that is hard to reverse, write one decision line",
		"wipnote delegate reply --task msg-0123456789ab",
	} {
		if !strings.Contains(p, want) {
			t.Errorf("preamble lacks %q", want)
		}
	}
	if strings.Index(p, "## Worker rules") > strings.Index(p, "You are the wipnote delegate worker") {
		t.Error("rules must come before the result preamble")
	}
}
