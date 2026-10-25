package runstore

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// Ack records that the worker started task id: ack/<id>, its body a
// timestamp.
func (m *Mailbox) Ack(id string) error {
	if !ValidMsgID(id) {
		return fmt.Errorf("runstore: invalid message id %q", id)
	}
	stamp := []byte(time.Now().UTC().Format(time.RFC3339) + "\n")
	return filelock.WriteFileAtomic(filepath.Join(m.dir, string(AckFolder), id), stamp, filePerm)
}

// Acked reports whether ack/<id> exists as a plain file.
func (m *Mailbox) Acked(id string) bool {
	return ValidMsgID(id) && isPlainFile(filepath.Join(m.dir, string(AckFolder), id))
}

func resultName(task string) string { return task + resultSuffix + ".json" }

// WriteResult writes the worker's result for task to
// outbox/<task-id>-result.json by temp file and rename. The worker normally
// writes this file itself, as the task's preamble tells it (slice 11).
func (m *Mailbox) WriteResult(task string, msg Message) error {
	if !ValidMsgID(task) {
		return fmt.Errorf("runstore: invalid task id %q", task)
	}
	msg.V, msg.ID, msg.Type, msg.InReplyTo = msgVersion, task+resultSuffix, TypeResult, task
	if msg.Created == "" {
		msg.Created = time.Now().UTC().Format(time.RFC3339)
	}
	return m.writeJSON(Outbox, resultName(task), msg)
}

// Result reads the result for task. ok is false when there is none, or the
// file is half-written, a symlink, or names another task.
func (m *Mailbox) Result(task string) (Message, bool, error) {
	if !ValidMsgID(task) {
		return Message{}, false, fmt.Errorf("runstore: invalid task id %q", task)
	}
	msg, ok := m.loadResult(task)
	return msg, ok, nil
}

// loadResult parses a result file, filling the fields a hand-written one may
// leave out from its name.
func (m *Mailbox) loadResult(task string) (Message, bool) {
	msg, ok := m.load(Outbox, resultName(task))
	if !ok || (msg.InReplyTo != "" && msg.InReplyTo != task) {
		return Message{}, false
	}
	msg.InReplyTo = task
	if msg.ID == "" {
		msg.ID = task + resultSuffix
	}
	if msg.Type == "" {
		msg.Type = TypeResult
	}
	return msg, true
}

// NewestResult returns the most recently written readable result in outbox,
// whatever task it answers; ok is false when there is none.
func (m *Mailbox) NewestResult() (Message, bool, error) {
	entries, err := os.ReadDir(filepath.Join(m.dir, string(Outbox)))
	if err != nil {
		return Message{}, false, fmt.Errorf("runstore: read outbox: %w", err)
	}
	type cand struct {
		task string
		mod  time.Time
	}
	var cands []cand
	for _, e := range entries {
		sub := resultFileRe.FindStringSubmatch(e.Name())
		if sub == nil || !e.Type().IsRegular() {
			continue
		}
		if fi, err := e.Info(); err == nil {
			cands = append(cands, cand{sub[1], fi.ModTime()})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		if !cands[i].mod.Equal(cands[j].mod) {
			return cands[i].mod.After(cands[j].mod)
		}
		return cands[i].task > cands[j].task
	})
	for _, c := range cands {
		if msg, ok := m.loadResult(c.task); ok {
			return msg, true, nil
		}
	}
	return Message{}, false, nil
}
