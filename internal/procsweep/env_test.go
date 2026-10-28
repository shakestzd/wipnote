package procsweep

import "testing"

func TestSessionIDFromEnv(t *testing.T) {
	const a = "aaaaaaaa-aaaa-4aaa-8aaa-aaaaaaaaaaaa"
	const b = "bbbbbbbb-bbbb-4bbb-8bbb-bbbbbbbbbbbb"
	cases := []struct {
		name string
		env  map[string]string
		want string
	}{
		{"none", map[string]string{"PATH": "/bin"}, ""},
		{"claude native", map[string]string{"CLAUDE_CODE_SESSION_ID": a}, a},
		{"codex thread", map[string]string{"CODEX_THREAD_ID": a, "CODEX_SESSION_ID": b}, a},
		{"codex root fallback", map[string]string{"CODEX_SESSION_ID": b}, b},
		{"wipnote fallback", map[string]string{"WIPNOTE_SESSION_ID": b}, b},
		// A stale WIPNOTE_SESSION_ID inherited from a parent shell must lose
		// to the harness-native id of the session that actually ran it.
		{"native beats stale wipnote", map[string]string{"WIPNOTE_SESSION_ID": b, "CLAUDE_CODE_SESSION_ID": a}, a},
		// Nested launch: a Codex session started from a Claude shell carries
		// both natives; the launcher's WIPNOTE_HARNESS stamp decides.
		{"harness stamp decides", map[string]string{"WIPNOTE_HARNESS": "codex", "CLAUDE_CODE_SESSION_ID": b, "CODEX_THREAD_ID": a}, a},
		{"rejects junk", map[string]string{"CLAUDE_CODE_SESSION_ID": "a b;rm"}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := SessionIDFromEnv(c.env); got != c.want {
				t.Fatalf("SessionIDFromEnv = %q, want %q", got, c.want)
			}
		})
	}
}

func TestKeepSessionEnv_DropsEverythingElse(t *testing.T) {
	got := keepSessionEnv([]string{"SECRET_TOKEN=x", "CLAUDE_CODE_SESSION_ID=abc", "noequals", "WIPNOTE_HARNESS=claude"})
	if len(got) != 2 || got["CLAUDE_CODE_SESSION_ID"] != "abc" || got["WIPNOTE_HARNESS"] != "claude" {
		t.Fatalf("keepSessionEnv kept the wrong keys: %v", got)
	}
}
