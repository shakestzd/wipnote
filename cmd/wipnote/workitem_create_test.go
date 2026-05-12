package main

import (
	"testing"
)

// TestNormalizeFilesInput covers the six required cases for --files normalization.
// Each test uses a fixed repoRoot so the function is deterministic without touching git.
func TestNormalizeFilesInput(t *testing.T) {
	const repoRoot = "/workspaces/repo"

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "absolute paths inside repo become relative",
			input: "/workspaces/repo/cmd/foo.go,/workspaces/repo/internal/bar.go",
			want:  "cmd/foo.go,internal/bar.go",
		},
		{
			name:  "already-relative paths pass through unchanged",
			input: "cmd/foo.go,internal/bar.go",
			want:  "cmd/foo.go,internal/bar.go",
		},
		{
			name:  "outside-repo absolute path gets unresolved prefix",
			input: "/home/user/external.txt",
			want:  "unresolved:/home/user/external.txt",
		},
		{
			name:  "whitespace around segments is stripped",
			input: "cmd/foo.go, internal/bar.go ",
			want:  "cmd/foo.go,internal/bar.go",
		},
		{
			name:  "empty input returns empty string",
			input: "",
			want:  "",
		},
		{
			name:  "empty segments from leading and trailing commas are dropped",
			input: ",cmd/foo.go,",
			want:  "cmd/foo.go",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeFilesInput(tc.input, repoRoot)
			if got != tc.want {
				t.Errorf("normalizeFilesInput(%q, %q) = %q, want %q",
					tc.input, repoRoot, got, tc.want)
			}
		})
	}
}
