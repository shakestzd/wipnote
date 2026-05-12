package paths_test

import (
	"testing"

	"github.com/shakestzd/wipnote/internal/paths"
)

// TestHostPathPattern_MatchesHomeDir verifies Linux home directories match.
func TestHostPathPattern_MatchesHomeDir(t *testing.T) {
	cases := []string{
		"/home/alice/project",
		"/home/bob/work/repo",
		"prefix /home/charlie/x suffix",
	}
	for _, s := range cases {
		if !paths.HostPathPattern.MatchString(s) {
			t.Errorf("expected HostPathPattern to match %q", s)
		}
	}
}

// TestHostPathPattern_MatchesUsersDir verifies macOS /Users/ home directories match.
func TestHostPathPattern_MatchesUsersDir(t *testing.T) {
	cases := []string{
		"/Users/alice/project",
		"/Users/fakeuser/Code",
		"prefix /Users/bob/x suffix",
	}
	for _, s := range cases {
		if !paths.HostPathPattern.MatchString(s) {
			t.Errorf("expected HostPathPattern to match %q", s)
		}
	}
}

// TestHostPathPattern_MatchesWorkspaces verifies Codespaces workspace paths match.
func TestHostPathPattern_MatchesWorkspaces(t *testing.T) {
	cases := []string{
		"/workspaces/wipnote/main.go",
		"/workspaces/foo/bar",
	}
	for _, s := range cases {
		if !paths.HostPathPattern.MatchString(s) {
			t.Errorf("expected HostPathPattern to match %q", s)
		}
	}
}

// TestHostPathPattern_MatchesMacTmp verifies macOS /private/var/folders/ matches.
func TestHostPathPattern_MatchesMacTmp(t *testing.T) {
	cases := []string{
		"/private/var/folders/abc/xyz",
		"/private/var/folders/",
	}
	for _, s := range cases {
		if !paths.HostPathPattern.MatchString(s) {
			t.Errorf("expected HostPathPattern to match %q", s)
		}
	}
}

// TestHostPathPattern_DoesNotMatchSafePaths verifies portable / generic paths
// are not flagged.
func TestHostPathPattern_DoesNotMatchSafePaths(t *testing.T) {
	cases := []string{
		"./relative/path",
		"foo/bar.go",
		"/usr/local/bin/tool",
		"/var/log/syslog",
		"/tmp/somefile",
		"plain text without paths",
	}
	for _, s := range cases {
		if paths.HostPathPattern.MatchString(s) {
			t.Errorf("expected HostPathPattern NOT to match %q", s)
		}
	}
}

// TestHostPathPattern_FindAllString returns the matched substring including the
// trailing slash. The precommit gate relies on this exact shape.
func TestHostPathPattern_FindAllString(t *testing.T) {
	matches := paths.HostPathPattern.FindAllString(
		"path=/Users/alice/x and /home/bob/y", -1)
	if len(matches) != 2 {
		t.Fatalf("expected 2 matches, got %d: %v", len(matches), matches)
	}
	if matches[0] != "/Users/alice/" {
		t.Errorf("match[0] = %q, want %q", matches[0], "/Users/alice/")
	}
	if matches[1] != "/home/bob/" {
		t.Errorf("match[1] = %q, want %q", matches[1], "/home/bob/")
	}
}
