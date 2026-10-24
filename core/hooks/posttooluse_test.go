package hooks

import "testing"

func TestExtractClosingIDs(t *testing.T) {
	tests := []struct {
		name    string
		msg     string
		wantIDs []string
	}{
		{
			name:    "closing keyword completes",
			msg:     "feat: add error hints (completes feat-598ceba4)",
			wantIDs: []string{"feat-598ceba4"},
		},
		{
			name:    "closing keyword fixes",
			msg:     "fix: resolve link error (fixes bug-1ce71599)",
			wantIDs: []string{"bug-1ce71599"},
		},
		{
			name:    "closing keyword closes",
			msg:     "closes spk-21cf4782 — audit done",
			wantIDs: []string{"spk-21cf4782"},
		},
		{
			name:    "closing keyword resolves",
			msg:     "resolves feat-05329c66",
			wantIDs: []string{"feat-05329c66"},
		},
		{
			name:    "closing keyword fix (no es)",
			msg:     "fix feat-12345678",
			wantIDs: []string{"feat-12345678"},
		},
		{
			name:    "closing keyword close (no s)",
			msg:     "close bug-abcdef01",
			wantIDs: []string{"bug-abcdef01"},
		},
		{
			name:    "closing keyword complete (no s)",
			msg:     "complete feat-aabbccdd",
			wantIDs: []string{"feat-aabbccdd"},
		},
		{
			name:    "parenthetical reference",
			msg:     "fix(errors): track branch not-found (feat-180ab53f)",
			wantIDs: []string{"feat-180ab53f"},
		},
		{
			name:    "parenthetical with spaces",
			msg:     "fix(errors): improve messages ( feat-180ab53f )",
			wantIDs: []string{"feat-180ab53f"},
		},
		{
			name:    "multiple IDs via keywords",
			msg:     "feat: Wave 1 — completes feat-598ceba4, completes feat-ebfac662",
			wantIDs: []string{"feat-598ceba4", "feat-ebfac662"},
		},
		{
			name:    "keyword and parenthetical deduplicated",
			msg:     "fixes feat-aabbccdd (feat-aabbccdd)",
			wantIDs: []string{"feat-aabbccdd"},
		},
		{
			name:    "mixed types",
			msg:     "closes feat-11111111 and fixes bug-22222222",
			wantIDs: []string{"feat-11111111", "bug-22222222"},
		},
		{
			name:    "case insensitive",
			msg:     "COMPLETES feat-aabbccdd",
			wantIDs: []string{"feat-aabbccdd"},
		},
		{
			name:    "no match — bare ID without keyword",
			msg:     "feat-598ceba4 some commit",
			wantIDs: nil,
		},
		{
			name:    "no match — no IDs",
			msg:     "fix: improve error messages",
			wantIDs: nil,
		},
		{
			name:    "no match — wrong prefix",
			msg:     "completes task-12345678",
			wantIDs: nil,
		},
		{
			name:    "no match — short hash",
			msg:     "completes feat-1234",
			wantIDs: nil,
		},
		{
			name:    "parenthetical with trailing issue number (broadened)",
			msg:     "fix(complete): unblock feature completion in non-Python projects (bug-900f6655, #114)",
			wantIDs: []string{"bug-900f6655"},
		},
		{
			name:    "parenthetical with multiple trailing tokens",
			msg:     "some message (feat-aabbccdd, foo, bar)",
			wantIDs: []string{"feat-aabbccdd"},
		},
		{
			name:    "bare ID in prose without parens or keywords",
			msg:     "checking progress on bug-900f6655 — lots to do",
			wantIDs: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := extractClosingIDs(tt.msg)
			if len(got) != len(tt.wantIDs) {
				t.Fatalf("extractClosingIDs(%q) = %v, want %v", tt.msg, got, tt.wantIDs)
			}
			for i := range got {
				if got[i] != tt.wantIDs[i] {
					t.Errorf("extractClosingIDs(%q)[%d] = %q, want %q", tt.msg, i, got[i], tt.wantIDs[i])
				}
			}
		})
	}
}

func TestLooksLikeGitCommit(t *testing.T) {
	tests := []struct {
		cmd  string
		want bool
	}{
		{`git commit -m "fix: stuff"`, true},
		{`git commit --amend`, true},
		{`git-commit`, true},
		{`git log`, false},
		{`echo "not a commit"`, false},
	}
	for _, tt := range tests {
		if got := looksLikeGitCommit(tt.cmd); got != tt.want {
			t.Errorf("looksLikeGitCommit(%q) = %v, want %v", tt.cmd, got, tt.want)
		}
	}
}

func TestParseGitCommitOutput(t *testing.T) {
	tests := []struct {
		name     string
		output   string
		wantHash string
		wantMsg  string
	}{
		{
			name:     "standard output",
			output:   "[main abc1234] fix: improve errors\n 3 files changed",
			wantHash: "abc1234",
			wantMsg:  "fix: improve errors",
		},
		{
			name:     "branch with slash",
			output:   "[feat/errors 1234567] feat: add hints (feat-aabbccdd)\n",
			wantHash: "1234567",
			wantMsg:  "feat: add hints (feat-aabbccdd)",
		},
		{
			name:     "no match",
			output:   "nothing to commit, working tree clean",
			wantHash: "",
			wantMsg:  "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hash, msg := parseGitCommitOutput(tt.output)
			if hash != tt.wantHash || msg != tt.wantMsg {
				t.Errorf("parseGitCommitOutput(%q) = (%q, %q), want (%q, %q)",
					tt.output, hash, msg, tt.wantHash, tt.wantMsg)
			}
		})
	}
}
