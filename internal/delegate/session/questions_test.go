package session

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// writeQFile writes outbox/<task>-q<n>.json as the worker would.
func (e *env) writeQFile(name, task string, n int, body string) string {
	e.t.Helper()
	id := task + "-q" + string(rune('0'+n))
	data, _ := json.Marshal(map[string]any{"body": body, "options": []string{"yes", "no"}})
	path := filepath.Join(e.mailbox(name).Dir(), "outbox", id+".json")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		e.t.Fatal(err)
	}
	return id
}

// answer records an answer to qid as slice 17 will: an inbox note in reply.
func (e *env) answer(name, qid string) {
	e.t.Helper()
	if _, err := e.mailbox(name).Post(runstore.Inbox, runstore.Message{From: "caller", To: name, Type: runstore.TypeNote, Body: "yes", InReplyTo: qid}); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) questions(name string) []Question {
	e.t.Helper()
	qs, err := Questions(e.deps, name)
	if err != nil {
		e.t.Fatal(err)
	}
	return qs
}

func (e *env) state(name string) State {
	e.t.Helper()
	rows, err := List(context.Background(), e.deps, false)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, r := range rows {
		if r.Name == name {
			return r.State
		}
	}
	e.t.Fatalf("no row %s", name)
	return ""
}

func TestQuestions_OutboxQFileListedWithSilentHooks(t *testing.T) {
	e := newEnv(t)
	e.unbound("w1", "brief") // no hook ever fired
	qid := e.writeQFile("w1", e.task("w1"), 1, "Which branch?")
	qs := e.questions("w1")
	if len(qs) != 1 || qs[0].ID != qid || qs[0].Kind != KindQuestionFile || qs[0].Text != "Which branch?" || len(qs[0].Options) != 2 {
		t.Fatalf("%+v", qs)
	}
	if st := e.state("w1"); st != StateQuestion {
		t.Fatalf("status %s", st)
	}
}

func TestWait_OutboxQFileReturnsAsks(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	if res := e.wait("w1", 50*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("%+v", res)
	}
	qid := e.writeQFile("w1", e.task("w1"), 1, "Proceed?")
	res := e.wait("w1", time.Second)
	if res.Outcome != OutcomeAsks || len(res.Asks) != 1 || res.Asks[0].ID != qid {
		t.Fatalf("%+v", res)
	}
}

func TestQuestions_AnsweredQFileNotListed(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	q1 := e.writeQFile("w1", e.task("w1"), 1, "first?")
	q2 := e.writeQFile("w1", e.task("w1"), 2, "second?")
	e.answer("w1", q1)
	if qs := e.questions("w1"); len(qs) != 1 || qs[0].ID != q2 {
		t.Fatalf("%+v", qs)
	}
	e.answer("w1", q2)
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("%+v", qs)
	}
	if st := e.state("w1"); st != StateWorking {
		t.Fatalf("status %s after every ask was answered", st)
	}
}

func TestQuestions_NoQFileNotListed(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	// Not a q-file: a result, a stray name and an older task's question.
	e.writeResult("w1", e.task("w1"), "answer")
	stray := filepath.Join(e.mailbox("w1").Dir(), "outbox", "notes-q1.json")
	_ = os.WriteFile(stray, []byte(`{"body":"x"}`), 0o600)
	qs := e.questions("w1")
	if qs == nil || len(qs) != 0 {
		t.Fatalf("%#v", qs)
	}
	data, _ := json.Marshal(qs)
	if string(data) != "[]" {
		t.Fatalf("payload %s", data)
	}
}

func TestQuestions_OlderTaskQFileNotPending(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	old := e.task("w1")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	e.writeQFile("w1", old, 1, "stale?")
	if len(e.questions("w1")) != 1 {
		t.Fatal("the current task's q-file is not listed")
	}
	e.answer("w1", old+"-q1")
	e.send("w1", "next task")
	e.writeQFile("w1", old, 2, "stale again?")
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("an older task's question is pending: %+v", qs)
	}
}

func TestQuestions_PendingThenCleared(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindQuestion, func(ev *runstore.Event) {
		ev.Tool, ev.ToolUseID, ev.Summary = "AskUserQuestion", "tu-ask", "Which file? [a.go | b.go]"
	})
	qs := e.questions("w1")
	if len(qs) != 1 || qs[0].ID != "tu-ask" || qs[0].Kind != KindAsk || qs[0].Text != "Which file? [a.go | b.go]" {
		t.Fatalf("%+v", qs)
	}
	if st := e.state("w1"); st != StateQuestion {
		t.Fatalf("status %s", st)
	}
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "AskUserQuestion", "tu-ask" })
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("answered ask still listed: %+v", qs)
	}
	e.event(s.Run, "sid-1", runstore.KindPermissionRequest, func(ev *runstore.Event) { ev.Tool, ev.Summary = "Bash", "rm -rf build" })
	if qs := e.questions("w1"); len(qs) != 1 || qs[0].Kind != KindPermission || qs[0].Tool != "Bash" {
		t.Fatalf("%+v", qs)
	}
	e.event(s.Run, "sid-1", runstore.KindStop)
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("%+v", qs)
	}
}
