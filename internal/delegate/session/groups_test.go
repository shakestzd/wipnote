package session

import (
	"bytes"
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Every readiness state slice 10 defines and every run state list reports.
var everyState = []State{
	StateStarting, StateReady, StateWorking, StateQuestion, StateDone,
	StateIdleNoResult, StateIdleShell, StateBlocked, StateGone,
	StateRunning, StateEnded, StateFailed,
}

func TestGroups_EveryStateMapped(t *testing.T) {
	if !slices.Equal(Groups, []Group{GroupWorking, GroupWaitingOnYou, GroupReadyForReview, GroupLanding, GroupIdle, GroupResolved}) {
		t.Fatalf("groups %v: want the Overview's six, in its order", Groups)
	}
	labels := map[string]bool{}
	for _, g := range Groups {
		if g.Label() == "" || g.Label() == string(g) || labels[g.Label()] {
			t.Fatalf("group %q has label %q: want a distinct display name", g, g.Label())
		}
		labels[g.Label()] = true
	}
	if !slices.Equal(AllStates(), everyState) {
		t.Fatalf("AllStates %v, want %v", AllStates(), everyState)
	}
	for _, s := range everyState {
		g, ok := GroupOf(s)
		if !ok || !slices.Contains(Groups, g) {
			t.Fatalf("state %q maps to %q ok=%v: want exactly one Overview group", s, g, ok)
		}
	}
	// Other side: a state nobody defined has no group, so a new state that
	// is not added to the table cannot be shown under a guessed group.
	for _, s := range []State{"", "bogus", "open"} {
		if g, ok := GroupOf(s); ok || g != "" {
			t.Fatalf("unknown state %q maps to %q", s, g)
		}
	}
}

func TestGroups_QuestionIsWaitingOnYou(t *testing.T) {
	for _, s := range []State{StateQuestion, StateBlocked, StateIdleNoResult, StateGone} {
		if g, _ := GroupOf(s); g != GroupWaitingOnYou {
			t.Fatalf("%s maps to %s, want waiting_on_you", s, g)
		}
	}
	// Other side: a worker that is busy, idle or finished waits on no one.
	want := map[State]Group{
		StateStarting: GroupWorking, StateWorking: GroupWorking, StateIdleShell: GroupWorking,
		StateReady: GroupIdle, StateDone: GroupReadyForReview,
	}
	for s, w := range want {
		if g, _ := GroupOf(s); g != w {
			t.Fatalf("%s maps to %s, want %s", s, g, w)
		}
	}
}

func TestGroups_FailedIsWaitingOnYou(t *testing.T) {
	two, zero := 2, 0
	cases := []struct {
		open   bool
		exit   *int
		reason string
		state  State
		group  Group
	}{
		{false, &two, delegationledger.EndExit, StateFailed, GroupWaitingOnYou},
		{false, nil, delegationledger.EndGone, StateGone, GroupWaitingOnYou},
		{false, &zero, delegationledger.EndExit, StateEnded, GroupResolved},
		{false, nil, delegationledger.EndStop, StateEnded, GroupResolved},
		{false, nil, delegationledger.EndPaneClosed, StateEnded, GroupResolved},
		{false, nil, delegationledger.EndImported, StateEnded, GroupResolved},
		{true, nil, "", StateRunning, GroupWorking},
	}
	for _, c := range cases {
		s := RunStateOf(c.open, c.exit, c.reason)
		if g, _ := GroupOf(s); s != c.state || g != c.group {
			t.Fatalf("open=%v exit=%v reason=%q: state %s group %s, want %s %s", c.open, c.exit, c.reason, s, g, c.state, c.group)
		}
	}
}

func TestGroups_PRStates(t *testing.T) {
	cases := []struct {
		state State
		pr    PRState
		want  Group
	}{
		{StateEnded, PROpen, GroupReadyForReview},
		{StateDone, PROpen, GroupReadyForReview},
		{StateReady, PRApproved, GroupLanding},
		{StateEnded, PRQueued, GroupLanding},
		{StateDone, PRMerged, GroupResolved},
		// Other side: no PR or a closed one leaves the state's own group, and
		// a worker that is busy or needs you keeps that group over any PR.
		{StateEnded, PRNone, GroupResolved},
		{StateReady, PRClosed, GroupIdle},
		{StateQuestion, PRApproved, GroupWaitingOnYou},
		{StateFailed, PROpen, GroupWaitingOnYou},
		{StateWorking, PROpen, GroupWorking},
	}
	for _, c := range cases {
		if g := GroupFor(c.state, c.pr); g != c.want {
			t.Fatalf("GroupFor(%s, %q) = %s, want %s", c.state, c.pr, g, c.want)
		}
	}
}

// list prints the group beside the state, and a worker's observed state is
// kept in its run folder so the band (which runs no predicate) and tree show
// the same group.
func TestGroups_ListShowsGroupAndKeepsObserved(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	e.event(s.Run, "sid-one", runstore.KindSessionStart)
	e.event(s.Run, "sid-one", runstore.KindQuestion)
	rows, err := List(context.Background(), e.deps, false)
	if err != nil || len(rows) != 1 || rows[0].State != StateQuestion || rows[0].Group != GroupWaitingOnYou {
		t.Fatalf("rows %+v err %v", rows, err)
	}
	got, ok := ReadObserved(e.runFolder(s.Run))
	if !ok || got.State != StateQuestion || got.Group != GroupWaitingOnYou {
		t.Fatalf("observed %+v ok=%v", got, ok)
	}
	nodes, err := Tree(e.deps, "")
	if err != nil || len(nodes) != 1 || nodes[0].Runs[0].Group != GroupWaitingOnYou {
		t.Fatalf("tree %+v err %v", nodes, err)
	}
	var text bytes.Buffer
	RenderTree(&text, nodes)
	if !strings.Contains(text.String(), "question · Waiting on you") {
		t.Fatalf("tree text lacks the group:\n%s", text.String())
	}
	// Other side: the answer clears the question at the next list.
	e.event(s.Run, "sid-one", runstore.KindToolEnd)
	rows, _ = List(context.Background(), e.deps, false)
	if got, _ := ReadObserved(e.runFolder(s.Run)); rows[0].Group != GroupIdle || got.Group != GroupIdle {
		t.Fatalf("after answer: row %+v observed %+v", rows[0], got)
	}
}

// An ended run's group comes from the ledger, never from a stale observed file.
func TestGroups_EndedRunIgnoresObserved(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	e.event(s.Run, "sid-one", runstore.KindSessionStart)
	e.event(s.Run, "sid-one", runstore.KindQuestion)
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if err := e.deps.Ledger.Close(s.Run, delegationledger.CloseInfo{Reason: delegationledger.EndStop}); err != nil {
		t.Fatal(err)
	}
	rows, _ := List(context.Background(), e.deps, true)
	nodes, _ := Tree(e.deps, "")
	if rows[0].State != StateEnded || rows[0].Group != GroupResolved || nodes[0].Runs[0].Group != GroupResolved {
		t.Fatalf("row %+v tree %+v", rows[0], nodes[0].Runs[0])
	}
}
