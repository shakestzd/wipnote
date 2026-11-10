package session

import (
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Ask is one structured question a worker raised, answered or not.
type Ask struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // KindAsk or KindQuestionFile
	Text        string   `json:"text"`
	Options     []string `json:"options,omitempty"`
	Recommended string   `json:"recommended,omitempty"` // an option's label
	Answered    bool     `json:"answered"`
	Answer      string   `json:"answer,omitempty"` // the caller's answer text
}

// RunAsksOut is every ask of one run, plus the work item its ledger row names.
type RunAsksOut struct {
	Run      string
	WorkItem string
	Asks     []Ask
}

// RunAsks lists all of name's asks, open or closed run: the hook-captured
// AskUserQuestion calls (not permission prompts) and every outbox question
// file of the run's tasks. An answer is the caller's inbox note in reply.
func RunAsks(d Deps, name string) (RunAsksOut, error) {
	r, err := findRow(d, name)
	if err != nil {
		return RunAsksOut{}, err
	}
	out := RunAsksOut{Run: r.ID, WorkItem: r.WorkItem}
	mb, err := d.Store.Mailbox(name)
	if err != nil {
		return out, err
	}
	answers := answerTexts(mb)
	if rf, err := d.Store.Run(r.ID); err == nil {
		evs, _ := rf.ReadEvents()
		for _, ev := range evs {
			if ev.Kind == runstore.KindQuestion && ev.ToolUseID != "" {
				out.Asks = append(out.Asks, withAnswer(Ask{ID: ev.ToolUseID, Kind: KindAsk, Text: ev.Summary}, answers))
			}
		}
	}
	msgs, _ := mb.Read(runstore.Inbox)
	for _, m := range msgs {
		if m.Type != runstore.TypeTask || m.Run != r.ID {
			continue
		}
		qs, _ := mb.QFiles(m.ID)
		for _, q := range qs {
			a := Ask{ID: q.ID, Kind: KindQuestionFile, Text: q.Text(), Options: q.Options, Recommended: q.Recommended}
			out.Asks = append(out.Asks, withAnswer(a, answers))
		}
	}
	return out, nil
}

// answerTexts maps a question id to the caller's answer note.
func answerTexts(mb *runstore.Mailbox) map[string]string {
	m := map[string]string{}
	msgs, _ := mb.Read(runstore.Inbox)
	for _, msg := range msgs {
		if msg.InReplyTo != "" {
			m[msg.InReplyTo] = msg.Body
		}
	}
	return m
}

func withAnswer(a Ask, answers map[string]string) Ask {
	a.Answer, a.Answered = answers[a.ID]
	return a
}

// optionIndex finds r among options: an exact label, else a 1-based number.
func optionIndex(options []string, r string) (int, bool) {
	for i, o := range options {
		if o == r {
			return i, true
		}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(r)); err == nil && n >= 1 && n <= len(options) {
		return n - 1, true
	}
	return 0, false
}
