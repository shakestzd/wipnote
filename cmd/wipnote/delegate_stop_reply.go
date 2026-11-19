package main

import (
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// saveReviewResult is the Stop hook's saver (bug-9f3b580f): for a review
// worker it stores the turn's final message as the task result, the way
// delegate reply would. Any error is dropped, so the hook is never slowed or
// failed by it.
func saveReviewResult(projectDir, run, message string) {
	store, err := runstore.New(projectDir)
	if err != nil {
		return
	}
	_ = session.StopReply(session.Deps{Store: store}, run, message)
}
