package runstore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"time"

	"github.com/shakestzd/wipnote/core/filelock"
)

// Folder is one of a mailbox's three folders. Each has one writer: the
// caller writes inbox, the worker writes outbox and ack.
type Folder string

const (
	Inbox     Folder = "inbox"
	Outbox    Folder = "outbox"
	AckFolder Folder = "ack"
)

// Message types (orch's MsgType).
const (
	TypeTask     = "task"
	TypeProgress = "progress"
	TypeQuestion = "question"
	TypeResult   = "result"
	TypeError    = "error"
	TypeBlocked  = "blocked"
	TypePrompt   = "prompt"
	TypeNote     = "note"
)

const (
	msgVersion = 1
	// maxMsgBytes skips a message file too large to be one of ours.
	maxMsgBytes  = 4 << 20
	resultSuffix = "-result"
)

var (
	msgFileRe    = regexp.MustCompile(`^([0-9]{6,})-(msg-[0-9a-f]{12,64})\.json$`)
	resultFileRe = regexp.MustCompile(`^(msg-[0-9a-f]{12,64})-result\.json$`)
)

// Message is one mailbox file, <seq>-<id>.json. The worker's result is
// outbox/<task-id>-result.json, with id <task-id>-result.
type Message struct {
	V         int    `json:"v"`
	ID        string `json:"id"`
	From      string `json:"from"`
	To        string `json:"to"`
	Type      string `json:"type"`
	Seq       int64  `json:"seq"`
	Body      string `json:"body"`
	InReplyTo string `json:"in_reply_to,omitempty"`
	Turn      int    `json:"turn,omitempty"`
	Created   string `json:"created"`
	Hinted    bool   `json:"hinted,omitempty"`
}

// Mailbox is <root>/mail/<name>/.
type Mailbox struct {
	name string
	dir  string
}

// Mailbox returns the mailbox for name, creating its inbox, outbox and ack
// folders (mode 0700).
func (s *Store) Mailbox(name string) (*Mailbox, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("runstore: invalid mailbox name %q", name)
	}
	dir := filepath.Join(s.root, mailDir, name)
	for _, f := range []Folder{Inbox, Outbox, AckFolder} {
		if err := s.privateMkdir(filepath.Join(dir, string(f))); err != nil {
			return nil, err
		}
	}
	return &Mailbox{name: name, dir: dir}, nil
}

// Name is the mailbox name; Dir its path.
func (m *Mailbox) Name() string { return m.name }
func (m *Mailbox) Dir() string  { return m.dir }

// Post delivers msg into folder with the next seq, a new id when it has none,
// and a default type (task in inbox, note in outbox).
func (m *Mailbox) Post(folder Folder, msg Message) (Message, error) {
	if msg.ID == "" {
		msg.ID = NewMsgID()
	}
	if msg.Type == "" {
		msg.Type = TypeNote
		if folder == Inbox {
			msg.Type = TypeTask
		}
	}
	existing, err := m.Read(folder)
	if err != nil {
		return Message{}, err
	}
	msg.Seq = 1
	if n := len(existing); n > 0 {
		msg.Seq = existing[n-1].Seq + 1
	}
	return m.Deliver(folder, msg)
}

// Deliver writes msg as its own file by temp file, fsync and rename. An id
// already in the folder returns the stored message unchanged (idempotent);
// otherwise seq must rise above the folder's highest.
func (m *Mailbox) Deliver(folder Folder, msg Message) (Message, error) {
	if folder != Inbox && folder != Outbox {
		return Message{}, fmt.Errorf("runstore: messages go to inbox or outbox, not %q", folder)
	}
	if !ValidMsgID(msg.ID) || msg.Seq < 1 {
		return Message{}, fmt.Errorf("runstore: invalid message id %q or seq %d", msg.ID, msg.Seq)
	}
	existing, err := m.Read(folder)
	if err != nil {
		return Message{}, err
	}
	for _, e := range existing {
		if e.ID == msg.ID {
			return e, nil
		}
	}
	if n := len(existing); n > 0 && msg.Seq <= existing[n-1].Seq {
		return Message{}, fmt.Errorf("runstore: seq %d does not rise above %d", msg.Seq, existing[n-1].Seq)
	}
	msg.V = msgVersion
	if msg.Created == "" {
		msg.Created = time.Now().UTC().Format(time.RFC3339)
	}
	name := fmt.Sprintf("%06d-%s.json", msg.Seq, msg.ID)
	return msg, m.writeJSON(folder, name, msg)
}

func (m *Mailbox) writeJSON(folder Folder, name string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Errorf("runstore: encode message: %w", err)
	}
	return filelock.WriteFileAtomic(filepath.Join(m.dir, string(folder), name), append(data, '\n'), filePerm)
}

// Read returns folder's messages in seq order. It skips temp files, names
// that are not <seq>-<msg-id>.json, symlinks, files that do not parse and
// files whose content names another id or seq than the file name.
func (m *Mailbox) Read(folder Folder) ([]Message, error) {
	entries, err := os.ReadDir(filepath.Join(m.dir, string(folder)))
	if err != nil {
		return nil, fmt.Errorf("runstore: read %s: %w", folder, err)
	}
	var out []Message
	for _, e := range entries {
		sub := msgFileRe.FindStringSubmatch(e.Name())
		if sub == nil || !e.Type().IsRegular() {
			continue
		}
		msg, ok := m.load(folder, e.Name())
		seq, _ := strconv.ParseInt(sub[1], 10, 64)
		if ok && msg.ID == sub[2] && msg.Seq == seq {
			out = append(out, msg)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

// load parses one plain message file; ok is false for anything else.
func (m *Mailbox) load(folder Folder, name string) (Message, bool) {
	path := filepath.Join(m.dir, string(folder), name)
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() > maxMsgBytes {
		return Message{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Message{}, false
	}
	var msg Message
	if err := json.Unmarshal(data, &msg); err != nil {
		return Message{}, false
	}
	return msg, true
}
