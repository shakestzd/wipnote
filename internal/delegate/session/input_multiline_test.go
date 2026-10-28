package session

import (
	"errors"
	"testing"
)

// A draft whose first line is empty still holds the person's text on the
// continuation lines below the prompt mark (PR 292 review).
func TestParseInput_ContinuationLinesAreDraft(t *testing.T) {
	for _, screen := range []string{
		"❯ \n  do not lose this draft\n",
		"──────\n> \n  second line typed\n──────\n",
		"│ > first                  │\n│   and more               │\n",
		"› \n  codex draft\n\n  ⏎ send\n",
	} {
		in := ParseInput(screen)
		if !in.Read || !in.Draft() {
			t.Fatalf("continuation draft missed: %+v from %q", in, screen)
		}
		if err := SendCheck(idle(), in); !errors.Is(err, ErrDraft) {
			t.Fatalf("send allowed over a draft in %q: %v", screen, err)
		}
	}
}

// Other side: text below the input box (after a rule or a blank line) and a
// grey continuation are not the person's draft.
func TestParseInput_TextBelowTheBoxIsNotDraft(t *testing.T) {
	for _, screen := range []string{
		"──────\n> \n──────\n  ? for shortcuts\n",
		"› \n\n  ⏎ send   ⌃J newline\n",
		"> \n  " + dim + "grey continuation" + reset + "\n",
		"╭──────╮\n│ >    │\n╰──────╯\n  bypass permissions on\n",
	} {
		if in := ParseInput(screen); !in.Read || in.Draft() {
			t.Fatalf("read as a draft: %+v from %q", in, screen)
		}
	}
}
