package session

import (
	"testing"

	"github.com/shakestzd/wipnote/core/slug"
)

func TestTerminalName(t *testing.T) {
	auto := slug.WorkerName("bug-1385b2e0", "Delegate workers tab and panes have no names")
	cases := []struct{ name, name0, title, want string }{
		{"short name plus title", "fixer", "Fix the login bug", "fixer-fix-the-login-bug"},
		{"auto name unchanged", auto, "Delegate workers tab and panes have no names", auto},
		{"short auto name unchanged", "feat-1-add-x", "Add x", "feat-1-add-x"},
		{"empty title", "fixer", "", "fixer"},
		{"symbol title", "fixer", "!!!", "fixer"},
		{"long name cut", "reviewer", "Name sessions by work item", "reviewer-name-sessions-by-work"},
		{"name fills width", "abcdefghijklmnopqrstuvwxyz012345", "Some title", "abcdefghijklmnopqrstuvwxyz012345"},
		{"cut would end in dash", "abcdefghijklmnopqrstuvwxyz", "ab cd", "abcdefghijklmnopqrstuvwxyz-ab-cd"},
		{"cut at word boundary", "abcdefghijklmnopqrstuvwxy", "ab cdef", "abcdefghijklmnopqrstuvwxy-ab"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := terminalName(c.name0, c.title)
			if got != c.want {
				t.Fatalf("terminalName(%q, %q) = %q, want %q", c.name0, c.title, got, c.want)
			}
			if len(got) > slug.NameWidth || got[len(got)-1] == '-' {
				t.Fatalf("bad result %q", got)
			}
		})
	}
}
