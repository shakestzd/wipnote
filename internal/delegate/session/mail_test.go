package session

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// worker spawns name with brief ("" for none), binds session sid-1 and
// leaves an empty styled input line, so send may type.
func (e *env) worker(name, brief string) Spawned {
	e.t.Helper()
	o := e.opts(name)
	o.Brief = brief
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		e.t.Fatalf("spawn %s: %v", name, err)
	}
	e.herdr.prompts = true
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.event(s.Run, "sid-1", runstore.KindSessionStart)
	return s
}

// unbound spawns name with brief and writes no event at all (silent hooks).
func (e *env) unbound(name, brief string) Spawned {
	e.t.Helper()
	o := e.opts(name)
	o.Brief = brief
	s, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		e.t.Fatalf("spawn %s: %v", name, err)
	}
	return s
}

func (e *env) mailbox(name string) *runstore.Mailbox {
	e.t.Helper()
	mb, err := e.deps.Store.Mailbox(name)
	if err != nil {
		e.t.Fatal(err)
	}
	return mb
}

// task is the newest inbox task of name.
func (e *env) task(name string) string {
	e.t.Helper()
	msgs, err := e.mailbox(name).Read(runstore.Inbox)
	if err != nil {
		e.t.Fatal(err)
	}
	for i := len(msgs) - 1; i >= 0; i-- {
		if msgs[i].Type == runstore.TypeTask {
			return msgs[i].ID
		}
	}
	e.t.Fatalf("%s has no task", name)
	return ""
}

func (e *env) writeResult(name, task, body string) {
	e.t.Helper()
	if err := e.mailbox(name).WriteResult(task, runstore.Message{From: name, To: "caller", Body: body}); err != nil {
		e.t.Fatal(err)
	}
}

// startTurn is what the worker's UserPromptSubmit hook does for the newest
// task: the prompt event, then the ack.
func (e *env) startTurn(s Spawned, sid string) {
	e.t.Helper()
	e.event(s.Run, sid, runstore.KindPrompt)
	if err := e.mailbox(s.Name).Ack(e.task(s.Name)); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) send(name, body string) Sent {
	e.t.Helper()
	sent, err := Send(context.Background(), e.deps, name, body)
	if err != nil {
		e.t.Fatalf("send %s: %v", name, err)
	}
	return sent
}

func TestSend_StoreFirstThenHint(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	var storedAtHint []runstore.Message
	e.herdr.onPrompt = func(string) { storedAtHint, _ = e.mailbox("w1").Read(runstore.Inbox) }
	sent := e.send("w1", "Review the diff.")
	if len(storedAtHint) != 1 || storedAtHint[0].ID != sent.Task || storedAtHint[0].Hinted {
		t.Fatalf("the inbox file was not written before the hint: %+v", storedAtHint)
	}
	if !sent.Hinted || sent.Turn != 1 || sent.Run != s.Run {
		t.Fatalf("%+v", sent)
	}
	if msgs, _ := e.mailbox("w1").Read(runstore.Inbox); len(msgs) != 1 || !msgs[0].Hinted {
		t.Fatalf("hint not marked: %+v", msgs)
	}
	// Other side: a lost hint keeps the stored message, marked unhinted.
	e.herdr.promptErr = errors.New("herdr: agent busy")
	e.herdr.onPrompt = nil
	e.event(s.Run, "sid-1", runstore.KindPrompt)
	e.event(s.Run, "sid-1", runstore.KindStop)
	lost := e.send("w1", "Second task.")
	if lost.Hinted || lost.HintError == "" {
		t.Fatalf("%+v", lost)
	}
	if msgs, _ := e.mailbox("w1").Read(runstore.Inbox); len(msgs) != 2 || msgs[1].Hinted {
		t.Fatalf("lost message not kept: %+v", msgs)
	}
}

func TestSend_AppendsResultPreamble(t *testing.T) {
	e := newEnv(t)
	e.worker("w1", "")
	sent := e.send("w1", "Count the files.")
	msgs, _ := e.mailbox("w1").Read(runstore.Inbox)
	body := msgs[0].Body
	want := filepath.Join(e.mailbox("w1").Dir(), "outbox", sent.Task+"-result.json")
	if !strings.Contains(body, want) || !strings.HasSuffix(strings.TrimSpace(body), "Count the files.") {
		t.Fatalf("body %q lacks the result preamble for %s", body, want)
	}
	if !strings.Contains(body, sent.Task+"-q1.json") {
		t.Fatalf("body %q does not say where a question goes", body)
	}
	calls := e.herdr.Calls()
	if i := indexOfCall(calls, "prompt "); i < 0 || !strings.Contains(calls[i], want) {
		t.Fatalf("hint %v lacks the preamble", calls)
	}
}

func TestSend_DraftOrWorkingRefusesAndWritesNothing(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	e.herdr.pane(PaneOf(s)).styled = "> half typed\n"
	if _, err := Send(context.Background(), e.deps, "w1", "x"); !errors.Is(err, ErrDraft) {
		t.Fatalf("err %v", err)
	}
	e.herdr.pane(PaneOf(s)).styled = "> \n"
	e.event(s.Run, "sid-1", runstore.KindPrompt)
	if _, err := Send(context.Background(), e.deps, "w1", "x"); !errors.Is(err, ErrNotAccepting) {
		t.Fatalf("err %v", err)
	}
	if msgs, _ := e.mailbox("w1").Read(runstore.Inbox); len(msgs) != 0 || hasCall(e.herdr.Calls(), "prompt ") {
		t.Fatalf("a refused send wrote %+v / prompted", msgs)
	}
}

func TestResult_NewestFileWhileWorking(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.writeResult("w1", e.task("w1"), "partial answer")
	if rows, _ := List(context.Background(), e.deps, false); rows[0].State != StateWorking {
		t.Fatalf("state %s", rows[0].State)
	}
	r, err := Result(e.deps, "w1")
	if err != nil || r.Status != ResultFound || r.Body != "partial answer" || r.Task != e.task("w1") {
		t.Fatalf("%+v err %v", r, err)
	}
}

func TestResult_NewestFileAfterWatermark(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	first := e.task("w1")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	second := e.send("w1", "next").Task
	time.Sleep(20 * time.Millisecond) // a later mtime
	e.writeResult("w1", first, "late answer to the first task")
	r, err := Result(e.deps, "w1")
	if err != nil || r.Task != first || r.Body != "late answer to the first task" {
		t.Fatalf("%+v err %v", r, err)
	}
	// Other side: the stale result never settles a wait on the second task.
	if res := e.wait("w1", 80*time.Millisecond); res.Outcome != OutcomeTimeout || res.Task != second {
		t.Fatalf("%+v", res)
	}
}

func TestResult_NoFileSaysNoneNotNull(t *testing.T) {
	e := newEnv(t)
	e.worker("w1", "brief")
	r, err := Result(e.deps, "w1")
	if err != nil || r.Status != ResultNone {
		t.Fatalf("%+v err %v", r, err)
	}
	data, _ := json.Marshal(r)
	if !strings.Contains(string(data), `"result":"none"`) || strings.Contains(string(data), "null") {
		t.Fatalf("payload %s", data)
	}
	e.writeResult("w1", e.task("w1"), "answer")
	if r, _ := Result(e.deps, "w1"); r.Status != ResultFound {
		t.Fatalf("%+v", r)
	}
}

func TestInbox_ShowsLostHintMessages(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	kept := e.send("w1", "first")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	e.herdr.promptErr = errors.New("no receipt")
	lost := e.send("w1", "second")
	rows, err := Inbox(e.deps, s.Run)
	if err != nil || len(rows) != 2 {
		t.Fatalf("%+v err %v", rows, err)
	}
	if rows[0].ID != kept.Task || rows[0].Lost || !rows[0].Acked {
		t.Fatalf("hinted, acked message reads lost: %+v", rows[0])
	}
	if rows[1].ID != lost.Task || !rows[1].Lost || !strings.HasSuffix(rows[1].Body, "second") {
		t.Fatalf("lost-hint message not shown: %+v", rows[1])
	}
	if _, err := Inbox(e.deps, "dlg-000000000000"); err == nil {
		t.Fatal("unknown run read")
	}
}
