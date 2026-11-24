package secretscan

import (
	"strings"
	"testing"
	"time"
)

func TestRedact_MasksEachPatternKind(t *testing.T) {
	cases := map[string]string{
		"token=abc123SECRET":                         "abc123SECRET",
		`"password": "hunter2hunter2"`:               "hunter2hunter2",
		"Bearer abcdefgh12345678":                    "abcdefgh12345678",
		"sk-ant-api03-ABCDEFGHIJKLMNOPQRST":          "ABCDEFGHIJKLMNOPQRST",
		"ghp_abcdefghijklmnopqrstuvwxyz0123":         "abcdefghijklmnopqrstuvwxyz0123",
		"github_pat_abcdefghijklmnopqrstuvwxyz":      "abcdefghijklmnopqrstuvwxyz",
		"AKIAABCDEFGHIJKLMNOP":                       "AKIAABCDEFGHIJKLMNOP",
		"xoxb-1234567890-abcdef":                     "1234567890-abcdef",
		"eyJhbGciOiJI.eyJzdWIiOiIx.SflKxwRJSMeKKF2Q": "eyJzdWIiOiIx",
	}
	for in, secret := range cases {
		got := Redact(in)
		if strings.Contains(got, secret) {
			t.Errorf("Redact(%q) = %q: secret kept", in, got)
		}
		if !strings.Contains(got, Mask) {
			t.Errorf("Redact(%q) = %q: no mask", in, got)
		}
		if !Match([]byte(in)) {
			t.Errorf("Match(%q) = false, want true", in)
		}
	}
}

func TestMatch_PlainTextIsClean(t *testing.T) {
	for _, in := range []string{"", "# Draft\n\nA plain paragraph about the plan.\n", "the keys are on the table"} {
		if Match([]byte(in)) {
			t.Errorf("Match(%q) = true, want false", in)
		}
		if got := Redact(in); got != in {
			t.Errorf("Redact(%q) = %q, want it unchanged", in, got)
		}
	}
}

// The patterns run on RE2, so a large adversarial input stays linear.
func TestMatch_LinearOnLargeInput(t *testing.T) {
	big := []byte(strings.Repeat("tokenx a ", 1<<17))
	start := time.Now()
	_ = Match(big)
	if d := time.Since(start); d > 2*time.Second {
		t.Errorf("Match of %d bytes took %v", len(big), d)
	}
}
