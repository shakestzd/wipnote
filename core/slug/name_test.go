package slug_test

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/shakestzd/wipnote/core/slug"
)

// TestWorkerName pins the worker and session name <item-id>-<slug>, cut to
// slug.NameWidth on a word boundary (plan-28b4a7f6 slice 15).
func TestWorkerName(t *testing.T) {
	for _, tc := range []struct{ id, title, want string }{
		{"feat-b11344a3", "Name sessions by work item", "feat-b11344a3-name-sessions-by"},
		{"bug-0a1b2c3d", "Fix: crash!", "bug-0a1b2c3d-fix-crash"},
		{"spk-0a0b0c0d", "", "spk-0a0b0c0d"},
		{"feat-0a1b2c3d", "  ¡¿ ", "feat-0a1b2c3d"},
		{" FEAT-0A1B2C3D ", "Hello", "feat-0a1b2c3d-hello"},
		{"feat-0a1b2c3d", "supercalifragilisticexpialidocious", "feat-0a1b2c3d-supercalifragilist"},
	} {
		got := slug.WorkerName(tc.id, tc.title)
		if got != tc.want {
			t.Errorf("WorkerName(%q, %q) = %q, want %q", tc.id, tc.title, got, tc.want)
		}
		if len(got) > slug.NameWidth || strings.HasSuffix(got, "-") {
			t.Errorf("WorkerName(%q, %q) = %q: over %d or ends in a hyphen", tc.id, tc.title, got, slug.NameWidth)
		}
	}
	// Other side: no work item gives no name, so callers keep their old name.
	if got := slug.WorkerName("", "A title"); got != "" {
		t.Fatalf("no item gave %q, want empty", got)
	}
}

// TestWorkTitle is the shared "<id><sep><title>" rule behind herdr pane
// labels and Claude session titles.
func TestWorkTitle(t *testing.T) {
	if got := slug.WorkTitle("feat-1", " ", "  spread\n over\tlines ", 48); got != "feat-1 spread over lines" {
		t.Fatalf("WorkTitle = %q", got)
	}
	if got := slug.WorkTitle("feat-1", ": ", "", 48); got != "feat-1" {
		t.Fatalf("no title = %q", got)
	}
	long := slug.WorkTitle("feat-8d3d6bdd", " ", strings.Repeat("é long ", 20), 48)
	if utf8.RuneCountInString(long) != 48 || !strings.HasSuffix(long, "…") || !strings.HasPrefix(long, "feat-8d3d6bdd é") {
		t.Fatalf("long = %q", long)
	}
	if got := slug.WorkTitle("", " ", "x", 48); got != "" {
		t.Fatalf("no id = %q, want empty", got)
	}
}
