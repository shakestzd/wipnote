package hostpath

import (
	"reflect"
	"testing"
)

// TestFind_MatchesHostPathsAndSkipsCI pins the scanner contract that
// `wipnote check host-paths` and the ledger writers share: developer home and
// workspace prefixes are violations, the GitHub Actions runner home is not.
func TestFind_MatchesHostPathsAndSkipsCI(t *testing.T) {
	got := Find("see /home/alice/proj and /Users/bob/x and /home/runner/work/y")
	want := []string{"/home/alice/", "/Users/bob/"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Find = %q, want %q", got, want)
	}
	if got := Find("docs/design/orch.md and /tmp/x"); len(got) != 0 {
		t.Fatalf("Find on portable text = %q, want none", got)
	}
}

// TestCheck_RefusesHostPathAndHomeDirectory is the write-time refusal. A bare
// home root (no trailing slash) and the running user's own home are refused as
// well as anything Find reports; relative paths and plain ids pass.
func TestCheck_RefusesHostPathAndHomeDirectory(t *testing.T) {
	t.Setenv("HOME", "/opt/fakehome")
	for _, bad := range []string{
		"/home/alice/proj",
		"/Users/bob",
		"/workspaces/carol",
		"/opt/fakehome",
		"/opt/fakehome/proj",
		"cwd=/private/var/folders/x/",
	} {
		if err := Check("label", bad); err == nil {
			t.Errorf("Check(%q) = nil, want a refusal", bad)
		}
	}
	for _, ok := range []string{
		"", "feat-dd8b0cde", "docs/design/orch.md", "review the parser",
		"/home/runner/work/x", "/opt/fakehomer", "/tmp/x",
	} {
		if err := Check("label", ok); err != nil {
			t.Errorf("Check(%q) = %v, want nil", ok, err)
		}
	}
}
