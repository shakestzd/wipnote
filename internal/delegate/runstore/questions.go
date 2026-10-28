package runstore

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
)

// A worker that needs an answer writes outbox/<task-id>-q<n>.json (D7). The
// file is pending until the caller records an answer: an inbox message whose
// in_reply_to is the question id. Slice 17's answer writes that message.
var (
	qFileRe = regexp.MustCompile(`^(msg-[0-9a-f]{12,64})-q([1-9][0-9]{0,5})\.json$`)
	qIDRe   = regexp.MustCompile(`^msg-[0-9a-f]{12,64}-q[1-9][0-9]{0,5}$`)
)

// QFile is one outbox question file. A hand-written one may use body or
// question for its text.
type QFile struct {
	ID       string   `json:"id"`
	Task     string   `json:"task"`
	N        int      `json:"n"`
	Body     string   `json:"body,omitempty"`
	Question string   `json:"question,omitempty"`
	Options  []string `json:"options,omitempty"`
}

// Text is the question's text.
func (q QFile) Text() string {
	if q.Body != "" {
		return q.Body
	}
	return q.Question
}

// ValidQID reports whether id is a question id, <task-id>-q<n>.
func ValidQID(id string) bool { return qIDRe.MatchString(id) }

// QFiles lists the question files of task in n order. A file that is not a
// plain file, too large, or not JSON is skipped.
func (m *Mailbox) QFiles(task string) ([]QFile, error) {
	entries, err := os.ReadDir(filepath.Join(m.dir, string(Outbox)))
	if err != nil {
		return nil, fmt.Errorf("runstore: read outbox: %w", err)
	}
	var out []QFile
	for _, e := range entries {
		sub := qFileRe.FindStringSubmatch(e.Name())
		if sub == nil || sub[1] != task || !e.Type().IsRegular() {
			continue
		}
		var q QFile
		if !m.loadJSON(Outbox, e.Name(), &q) {
			continue
		}
		q.N, _ = strconv.Atoi(sub[2])
		q.Task, q.ID = sub[1], sub[1]+"-q"+sub[2]
		out = append(out, q)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].N < out[j].N })
	return out, nil
}

// Answered reports whether an inbox message replies to question id.
func (m *Mailbox) Answered(id string) (bool, error) {
	msgs, err := m.Read(Inbox)
	if err != nil {
		return false, err
	}
	for _, msg := range msgs {
		if msg.InReplyTo == id {
			return true, nil
		}
	}
	return false, nil
}
