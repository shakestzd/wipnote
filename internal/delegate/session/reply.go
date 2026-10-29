package session

import (
	"fmt"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Reply kinds.
const (
	ReplyResult   = "result"
	ReplyQuestion = "question"
)

// ReplyOut is reply's payload: the file the worker's answer went to.
type ReplyOut struct {
	Run  string `json:"run"`
	Name string `json:"name"`
	Task string `json:"task"`
	ID   string `json:"id"`
	Kind string `json:"kind"`
}

// ReplyOptions say what a worker replies: the body, and for a question
// (Ask) the options its caller picks from.
type ReplyOptions struct {
	Task    string
	Body    string
	Ask     bool
	Options []string
}

// Reply writes a worker's answer to a task of its own run through the run
// store, so a worker whose hooks guard .wipnote/ still delivers it
// (bug-66574a82): the task's result file, or with Ask its next question
// file. An empty Task means the run's newest task.
func Reply(d Deps, runID string, o ReplyOptions) (ReplyOut, error) {
	if strings.TrimSpace(o.Body) == "" {
		return ReplyOut{}, fmt.Errorf("delegate reply: the reply is empty")
	}
	_, info, err := loadInfo(d.Store, runID)
	if err != nil {
		return ReplyOut{}, err
	}
	mb, err := d.Store.Mailbox(info.Name)
	if err != nil {
		return ReplyOut{}, err
	}
	task := o.Task
	if task == "" {
		t, ok := newestTask(d.Store, info.Name, info.Run)
		if !ok {
			return ReplyOut{}, fmt.Errorf("delegate reply: run %s has no task to answer", info.Run)
		}
		task = t.msg.ID
	} else if !runTasks(mb, info.Run)(task) {
		return ReplyOut{}, fmt.Errorf("delegate reply: %s is not a task of run %s", task, info.Run)
	}
	out := ReplyOut{Run: info.Run, Name: info.Name, Task: task, Kind: ReplyResult}
	if o.Ask {
		out.Kind = ReplyQuestion
		out.ID, err = mb.WriteQFile(task, o.Body, o.Options)
		return out, err
	}
	out.ID = task + "-result"
	return out, mb.WriteResult(task, runstore.Message{Run: info.Run, From: info.Name, To: "caller", Body: o.Body})
}
