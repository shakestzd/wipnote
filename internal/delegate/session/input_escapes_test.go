package session

import "testing"

// Text a terminal or wipnote wrote onto a finished worker's input line is not
// a person's draft, so delegate stop closes the pane (bug-440acb23, #344).
func TestParseInput_TerminalAndWipnoteTextIsNotDraft(t *testing.T) {
	for name, screen := range map[string]string{
		"osc title and herdr state, ST": "❯ \x1b]2;logo-fit\x1b\\\x1b]7501;state=done:app=claude-code\x1b\\\n",
		"osc title, BEL":                "> \x1b]0;worker\a\n",
		"c1 osc":                        "> \u009d2;title\u009c\n",
		"dcs and apc":                   "> \x1bPq#0\x1b\\\x1b_hello\x1b\\\n",
		"charset select":                "> \x1b(B\n",
		"nbsp only":                     ">  \n",
		"collector warning":             ">  wipnote: warning: shared collector moved to port 4319; this session exports there\n",
		"warning after osc":             "> \x1b]2;t\x1b\\ wipnote: warning: per-session collector skipped: x\n",
	} {
		in := ParseInput(screen)
		if !in.Read {
			t.Fatalf("%s: prompt line not found in %q", name, screen)
		}
		if in.Draft() {
			t.Fatalf("%s: read as a draft: %q", name, in.Text)
		}
	}
}

// Real typing next to those sequences is still a draft.
func TestParseInput_TypedTextBesideEscapesIsDraft(t *testing.T) {
	for name, screen := range map[string]string{
		"after osc":                        "> \x1b]2;t\x1b\\keep this\n",
		"before osc":                       "> keep this\x1b]0;t\a\n",
		"mentions wipnote":                 "> ask wipnote: warning: about this\n",
		"wipnote continuation then typing": "> \n  wipnote: warning: x\n  but this is typed\n",
	} {
		if in := ParseInput(screen); !in.Read || !in.Draft() {
			t.Fatalf("%s: typed text missed: %+v", name, in)
		}
	}
}
