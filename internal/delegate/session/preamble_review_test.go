package session

import (
	"strings"
	"testing"
)

// bug-9f3b580f: the review preamble asks for a final message, not a command.
func TestPreamble_ReviewProfileNamesNoReplyCommand(t *testing.T) {
	info := Info{Name: "w1", Run: "dlg-1", WorkItem: "bug-1", Task: "msg-1", Setup: Setup{Profile: ProfileReview}}
	got := Preamble(info, "/m")
	if !strings.Contains(got, "end your turn with your full answer as your final message; it is saved as your result") {
		t.Errorf("preamble lacks the final-message line:\n%s", got)
	}
	if strings.Contains(got, "delegate reply") {
		t.Errorf("review preamble still names delegate reply:\n%s", got)
	}
	info.Setup.Profile = "standard"
	if !strings.Contains(Preamble(info, "/m"), "wipnote delegate reply --task msg-1") {
		t.Error("standard preamble lost the reply command")
	}
}
