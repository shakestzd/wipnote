package main

import (
	"testing"
)

// TestDecideTmuxWrap verifies the wrap decision logic for all combinations of
// (tmux-flag, TMUX env, tmux-on-path) → expected action.
func TestDecideTmuxWrap(t *testing.T) {
	tests := []struct {
		name       string
		tmuxFlag   bool
		tmuxEnv    string
		tmuxOnPath bool
		wantAction tmuxWrapAction
	}{
		{
			name:       "flag not set — always skip regardless of env/path",
			tmuxFlag:   false,
			tmuxEnv:    "",
			tmuxOnPath: true,
			wantAction: tmuxActionSkip,
		},
		{
			name:       "flag not set — skip even when already in tmux",
			tmuxFlag:   false,
			tmuxEnv:    "/tmp/tmux-1000/default,12345,0",
			tmuxOnPath: true,
			wantAction: tmuxActionSkip,
		},
		{
			name:       "flag set, already in tmux — skip (prevent nesting)",
			tmuxFlag:   true,
			tmuxEnv:    "/tmp/tmux-1000/default,12345,0",
			tmuxOnPath: true,
			wantAction: tmuxActionSkip,
		},
		{
			name:       "flag set, not in tmux, tmux missing — error",
			tmuxFlag:   true,
			tmuxEnv:    "",
			tmuxOnPath: false,
			wantAction: tmuxActionError,
		},
		{
			name:       "flag set, not in tmux, tmux present — exec",
			tmuxFlag:   true,
			tmuxEnv:    "",
			tmuxOnPath: true,
			wantAction: tmuxActionExec,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := decideTmuxWrap(tc.tmuxFlag, tc.tmuxEnv, tc.tmuxOnPath)
			if got != tc.wantAction {
				t.Errorf("decideTmuxWrap(%v, %q, %v) = %v, want %v",
					tc.tmuxFlag, tc.tmuxEnv, tc.tmuxOnPath, got, tc.wantAction)
			}
		})
	}
}

// TestStripTmuxFlag verifies that --tmux variants are removed and other args preserved.
func TestStripTmuxFlag(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "removes bare --tmux",
			in:   []string{"wipnote", "yolo", "--dev", "--tmux", "--feature", "feat-abc"},
			want: []string{"wipnote", "yolo", "--dev", "--feature", "feat-abc"},
		},
		{
			name: "removes --tmux=true variant",
			in:   []string{"wipnote", "yolo", "--tmux=true", "--dev"},
			want: []string{"wipnote", "yolo", "--dev"},
		},
		{
			name: "no --tmux flag — unchanged",
			in:   []string{"wipnote", "yolo", "--dev", "--feature", "feat-xyz"},
			want: []string{"wipnote", "yolo", "--dev", "--feature", "feat-xyz"},
		},
		{
			name: "only --tmux",
			in:   []string{"wipnote", "yolo", "--tmux"},
			want: []string{"wipnote", "yolo"},
		},
		{
			name: "empty args",
			in:   []string{},
			want: []string{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := stripTmuxFlag(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("len mismatch: got %v, want %v", got, tc.want)
			}
			for i, v := range got {
				if v != tc.want[i] {
					t.Errorf("arg[%d]: got %q, want %q", i, v, tc.want[i])
				}
			}
		})
	}
}
