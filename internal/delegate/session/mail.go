package session

import (
	"context"
	"fmt"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Sent is send's payload. Hinted is false when the herdr hint failed; the
// task is in the inbox anyway (the store is authoritative, the hint has no
// receipt), and the worker's delegate inbox shows it.
type Sent struct {
	Run       string `json:"run"`
	Name      string `json:"name"`
	Task      string `json:"task"`
	Turn      int    `json:"turn"`
	Hinted    bool   `json:"hinted"`
	HintError string `json:"hint_error,omitempty"`
}

// Send gives a live worker a task. It refuses, writing nothing, unless the
// readiness predicate accepts input and the input line holds no draft
// (SendCheck, D2 and D8). Then it writes the inbox file with the result
// preamble first and only then types the hint (herdr agent prompt).
func Send(ctx context.Context, d Deps, name, text string) (Sent, error) {
	r, rf, info, err := findWorker(d, name)
	if err != nil {
		return Sent{}, err
	}
	if d.Herdr == nil {
		return Sent{}, ErrNotInHerdr
	}
	if adopt(ctx, d.Herdr, &info) {
		_ = WriteInfo(rf, info)
	}
	o, _ := observe(ctx, d, rf, info)
	if err := SendCheck(o, ReadInput(ctx, d.Herdr, herdr.PaneID(info.Pane))); err != nil {
		return Sent{}, err
	}
	mb, msg, turn, err := postTask(d, info, text)
	if err != nil {
		return Sent{}, err
	}
	sent := Sent{Run: r.ID, Name: name, Task: msg.ID, Turn: turn}
	if err := d.Herdr.Prompt(ctx, info.Pin(), msg.Body); err != nil {
		sent.HintError = err.Error()
		return sent, nil
	}
	sent.Hinted = true
	_, _ = mb.MarkHinted(msg)
	return sent, nil
}

// postTask writes the task to the inbox with the preamble naming its result
// and question files.
func postTask(d Deps, info Info, text string) (*runstore.Mailbox, runstore.Message, int, error) {
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return nil, runstore.Message{}, 0, err
	}
	turn := 1
	if t, ok := newestTask(d.Store, info.Name); ok {
		turn = t.turn + 1
	}
	info.Task = runstore.NewMsgID()
	msg, err := mb.Post(runstore.Inbox, runstore.Message{
		ID: info.Task, From: "caller", To: info.Name, Type: runstore.TypeTask, Turn: turn,
		Body: Preamble(info, mb.Dir()) + text,
	})
	return mb, msg, turn, err
}

// Result values.
const (
	ResultFound = "found"
	ResultNone  = "none"
)

// ResultOut is result's payload: the newest result file's body and the task
// it answers, or result "none" (never null).
type ResultOut struct {
	Run     string `json:"run"`
	Name    string `json:"name"`
	Status  string `json:"result"`
	Task    string `json:"task,omitempty"`
	Body    string `json:"body,omitempty"`
	Created string `json:"created,omitempty"`
}

// Result returns the newest result file name's worker wrote, whatever the
// run's state or which task it answers (D3).
func Result(d Deps, name string) (ResultOut, error) {
	r, err := findRow(d, name)
	if err != nil {
		return ResultOut{}, err
	}
	out := ResultOut{Run: r.ID, Name: name, Status: ResultNone}
	mb, err := d.Store.Mailbox(name)
	if err != nil {
		return out, err
	}
	msg, ok, err := mb.NewestResult()
	if err != nil || !ok {
		return out, err
	}
	out.Status, out.Task, out.Body, out.Created = ResultFound, msg.InReplyTo, msg.Body, msg.Created
	return out, nil
}

// InboxRow is one inbox message as the worker's delegate inbox prints it.
// Lost marks a task whose hint failed and that the worker never acked.
type InboxRow struct {
	ID        string `json:"id"`
	Seq       int64  `json:"seq"`
	Type      string `json:"type"`
	From      string `json:"from"`
	Turn      int    `json:"turn,omitempty"`
	InReplyTo string `json:"in_reply_to,omitempty"`
	Created   string `json:"created"`
	Hinted    bool   `json:"hinted"`
	Acked     bool   `json:"acked"`
	Lost      bool   `json:"lost"`
	Body      string `json:"body"`
}

// Inbox is the worker-side read of its own mailbox, named by its run's
// session.json. It never returns nil.
func Inbox(d Deps, runID string) ([]InboxRow, error) {
	_, info, err := loadInfo(d.Store, runID)
	if err != nil {
		return nil, err
	}
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return nil, err
	}
	msgs, err := mb.Read(runstore.Inbox)
	if err != nil {
		return nil, err
	}
	rows := []InboxRow{}
	for _, m := range msgs {
		acked := mb.Acked(m.ID)
		rows = append(rows, InboxRow{ID: m.ID, Seq: m.Seq, Type: m.Type, From: m.From, Turn: m.Turn,
			InReplyTo: m.InReplyTo, Created: m.Created, Hinted: m.Hinted, Acked: acked,
			Lost: m.Type == runstore.TypeTask && !m.Hinted && !acked, Body: m.Body})
	}
	return rows, nil
}

// findRow returns the newest interactive row named name, open or closed.
func findRow(d Deps, name string) (delegationledger.Record, error) {
	recs, err := d.Ledger.ReadAll()
	if err != nil {
		return delegationledger.Record{}, err
	}
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Mode == delegationledger.ModeInteractive && recs[i].Name == name {
			return recs[i], nil
		}
	}
	return delegationledger.Record{}, fmt.Errorf("%w: %q", ErrNoWorker, name)
}
