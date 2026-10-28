package statusall

import (
	"bytes"
	"strings"
	"testing"
)

// A source state every project shares is printed once in the header; one a
// single project has is a note on that project.
func TestWriteText_SharedSourcesOnceAndOwnNotes(t *testing.T) {
	a, b := fullReport().Projects[0], fullReport().Projects[0]
	b.Name = "beta"
	a.Sources = map[string]string{"git": SourceOK, "gh": SourceNotAvailable + ": HTTP 502", "herdr": SourceNotAvailable + ": x"}
	b.Sources = map[string]string{"git": SourceOK, "gh": SourceOK, "herdr": SourceNotAvailable + ": x"}

	var buf bytes.Buffer
	WriteText(&buf, Report{Projects: []Project{a, b}}, testNow)
	out := buf.String()

	if strings.Count(out, "herdr: not available: x") != 1 || !strings.HasPrefix(strings.SplitN(out, "\n", 3)[1], "herdr:") {
		t.Fatalf("herdr note must be printed once, in the header:\n%s", out)
	}
	if strings.Count(out, "gh: not available: HTTP 502") != 1 || !strings.Contains(out, "note      gh: not available: HTTP 502") {
		t.Fatalf("gh note must be on alpha only:\n%s", out)
	}
	for _, want := range []string{
		"alpha  · active 5m ago · on work",
		"plan      plan-0000aaaa Ship the widget · slice 2 Second (promoted) · 1/3 done",
		"pr        #42 OPEN · checks 1 pass, 1 fail, 0 pending (fail: lint) · review APPROVED · MERGEABLE",
		"agent     claude blocked w1:p2 feat-11110002 Second · \"Allow this command?\"",
		"NEEDS YOU blocked_agent: claude agent in pane w1:p2 is blocked",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in\n%s", want, out)
		}
	}
}
