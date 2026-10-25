package runstore_test

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func newMailbox(t *testing.T) *runstore.Mailbox {
	t.Helper()
	s, err := runstore.New(newProject(t))
	if err != nil {
		t.Fatal(err)
	}
	mb, err := s.Mailbox("worker-1")
	if err != nil {
		t.Fatal(err)
	}
	return mb
}

func TestMailbox_Layout(t *testing.T) {
	mb := newMailbox(t)
	for _, f := range []runstore.Folder{runstore.Inbox, runstore.Outbox, runstore.AckFolder} {
		fi, err := os.Stat(filepath.Join(mb.Dir(), string(f)))
		if err != nil || !fi.IsDir() || fi.Mode().Perm() != 0o700 {
			t.Errorf("folder %s: %v %v", f, fi, err)
		}
	}
	s, _ := runstore.New(newProject(t))
	for _, bad := range []string{"", "Worker", "../x", "a/b", strings.Repeat("a", 40)} {
		if _, err := s.Mailbox(bad); err == nil {
			t.Errorf("Mailbox(%q) must refuse", bad)
		}
	}
}

// TestMailbox_AtomicDeliver: every message is its own 0600 file with no temp
// left; seq rises; a reader racing the writer never sees a partial message;
// re-delivering an id returns the stored message.
func TestMailbox_AtomicDeliver(t *testing.T) {
	mb := newMailbox(t)
	var stop atomic.Bool
	var wg sync.WaitGroup
	wg.Add(1)
	go raceReader(t, mb, &stop, &wg)
	first := runstore.Message{}
	for i := 0; i < 40; i++ {
		m, err := mb.Post(runstore.Inbox, runstore.Message{From: "caller", To: "worker-1", Body: "task body", Turn: i})
		if err != nil {
			t.Fatal(err)
		}
		if i == 0 {
			first = m
		}
		if m.Seq != int64(i+1) || m.Created == "" || m.Type != runstore.TypeTask {
			t.Fatalf("message %d: %+v", i, m)
		}
	}
	stop.Store(true)
	wg.Wait()
	entries, _ := os.ReadDir(filepath.Join(mb.Dir(), "inbox"))
	if len(entries) != 40 {
		t.Fatalf("want 40 files, got %d", len(entries))
	}
	for _, e := range entries {
		fi, _ := e.Info()
		if !strings.HasSuffix(e.Name(), ".json") || strings.Contains(e.Name(), ".tmp") || fi.Mode().Perm() != 0o600 {
			t.Errorf("unexpected file %s mode %v", e.Name(), fi.Mode())
		}
	}
	again, err := mb.Deliver(runstore.Inbox, runstore.Message{ID: first.ID, Seq: 99, Body: "other"})
	if err != nil || again.Body != "task body" || again.Seq != 1 {
		t.Errorf("re-delivery of %s = %+v, %v; want the stored message", first.ID, again, err)
	}
}

// raceReader reads the inbox until stop, failing on any message that is not
// whole.
func raceReader(t *testing.T, mb *runstore.Mailbox, stop *atomic.Bool, wg *sync.WaitGroup) {
	defer wg.Done()
	for !stop.Load() {
		msgs, err := mb.Read(runstore.Inbox)
		if err != nil {
			t.Errorf("Read: %v", err)
			return
		}
		for _, m := range msgs {
			if m.Body != "task body" || !runstore.ValidMsgID(m.ID) {
				t.Errorf("partial or bad message seen: %+v", m)
				return
			}
		}
	}
}

// TestMailbox_SkipsBadFiles: a half-written message, a temp file, a misnamed
// file and a symlink are skipped; good messages around them are read.
func TestMailbox_SkipsBadFiles(t *testing.T) {
	mb := newMailbox(t)
	good, err := mb.Post(runstore.Outbox, runstore.Message{From: "w", Type: runstore.TypeProgress, Body: "ok"})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(mb.Dir(), "outbox")
	id := runstore.NewMsgID()
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(out, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("000002-"+id+".json", `{"v":1,"id":"`+id+`","seq":2,"bo`)
	write("000003-"+id+".json.tmp.1.1", `{}`)
	write("000004-msg-0123456789abcdef.json", `{"v":1,"id":"msg-fedcba9876543210","seq":4,"body":"x"}`)
	_ = os.Symlink(filepath.Join(out, "000001-"+good.ID+".json"), filepath.Join(out, "000005-"+id+".json"))
	msgs, err := mb.Read(runstore.Outbox)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || msgs[0].ID != good.ID {
		t.Fatalf("Read = %+v; want only the good message", msgs)
	}
	next, _ := mb.Post(runstore.Outbox, runstore.Message{From: "w", Type: runstore.TypeProgress, Body: "two"})
	if next.Seq != 2 {
		t.Errorf("seq after a skipped file = %d, want 2", next.Seq)
	}
}

// TestMailbox_ResultAndAck: the worker's outbox/<task>-result.json is found
// by task and as the newest; a half-written result is not; ack is a file.
func TestMailbox_ResultAndAck(t *testing.T) {
	mb := newMailbox(t)
	task := runstore.NewMsgID()
	if mb.Acked(task) {
		t.Fatal("not acked yet")
	}
	if err := mb.Ack(task); err != nil || !mb.Acked(task) {
		t.Fatalf("Ack: %v", err)
	}
	if _, ok, _ := mb.Result(task); ok {
		t.Fatal("no result yet")
	}
	if err := mb.WriteResult(task, runstore.Message{From: "w", Body: "done"}); err != nil {
		t.Fatal(err)
	}
	r, ok, err := mb.Result(task)
	if err != nil || !ok || r.Body != "done" || r.Type != runstore.TypeResult || r.InReplyTo != task {
		t.Fatalf("Result = %+v %v %v", r, ok, err)
	}
	torn := runstore.NewMsgID()
	_ = os.WriteFile(filepath.Join(mb.Dir(), "outbox", torn+"-result.json"), []byte(`{"body":"ha`), 0o600)
	if _, ok, _ := mb.Result(torn); ok {
		t.Error("a half-written result must not be read")
	}
	newest, ok, _ := mb.NewestResult()
	if !ok || newest.InReplyTo != task {
		t.Errorf("NewestResult = %+v %v", newest, ok)
	}
	if msgs, _ := mb.Read(runstore.Outbox); len(msgs) != 0 {
		t.Errorf("result files are not outbox messages: %+v", msgs)
	}
}
