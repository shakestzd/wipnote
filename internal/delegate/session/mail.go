package session

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/filelock"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// ErrTaskUnacked refuses a send while the worker has not started its newest
// task: its readiness still reflects the turn before, so a second task would
// be acked in the first one's place.
var ErrTaskUnacked = errors.New("delegate: the worker has not acked its newest task yet")

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
// readiness predicate accepts input, the input line holds no draft
// (SendCheck, D2 and D8) and the worker acked its newest task. That whole
// decision and the post run under the worker's send lock, so no reading
// taken before the lock decides anything. The inbox file (with the result
// preamble) is written first; only then is the hint typed (herdr agent
// prompt), after the lock is released.
func Send(ctx context.Context, d Deps, name, text string) (Sent, error) {
	_, rf, info, err := findWorker(d, name)
	if err != nil {
		return Sent{}, err
	}
	if d.Herdr == nil {
		return Sent{}, ErrNotInHerdr
	}
	if adopt(ctx, d.Herdr, &info) {
		_ = WriteInfo(rf, info)
	}
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return Sent{}, err
	}
	release := filelock.Guard(filepath.Join(mb.Dir(), sendLock))
	msg, turn, err := decideAndPost(ctx, d, rf, info, mb, text)
	release()
	if err != nil {
		return Sent{}, err
	}
	sent := Sent{Run: info.Run, Name: name, Task: msg.ID, Turn: turn}
	if err := d.Herdr.Prompt(ctx, info.Pin(), msg.Body); err != nil {
		sent.HintError = err.Error()
		return sent, nil
	}
	sent.Hinted = true
	_, _ = mb.MarkHinted(msg)
	return sent, nil
}

// sendLock, in the worker's mailbox, makes send's decision and post one step.
const sendLock = "send.lock"

// decideAndPost runs under sendLock: the row must still be open, the
// readiness and the input line are read now, the run's newest task must be
// acked, and then the task is posted, tagged with the run.
func decideAndPost(ctx context.Context, d Deps, rf *runstore.RunFolder, info Info, mb *runstore.Mailbox, text string) (runstore.Message, int, error) {
	if r, ok, err := d.Ledger.Get(info.Run); err != nil || !ok || !r.IsOpen() {
		return runstore.Message{}, 0, fmt.Errorf("%w: %q (run %s ended)", ErrNoWorker, info.Name, info.Run)
	}
	o, _ := observe(ctx, d, rf, info)
	if err := SendCheck(o, ReadInput(ctx, d.Herdr, herdr.PaneID(info.Pane))); err != nil {
		return runstore.Message{}, 0, err
	}
	t, ok := newestTask(d.Store, info.Name, info.Run)
	if ok && !t.acked {
		return runstore.Message{}, 0, fmt.Errorf("%w: task %s is pending; wait for the worker (delegate wait) or check delegate inbox in the worker",
			ErrTaskUnacked, t.msg.ID)
	}
	info.Task = runstore.NewMsgID()
	preamble, err := WorkerPreamble(d.Store, info)
	if err != nil {
		return runstore.Message{}, 0, err
	}
	msg, err := mb.Post(runstore.Inbox, runstore.Message{
		ID: info.Task, Run: info.Run, From: "caller", To: info.Name, Type: runstore.TypeTask, Turn: t.turn + 1,
		Body: preamble + text,
	})
	return msg, t.turn + 1, err
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
	msg, ok, err := mb.NewestResultFor(runTasks(mb, r.ID)) // this run's tasks only
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
		if m.Run != info.Run {
			continue // an earlier run with the same name
		}
		acked := mb.Acked(m.ID)
		rows = append(rows, InboxRow{ID: m.ID, Seq: m.Seq, Type: m.Type, From: m.From, Turn: m.Turn,
			InReplyTo: m.InReplyTo, Created: m.Created, Hinted: m.Hinted, Acked: acked,
			Lost: m.Type == runstore.TypeTask && !m.Hinted && !acked, Body: m.Body})
	}
	return rows, nil
}

// runTasks accepts the task ids run posted to mb.
func runTasks(mb *runstore.Mailbox, run string) func(string) bool {
	ids := map[string]bool{}
	if msgs, err := mb.Read(runstore.Inbox); err == nil {
		for _, m := range msgs {
			if m.Type == runstore.TypeTask && m.Run == run {
				ids[m.ID] = true
			}
		}
	}
	return func(task string) bool { return ids[task] }
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
