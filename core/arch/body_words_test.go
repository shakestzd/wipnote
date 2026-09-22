package arch

import (
	"strings"
	"testing"
)

func TestCountWords_SkipsCodeSpansAndURLs(t *testing.T) {
	cases := []struct {
		name  string
		input string
		want  int
	}{
		{"plain", "one two three", 3},
		{"single code span", "call `foo.Bar()` here", 2},
		{"multi-word code span", "see `git merge-base --is-ancestor` first", 2},
		{"code span glued to word", "x`a b`y", 2},
		{"unmatched backtick counts literally", "a `b c", 3},
		{"https url", "tracked at https://github.com/x/y/issues/1 today", 3},
		{"http url in parens", "bug (http://example.com/bug) fixed", 2},
		{"only urls and code", "`x` https://a.b", 0},
		{"empty", "   ", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := countWords(tc.input); got != tc.want {
				t.Errorf("countWords(%q) = %d, want %d", tc.input, got, tc.want)
			}
			if got := CountBodyWords(tc.input); got != tc.want {
				t.Errorf("CountBodyWords(%q) = %d, want %d", tc.input, got, tc.want)
			}
		})
	}
}

func TestValidate_WordLimitIgnoresCodeAndURLs(t *testing.T) {
	c := validCard()
	// 120 prose words plus references that must not count.
	c.Body = strings.Repeat("word ", MaxBodyWords) + "`some.Identifier()` https://example.com/very/long/path"
	if err := Validate(c); err != nil {
		t.Errorf("code spans and URLs must not count toward the limit: %v", err)
	}
}

func TestValidate_OverflowExcerptNamesTrailingText(t *testing.T) {
	c := validCard()
	c.Body = strings.Repeat("word ", MaxBodyWords) + "this trailing sentence pushed it over."
	err := Validate(c)
	if err == nil {
		t.Fatal("expected word-limit error")
	}
	msg := err.Error()
	for _, want := range []string{"body exceeds 120-word limit (126 words", "text past word 120", "this trailing sentence pushed it over."} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q should contain %q", msg, want)
		}
	}
}

func TestOverflowExcerpt_Truncates(t *testing.T) {
	body := strings.Repeat("w ", MaxBodyWords) + strings.Repeat("overflow ", 30)
	got := overflowExcerpt(body, MaxBodyWords)
	if !strings.HasSuffix(got, "…") {
		t.Errorf("long excerpt should end with an ellipsis, got %q", got)
	}
	if n := len([]rune(got)); n != overflowExcerptRunes+1 {
		t.Errorf("excerpt length = %d runes, want %d", n, overflowExcerptRunes+1)
	}
	if overflowExcerpt("a b", MaxBodyWords) != "" {
		t.Error("no overflow should yield an empty excerpt")
	}
}

func TestTruncateBody(t *testing.T) {
	t.Run("under cap is untouched", func(t *testing.T) {
		got, cut := TruncateBody("short body.")
		if cut || got != "short body." {
			t.Errorf("got (%q, %v)", got, cut)
		}
	})
	t.Run("cuts at last sentence boundary under cap", func(t *testing.T) {
		first := strings.TrimSpace(strings.Repeat("alpha ", 60)) + "."
		second := strings.TrimSpace(strings.Repeat("beta ", 50)) + "!"
		third := strings.TrimSpace(strings.Repeat("gamma ", 30)) + "?"
		got, cut := TruncateBody(first + " " + second + " " + third)
		if !cut {
			t.Fatal("expected a cut")
		}
		if got != first+" "+second {
			t.Errorf("should keep the first two sentences (110 words), got %d words: %q", countWords(got), got)
		}
	})
	t.Run("decimal point is not a boundary", func(t *testing.T) {
		body := "v1.2 " + strings.TrimSpace(strings.Repeat("x ", 100)) + ". " + strings.Repeat("y ", 30)
		got, cut := TruncateBody(body)
		if !cut || !strings.HasSuffix(got, "x.") || strings.Contains(got, "y") {
			t.Errorf("got %q", got)
		}
	})
	t.Run("no boundary falls back to word cut", func(t *testing.T) {
		got, cut := TruncateBody(strings.Repeat("w ", MaxBodyWords+10))
		if !cut || countWords(got) != MaxBodyWords {
			t.Errorf("got %d words (cut=%v)", countWords(got), cut)
		}
	})
	t.Run("keeps code spans and urls intact", func(t *testing.T) {
		body := strings.Repeat("w ", MaxBodyWords-1) + "`a b c` https://x.y/z end extra"
		got, cut := TruncateBody(body)
		if !cut || !strings.Contains(got, "`a b c`") || !strings.HasSuffix(got, "end") {
			t.Errorf("got %q", got)
		}
		if countWords(got) != MaxBodyWords {
			t.Errorf("got %d words", countWords(got))
		}
	})
}
