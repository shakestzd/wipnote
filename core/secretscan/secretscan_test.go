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

// The patterns run on RE2, so a large adversarial input stays linear. The test
// times a small input and one four times larger, best of three each, and
// compares them. A wall-clock limit failed whenever the machine was busy; a
// ratio scales with load. Linear growth gives about 4x and quadratic 16x.
func TestMatch_LinearOnLargeInput(t *testing.T) {
	best := func(in []byte) time.Duration {
		min := time.Duration(1<<63 - 1)
		for i := 0; i < 3; i++ {
			start := time.Now()
			_ = Match(in)
			if d := time.Since(start); d < min {
				min = d
			}
		}
		return min
	}
	small := best([]byte(strings.Repeat("tokenx a ", 1<<15)))
	large := best([]byte(strings.Repeat("tokenx a ", 1<<17)))
	if large > 10*small {
		t.Errorf("Match took %v on the small input and %v on one 4x larger, want under 10x", small, large)
	}
}
