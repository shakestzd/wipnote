package session

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// DefaultWatchTimeout stays under Claude Code's Monitor cap of 30 minutes.
const DefaultWatchTimeout = 25 * time.Minute

// WatchRerun is the last line of a watch that timed out.
const WatchRerun = "watch timed out: run delegate watch again to keep watching"

// askBound caps the question text one watch line carries.
const askBound = 120

// stateWorking is the watch state of a worker with no durable signal yet.
const stateWorking = "working"

// Watch is wait's loop with an observer: emit gets one line each time the
// observed state changes, and nothing while it holds. Unlike wait, a pending
// question is not terminal: Watch prints it and keeps watching. Every other
// settled outcome, and the timeout, ends the watch; its result carries the
// exit status. Like wait it writes no run state.
func Watch(ctx context.Context, d Deps, name string, o WaitOptions, emit func(line string)) (WaitResult, error) {
	last := ""
	res, err := lookLoop(ctx, d, name, o, func(res WaitResult, settled bool) bool {
		if key := watchKey(res, settled); key != last {
			last = key
			emit(WatchLine(res, settled))
		}
		return settled && res.Outcome != OutcomeAsks
	})
	if err == nil && res.Outcome == OutcomeTimeout {
		emit(fmt.Sprintf("run=%s name=%s state=timeout", res.Run, res.Name))
		emit(WatchRerun)
	}
	return res, err
}

// watchKey identifies the observed state: the outcome, and for asks the
// pending question ids, so a new question prints again.
func watchKey(res WaitResult, settled bool) string {
	if !settled {
		return stateWorking
	}
	key := res.Outcome
	for _, q := range res.Asks {
		key += "|" + q.ID
	}
	return key
}

// WatchLine renders one state as a stable key=value line.
func WatchLine(res WaitResult, settled bool) string {
	state := stateWorking
	if settled {
		state = res.Outcome
	}
	line := fmt.Sprintf("run=%s name=%s state=%s", res.Run, res.Name, state)
	if settled && res.Outcome == OutcomeAsks && len(res.Asks) > 0 {
		line += fmt.Sprintf(" asks=%d q=%q", len(res.Asks), boundText(res.Asks[0].Text))
	}
	return line
}

// boundText collapses s to one line of at most askBound runes.
func boundText(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > askBound {
		return string(r[:askBound]) + "..."
	}
	return s
}
