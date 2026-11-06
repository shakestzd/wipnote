package session

import (
	"context"
	"errors"
	"fmt"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ErrNoQuestion refuses an answer to a question the worker has no pending.
var ErrNoQuestion = errors.New("delegate: no such pending question")

// Answered is answer's payload. Hinted is false when the herdr hint failed
// or there was no herdr; the answer is in the inbox anyway.
type Answered struct {
	Run       string `json:"run"`
	Name      string `json:"name"`
	Question  string `json:"question"`
	Answer    string `json:"answer_id"`
	Hinted    bool   `json:"hinted"`
	HintError string `json:"hint_error,omitempty"`
}

// Answer records the caller's answer to a pending question: an inbox note in
// reply to the question id, which is what clears it from questions, list and
// wait; then a herdr hint types the text for the worker. The inbox is
// authoritative, like send's: a failed hint leaves the answer there.
func Answer(ctx context.Context, d Deps, name, qid, text string) (Answered, error) {
	_, _, info, err := findWorker(d, name)
	if err != nil {
		return Answered{}, err
	}
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return Answered{}, err
	}
	if !hasPending(d, name, qid) {
		return Answered{}, fmt.Errorf("%w: %q for %s (see delegate questions)", ErrNoQuestion, qid, name)
	}
	msg, err := mb.Post(runstore.Inbox, runstore.Message{
		Run: info.Run, From: "caller", To: info.Name, Type: runstore.TypeNote, Body: text, InReplyTo: qid,
	})
	if err != nil {
		return Answered{}, err
	}
	out := Answered{Run: info.Run, Name: name, Question: qid, Answer: msg.ID}
	if d.Herdr == nil {
		out.HintError = ErrNotInHerdr.Error()
		return out, nil
	}
	if err := d.Herdr.Prompt(ctx, info.Pin(), text); err != nil {
		out.HintError = err.Error()
		return out, nil
	}
	out.Hinted = true
	_, _ = mb.MarkHinted(msg)
	return out, nil
}

// hasPending reports whether qid is one of name's pending questions.
func hasPending(d Deps, name, qid string) bool {
	qs, err := Questions(d, name)
	if err != nil {
		return false
	}
	for _, q := range qs {
		if q.ID == qid {
			return true
		}
	}
	return false
}
