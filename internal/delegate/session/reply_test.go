package session

import (
	"strings"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// A worker answers through delegate reply, not by writing into .wipnote/
// itself (bug-66574a82): the result lands where wait and result read it.
func TestReply_WritesTheNewestTasksResult(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	sent := e.send("w1", "Count the files.")
	out, err := Reply(e.deps, s.Run, ReplyOptions{Body: "42 files"})
	if err != nil || out.Task != sent.Task || out.Kind != ReplyResult || out.ID != sent.Task+"-result" {
		t.Fatalf("%+v err %v", out, err)
	}
	msg, ok, err := e.mailbox("w1").Result(sent.Task)
	if err != nil || !ok || msg.Body != "42 files" || msg.Run != s.Run {
		t.Fatalf("result %+v ok %v err %v", msg, ok, err)
	}
	r, err := Result(e.deps, "w1")
	if err != nil || r.Status != ResultFound || r.Body != "42 files" {
		t.Fatalf("delegate result reads %+v err %v", r, err)
	}
}

func TestReply_AskWritesNumberedQuestions(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	sent := e.send("w1", "Pick a colour.")
	for n, q := range []string{"Which colour?", "Which shade?"} {
		out, err := Reply(e.deps, s.Run, ReplyOptions{Task: sent.Task, Body: q, Ask: true, Options: []string{"red", "blue"}})
		want := sent.Task + "-q" + string(rune('1'+n))
		if err != nil || out.Kind != ReplyQuestion || out.ID != want {
			t.Fatalf("%+v err %v, want id %s", out, err, want)
		}
	}
	qs, err := e.mailbox("w1").QFiles(sent.Task)
	if err != nil || len(qs) != 2 || qs[1].Text() != "Which shade?" || strings.Join(qs[0].Options, ",") != "red,blue" {
		t.Fatalf("%+v err %v", qs, err)
	}
	if _, ok, _ := e.mailbox("w1").Result(sent.Task); ok {
		t.Fatal("a question wrote the result")
	}
	if got := pendingQFiles(e.deps.Store, "w1", s.Run); len(got) != 2 {
		t.Fatalf("pending before the result: %+v", got)
	}
	// The answer typed into the pane records nothing; the result ends the
	// task, so its questions are no longer pending.
	if _, err := Reply(e.deps, s.Run, ReplyOptions{Body: "blue"}); err != nil {
		t.Fatal(err)
	}
	if got := pendingQFiles(e.deps.Store, "w1", s.Run); len(got) != 0 {
		t.Fatalf("questions still pending after the result: %+v", got)
	}
}

func TestReply_Refuses(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	if _, err := Reply(e.deps, s.Run, ReplyOptions{Body: "early"}); err == nil {
		t.Fatal("reply with no task accepted")
	}
	e.send("w1", "Count the files.")
	if _, err := Reply(e.deps, s.Run, ReplyOptions{Body: "  \n"}); err == nil {
		t.Fatal("empty reply accepted")
	}
	if _, err := Reply(e.deps, s.Run, ReplyOptions{Task: runstore.NewMsgID(), Body: "x"}); err == nil {
		t.Fatal("reply to another run's task accepted")
	}
	if _, err := Reply(e.deps, "dlg-000000000000", ReplyOptions{Body: "x"}); err == nil {
		t.Fatal("unknown run accepted")
	}
}

func TestPreamble_NamesDelegateReply(t *testing.T) {
	p := Preamble(Info{Name: "w1", Run: "dlg-0123456789ab", Task: "msg-0123456789ab"}, "/mail/w1")
	for _, want := range []string{"wipnote delegate reply --task msg-0123456789ab \"", "--ask", "/mail/w1/outbox/msg-0123456789ab-result.json"} {
		if !strings.Contains(p, want) {
			t.Errorf("preamble lacks %q:\n%s", want, p)
		}
	}
}
