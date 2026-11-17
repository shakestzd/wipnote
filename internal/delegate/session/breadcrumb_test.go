package session

import (
	"slices"
	"strings"
	"testing"
)

func TestParentLabel(t *testing.T) {
	long := "an-exceedingly-long-session-name-here"
	cases := []struct{ name, callerName, id, want string }{
		{"item id prefix", "feat-52d2792b-name-delegated-worker", "abcdef12-3456", "feat-52d2792b"},
		{"spike id", "spk-b32b954a research", "x", "spk-b32b954a"},
		{"plain short name", "wipnote-2026-11-16", "abcdef12-3456", "wipnote-2026-11-16"},
		{"long name cut to 24 runes", long, "abcdef12-3456", "an-exceedingly-long-sess"},
		{"nested chain kept whole", "feat-52d2792b › crumb-names", "abcdef12-3456", "feat-52d2792b › crumb-names"},
		{"no name uses short session id", "", "abcdef12-3456", "abcdef12"},
		{"no caller", "", "", ""},
		{"launch name with title uses the title", "wipnote-20261116-22.59.16 · fix delegate stop drafts", "x", "fix delegate stop drafts"},
		{"long title after the launch name is cut", "wipnote-20261116-22.59.16 · " + long, "x", "an-exceedingly-long-sess"},
		{"launch name with empty title keeps the name", "wipnote-20261116-22.59.16 · ", "x", "wipnote-20261116-22.59.16"[:24]},
	}
	for _, c := range cases {
		if got := ParentLabel(c.callerName, c.id); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
	if n := len([]rune(ParentLabel("ééééééééééééééééééééééééééé", "x"))); n != 24 {
		t.Errorf("rune cut: got %d runes", n)
	}
}

func TestParentLabelNestedGrows(t *testing.T) {
	parent := ParentLabel("feat-52d2792b-x", "p")
	worker := ClaudeSessionName(parent, "w1")
	child := ClaudeSessionName(ParentLabel(worker, "w1id"), "w2")
	if child != "feat-52d2792b › w1 › w2" {
		t.Errorf("chain = %q", child)
	}
}

func TestBootArgvSessionNameBreadcrumb(t *testing.T) {
	info := Info{Harness: HarnessClaude, Name: "fix-ttl", CallerLabel: "feat-52d2792b"}
	argv := BootArgv("wipnote", info, "")
	i := slices.Index(argv, "--name")
	if i < 0 || argv[i+1] != "feat-52d2792b › fix-ttl" {
		t.Errorf("argv = %q", argv)
	}
	info.CallerLabel = ""
	argv = BootArgv("wipnote", info, "")
	if i := slices.Index(argv, "--name"); i < 0 || argv[i+1] != "fix-ttl" {
		t.Errorf("no caller: argv = %q", argv)
	}
	info.CallerLabel, info.ResumeSession = "feat-52d2792b", "sess-1"
	if argv = BootArgv("wipnote", info, ""); slices.Contains(argv, "--name") {
		t.Errorf("resume has --name: %q", argv)
	}
	info = Info{Harness: HarnessCodex, Name: "w", CallerLabel: "x"}
	if argv = BootArgv("wipnote", info, ""); strings.Contains(strings.Join(argv, " "), "--name") {
		t.Errorf("codex has --name: %q", argv)
	}
}
