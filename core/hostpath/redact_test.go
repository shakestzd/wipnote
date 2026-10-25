package hostpath

import "testing"

// TestRedact_BareHomeRoot: a home or workspace root with no trailing slash is
// redacted as well as a path inside one.
func TestRedact_BareHomeRoot(t *testing.T) {
	for in, want := range map[string]string{
		"/Users/alice":               "[host-path]",
		"see /home/bob.":             "see [host-path].",
		"(/workspaces/carol)":        "([host-path])",
		"owner /home/john.doe.":      "owner [host-path].",
		"/Users/alice/proj/notes.md": "[host-path]/proj/notes.md",
		"a /home/bob/x and /Users/c": "a [host-path]/x and [host-path]",
	} {
		if got := Redact(in); got != want {
			t.Errorf("Redact(%q) = %q, want %q", in, got, want)
		}
		if found := Find(Redact(in)); len(found) != 0 {
			t.Errorf("Redact(%q) still holds %v", in, found)
		}
		if err := Check("v", Redact(in)); err != nil {
			t.Errorf("Check after Redact(%q): %v", in, err)
		}
	}
}

// TestRedact_LeavesPortableText: repo-relative paths, the CI runner home and
// plain words are untouched.
func TestRedact_LeavesPortableText(t *testing.T) {
	for _, in := range []string{
		"internal/delegate/run.go",
		"cmd/wipnote/home.go",
		"docs/home/notes",
		"/home/runner/work/x",
		"/tmp/x and /opt/home",
		"",
	} {
		if got := Redact(in); got != in {
			t.Errorf("Redact(%q) = %q, want unchanged", in, got)
		}
	}
}
