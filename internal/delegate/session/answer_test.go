package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func TestAnswer_RecordsReplyClearsQuestionAndHints(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	qid := e.writeQFile("w1", e.task("w1"), 1, "Proceed?")
	if e.state("w1") != StateQuestion {
		t.Fatalf("state %s", e.state("w1"))
	}
	out, err := Answer(context.Background(), e.deps, "w1", qid, "yes, proceed")
	if err != nil || !out.Hinted || out.Question != qid || out.Run != s.Run {
		t.Fatalf("%+v %v", out, err)
	}
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("still pending: %+v", qs)
	}
	if e.state("w1") == StateQuestion {
		t.Fatal("list still reports question")
	}
	msgs, _ := e.mailbox("w1").Read(runstore.Inbox)
	last := msgs[len(msgs)-1]
	if last.InReplyTo != qid || last.Body != "yes, proceed" || last.Type == runstore.TypeTask {
		t.Fatalf("inbox note %+v", last)
	}
	if res := e.wait("w1", 40*time.Millisecond); res.Outcome == OutcomeAsks {
		t.Fatalf("wait still asks: %+v", res)
	}
	// Other side: answering twice, or an unknown id, writes nothing.
	for _, id := range []string{qid, "msg-0123456789abcdef-q9"} {
		if _, err := Answer(context.Background(), e.deps, "w1", id, "again"); !errors.Is(err, ErrNoQuestion) {
			t.Fatalf("%s: %v", id, err)
		}
	}
	if after, _ := e.mailbox("w1").Read(runstore.Inbox); len(after) != len(msgs) {
		t.Fatal("a refused answer wrote to the inbox")
	}
}

func TestAnswer_LostHintKeepsAnswerInInbox(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	qid := e.writeQFile("w1", e.task("w1"), 1, "Proceed?")
	e.herdr.promptErr = errors.New("herdr: agent busy")
	out, err := Answer(context.Background(), e.deps, "w1", qid, "go")
	if err != nil || out.Hinted || !strings.Contains(out.HintError, "busy") {
		t.Fatalf("%+v %v", out, err)
	}
	if qs := e.questions("w1"); len(qs) != 0 {
		t.Fatalf("pending: %+v", qs)
	}
}
