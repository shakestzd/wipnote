package session

import (
	"errors"
	"testing"
)

const (
	dim   = "\x1b[2m"
	grey  = "\x1b[90m"
	reset = "\x1b[0m"
)

// idle is a bound worker whose turn ended.
func idle() Observation { return Observation{Bound: true, Turn: TurnEnded} }

func TestReady_IdleWithResultIsDone(t *testing.T) {
	o := idle()
	o.TaskAnswered = true
	if r := Ready(o); r.State != StateDone {
		t.Fatalf("%+v", r)
	}
	o.Turn = TurnRunning // a result file mid-turn does not end the turn
	if r := Ready(o); r.State != StateWorking {
		t.Fatalf("%+v", r)
	}
}

func TestReady_IdleNoResult(t *testing.T) {
	o := idle()
	o.TaskPending = true
	if r := Ready(o); r.State != StateIdleNoResult {
		t.Fatalf("%+v", r)
	}
	o.TaskPending = false
	if r := Ready(o); r.State != StateReady {
		t.Fatalf("no task given: %+v", r)
	}
}

func TestReady_IdleBackgroundShellRunning(t *testing.T) {
	o := idle()
	o.TaskPending, o.ShellRunning = true, true
	if r := Ready(o); r.State != StateIdleShell {
		t.Fatalf("%+v", r)
	}
	o.ShellRunning = false
	if r := Ready(o); r.State != StateIdleNoResult {
		t.Fatalf("%+v", r)
	}
}

// One predicate: probe, wait and send never disagree about a state.
func TestReady_ProbeSendWaitAgree(t *testing.T) {
	cases := []Observation{
		{}, {PaneGone: true}, {Dialog: "claude folder-trust"}, {Bound: true}, {Bound: true, Turn: TurnRunning},
		{Bound: true, Question: true}, {Bound: true, Turn: TurnEnded, TaskAnswered: true},
		{Bound: true, Turn: TurnEnded, TaskPending: true}, {Bound: true, Turn: TurnEnded, TaskPending: true, ShellRunning: true},
	}
	empty := Input{Read: true}
	for _, o := range cases {
		state := Probe(o)
		if state != Ready(o).State {
			t.Fatalf("probe %s != ready %s for %+v", state, Ready(o).State, o)
		}
		outcome, settled := WaitOutcome(o)
		if settled && outcome != state {
			t.Fatalf("wait %s != probe %s for %+v", outcome, state, o)
		}
		if settled == (state == StateWorking || state == StateStarting) {
			t.Fatalf("wait settled=%v on %s", settled, state)
		}
		if sendOK := SendCheck(o, empty) == nil; sendOK != state.AcceptsInput() {
			t.Fatalf("send ok=%v but %s accepts=%v", sendOK, state, state.AcceptsInput())
		}
	}
}

func TestReady_GreySuggestionIsEmptyInput(t *testing.T) {
	for _, screen := range []string{
		"──────\n> " + dim + "Try \"fix the tests\"" + reset + "\n──────\n",
		"│ > " + grey + "run the linter" + reset + " │\n",
		"› " + "\x1b[38;5;244m" + "Summarize recent commits" + reset + "\n",
	} {
		in := ParseInput(screen)
		if !in.Read || in.Draft() {
			t.Fatalf("grey suggestion read as a draft: %+v from %q", in, screen)
		}
	}
	// Other side: the same text in the default colour is a draft.
	if in := ParseInput("> run the linter\n"); !in.Read || !in.Draft() {
		t.Fatalf("plain text not a draft: %+v", in)
	}
}

func TestReady_GreySuggestionNeverWorking(t *testing.T) {
	// The predicate takes no input-line text at all: working comes from the
	// hooks' turn events only, so a suggestion on the line cannot make it so.
	o := idle()
	if r := Ready(o); r.State != StateReady {
		t.Fatalf("%+v", r)
	}
	o.Turn = TurnRunning
	if r := Ready(o); r.State != StateWorking {
		t.Fatalf("%+v", r)
	}
}

func TestSend_DefaultColourDraftBlocks(t *testing.T) {
	draft := ParseInput("> half-typed by a person\n")
	if err := SendCheck(idle(), draft); !errors.Is(err, ErrDraft) {
		t.Fatalf("err %v", err)
	}
	if err := SendCheck(idle(), ParseInput("> "+dim+"suggested"+reset+"\n")); err != nil {
		t.Fatalf("grey suggestion blocked send: %v", err)
	}
	if err := SendCheck(idle(), ParseInput(">  \n")); err != nil {
		t.Fatalf("empty line blocked send: %v", err)
	}
}

func TestReady_StyledReadFailsCountsAsDraftForSendOnly(t *testing.T) {
	failed := Input{} // the styled read failed
	if !failed.Draft() {
		t.Fatal("failed read is not a draft")
	}
	if err := SendCheck(idle(), failed); !errors.Is(err, ErrDraft) {
		t.Fatalf("err %v", err)
	}
	// It never changes readiness: Ready takes no input at all.
	if Probe(idle()) != StateReady {
		t.Fatal("state changed")
	}
	// A screen with no prompt line is a failed read too.
	if in := ParseInput("just some output\n"); in.Read || !in.Draft() {
		t.Fatalf("%+v", in)
	}
}

func TestReady_MixedDimAndPlainIsDraft(t *testing.T) {
	in := ParseInput("> " + dim + "suggest" + reset + "ed by hand\n")
	if !in.Read || !in.Draft() {
		t.Fatalf("%+v", in)
	}
	in = ParseInput("> " + dim + "all grey" + "\x1b[22m" + "\n")
	if !in.Read || in.Draft() {
		t.Fatalf("%+v", in)
	}
}

func TestTrustDialog_Labels(t *testing.T) {
	cases := map[string]string{
		"❯ No, exit\n  Yes, I trust this folder": "claude",
		"› 1. Trust and continue\n  2. Quit":     "codex",
		"Yes, I trust this folder":               "",
		"1. Trust and continue":                  "",
		"plain output":                           "",
	}
	for screen, want := range cases {
		got := TrustDialog(screen)
		if (want == "") != (got == "") || (want != "" && got[:len(want)] != want) {
			t.Fatalf("%q: got %q want %q", screen, got, want)
		}
	}
}
