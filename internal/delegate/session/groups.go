package session

import "github.com/shakestzd/wipnote/core/delegationledger"

// Group is where a run sits in the Projects Overview's state language
// (plan-28b4a7f6 slice 25; decision P: delegate plus herdr is a
// multi-harness Projects harness). Ready stays the one source of a worker's
// state (D2); this file only maps states to groups.
type Group string

// The Overview groups.
const (
	GroupWorking        Group = "working"
	GroupWaitingOnYou   Group = "waiting_on_you" // a reply, an approval, or a failure
	GroupReadyForReview Group = "ready_for_review"
	GroupLanding        Group = "landing" // its PR is approved or queued
	GroupIdle           Group = "idle"
	GroupResolved       Group = "resolved"
)

// Groups is every group in the order list, tree, the band and run.html
// present them.
var Groups = []Group{GroupWorking, GroupWaitingOnYou, GroupReadyForReview, GroupLanding, GroupIdle, GroupResolved}

var groupLabels = map[Group]string{
	GroupWorking: "Working", GroupWaitingOnYou: "Waiting on you", GroupReadyForReview: "Ready for review",
	GroupLanding: "Landing", GroupIdle: "Idle", GroupResolved: "Resolved",
}

// Label is the group's display name; "" for no group.
func (g Group) Label() string { return groupLabels[g] }

// StateFailed is an ended run whose exit code was not 0.
const StateFailed State = "failed"

// stateGroups is the table. Waiting on you holds what statusall's needs_you
// lists for runs and agents: blocked (blocked_agent), failed (failed_run)
// and gone (lost_run); plus a question and a turn that ended owing its result.
var stateGroups = map[State]Group{
	StateStarting:     GroupWorking,
	StateReady:        GroupIdle,
	StateWorking:      GroupWorking,
	StateQuestion:     GroupWaitingOnYou,
	StateDone:         GroupReadyForReview, // the result file waits to be read
	StateIdleNoResult: GroupWaitingOnYou,
	StateIdleShell:    GroupWorking, // a background shell still runs
	StateBlocked:      GroupWaitingOnYou,
	StateGone:         GroupWaitingOnYou,
	StateRunning:      GroupWorking,
	StateEnded:        GroupResolved,
	StateFailed:       GroupWaitingOnYou,
}

// AllStates is every readiness state, then every run state.
func AllStates() []State {
	return []State{
		StateStarting, StateReady, StateWorking, StateQuestion, StateDone,
		StateIdleNoResult, StateIdleShell, StateBlocked, StateGone,
		StateRunning, StateEnded, StateFailed,
	}
}

// GroupOf is a state's group; ok is false for a state the table lacks.
func GroupOf(s State) (Group, bool) {
	g, ok := stateGroups[s]
	return g, ok
}

// PRState is the state of a run's pull request; PRNone when it has none.
type PRState string

// PR states.
const (
	PRNone     PRState = ""
	PROpen     PRState = "open"
	PRApproved PRState = "approved"
	PRQueued   PRState = "queued"
	PRMerged   PRState = "merged"
	PRClosed   PRState = "closed"
)

// GroupFor is a run's group given its PR. A run that is working or waits
// on you keeps that group; otherwise an open PR is ready for review, an
// approved or queued one is landing and a merged one is resolved. No PR or
// a closed one leaves the state's own group.
func GroupFor(s State, pr PRState) Group {
	g, _ := GroupOf(s)
	if g == GroupWorking || g == GroupWaitingOnYou {
		return g
	}
	switch pr {
	case PROpen:
		return GroupReadyForReview
	case PRApproved, PRQueued:
		return GroupLanding
	case PRMerged:
		return GroupResolved
	}
	return g
}

// RunStateOf is the state of a run that is not an open interactive worker,
// from its ledger row or its run folder: running while open; once ended,
// gone for a lost run, failed for a non-zero exit, else ended.
func RunStateOf(open bool, exit *int, endReason string) State {
	switch {
	case open:
		return StateRunning
	case endReason == delegationledger.EndGone:
		return StateGone
	case exit != nil && *exit != 0:
		return StateFailed
	}
	return StateEnded
}
