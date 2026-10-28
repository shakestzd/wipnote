package session

import (
	"fmt"

	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Question kinds.
const (
	KindAsk          = "ask"           // an AskUserQuestion call (PreToolUse hook)
	KindPermission   = "permission"    // a PermissionRequest prompt (hook)
	KindQuestionFile = "question_file" // an outbox <task-id>-q<n>.json (D7)
)

// Question is one pending ask, as questions prints it. ID is the tool use id
// for a hook ask and <task-id>-q<n> for a question file; slice 17's answer
// takes it.
type Question struct {
	ID      string   `json:"id"`
	Kind    string   `json:"kind"`
	Task    string   `json:"task,omitempty"`
	Tool    string   `json:"tool,omitempty"`
	Text    string   `json:"text,omitempty"`
	Options []string `json:"options,omitempty"`
}

// Questions lists name's pending asks: the current session's unanswered
// AskUserQuestion and PermissionRequest events, then the newest task's
// unanswered question files, which count even when the hooks are silent. A
// closed run has none. It never returns nil.
func Questions(d Deps, name string) ([]Question, error) {
	r, err := findRow(d, name)
	if err != nil {
		return nil, err
	}
	return runQuestions(d, r)
}

// RunQuestions is Questions for one run id (the run view's).
func RunQuestions(d Deps, runID string) ([]Question, error) {
	r, ok, err := d.Ledger.Get(runID)
	if err != nil || !ok {
		return nil, fmt.Errorf("delegate: run %s has no ledger row: %v", runID, err)
	}
	return runQuestions(d, r)
}

func runQuestions(d Deps, r delegationledger.Record) ([]Question, error) {
	if !r.IsOpen() {
		return []Question{}, nil
	}
	rf, err := d.Store.Run(r.ID)
	if err != nil {
		return nil, err
	}
	evs, _ := rf.ReadEvents()
	return pendingQuestions(d.Store, r.Name, r.ID, factsOf(evs)), nil
}

func pendingQuestions(store *runstore.Store, name, run string, f eventFacts) []Question {
	out := []Question{}
	for _, ev := range f.asks {
		kind := KindAsk
		if ev.Kind == runstore.KindPermissionRequest {
			kind = KindPermission
		}
		out = append(out, Question{ID: ev.ToolUseID, Kind: kind, Tool: ev.Tool, Text: ev.Summary})
	}
	for _, q := range pendingQFiles(store, name, run) {
		out = append(out, Question{ID: q.ID, Kind: KindQuestionFile, Task: q.Task, Text: q.Text(), Options: q.Options})
	}
	return out
}

// pendingQFiles are the newest task's question files with no answer yet. An
// older task's question is not pending: the caller has moved on.
func pendingQFiles(store *runstore.Store, name, run string) []runstore.QFile {
	t, ok := newestTask(store, name, run)
	if !ok {
		return nil
	}
	qs, err := t.mb.QFiles(t.msg.ID)
	if err != nil {
		return nil
	}
	var out []runstore.QFile
	for _, q := range qs {
		if answered, err := t.mb.Answered(q.ID); err == nil && !answered {
			out = append(out, q)
		}
	}
	return out
}
