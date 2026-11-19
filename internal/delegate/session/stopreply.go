package session

import (
	"strings"
)

// StopReply saves a review worker's final message as the result of its
// newest task (bug-9f3b580f). The review profile has no shell, so the worker
// cannot run delegate reply; its Stop hook calls this instead. It goes
// through Reply, so the file and the ledger effect are the same. It writes
// nothing for another profile, an empty message, a run with no task, or a
// task that already has a result: the worker's own answer wins.
func StopReply(d Deps, runID, message string) error {
	if strings.TrimSpace(message) == "" {
		return nil
	}
	_, info, err := loadInfo(d.Store, runID)
	if err != nil || info.Setup.Profile != ProfileReview {
		return err
	}
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return err
	}
	t, ok := newestTask(d.Store, info.Name, info.Run)
	if !ok {
		return nil
	}
	if _, found, err := mb.Result(t.msg.ID); err != nil || found {
		return err
	}
	_, err = Reply(d, runID, ReplyOptions{Task: t.msg.ID, Body: message})
	return err
}
