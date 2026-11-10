package session

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Ask is one structured question a worker raised, answered or not.
type Ask struct {
	ID          string   `json:"id"`
	Kind        string   `json:"kind"` // KindAsk or KindQuestionFile
	Text        string   `json:"text"`
	Header      string   `json:"header,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
	Options     []string `json:"options,omitempty"`
	Recommended string   `json:"recommended,omitempty"` // an option's label
	Answered    bool     `json:"answered"`
	// Pane is set when the person answered in the worker's pane: the
	// AskUserQuestion call ended with a result. Answer is then the pane's
	// answer text, or "" when the hook did not capture it.
	Pane   bool   `json:"pane,omitempty"`
	Answer string `json:"answer,omitempty"` // the answer text
}

// RunAsksOut is every ask of one run, plus the work item its ledger row names.
type RunAsksOut struct {
	Run      string
	WorkItem string
	Asks     []Ask
}

// RunAsks lists all of name's asks, open or closed run: the hook-captured
// AskUserQuestion calls (not permission prompts) and every outbox question
// file of the run's tasks. A hook ask whose tool use ended with a result was
// answered in the pane; otherwise an answer is the caller's inbox note in
// reply.
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
		out.Asks = hookAsks(evs, answers)
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

// hookAsks turns the run's AskUserQuestion events into asks, one per
// question of a call. A call with one question keeps the tool use id; one
// with several gets "<tool-use-id>#<n>". An event recorded before the
// structured asks existed falls back to its summary, with no options.
func hookAsks(evs []runstore.Event, answers map[string]string) []Ask {
	ends := map[string]runstore.Event{}
	for _, ev := range evs {
		if ev.Kind == runstore.KindToolEnd && ev.ToolUseID != "" {
			ends[ev.ToolUseID] = ev
		}
	}
	var out []Ask
	for _, ev := range evs {
		if ev.Kind != runstore.KindQuestion || ev.ToolUseID == "" {
			continue
		}
		items := ev.Asks
		if len(items) == 0 {
			items = []runstore.EventAsk{{Question: ev.Summary}}
		}
		end, ended := ends[ev.ToolUseID]
		for i, it := range items {
			a := Ask{ID: ev.ToolUseID, Kind: KindAsk, Text: it.Question, Header: it.Header, MultiSelect: it.MultiSelect, Options: it.Options}
			if len(items) > 1 {
				a.ID = fmt.Sprintf("%s#%d", ev.ToolUseID, i+1)
			}
			if ended && end.Summary != "failed" {
				a.Answered, a.Pane, a.Answer = true, true, paneAnswer(end.Asks, it.Question, i)
			} else if a = withAnswer(a, answers); !a.Answered {
				a.Answer, a.Answered = answers[ev.ToolUseID]
			}
			out = append(out, a)
		}
	}
	return out
}

// paneAnswer is the answer the tool_end recorded for the question: matched
// by text, else by position.
func paneAnswer(ends []runstore.EventAsk, question string, i int) string {
	for _, e := range ends {
		if e.Question == question {
			return e.Answer
		}
	}
	if i < len(ends) {
		return ends[i].Answer
	}
	return ""
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
