package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// filedAsks is what filing a run's worker asks did: the new question ids,
// and a warning when there was nowhere to file them.
type filedAsks struct {
	Filed   []string
	Warning string
}

// fileWorkerAsks files each ask of a run as a question record on the
// worker's work item, once: an ask whose id is already a source_id on the
// item is skipped. Permission prompts never reach here (RunAsks drops them).
func fileWorkerAsks(name string, run session.RunAsksOut) (filedAsks, error) {
	var out filedAsks
	if len(run.Asks) == 0 {
		return out, nil
	}
	if run.WorkItem == "" {
		texts := make([]string, len(run.Asks))
		for i, a := range run.Asks {
			texts[i] = strconv.Quote(a.Text)
		}
		out.Warning = fmt.Sprintf("worker %s has no work item, so %d question(s) were not filed: %s",
			name, len(run.Asks), strings.Join(texts, "; "))
		return out, nil
	}
	p, col, id, err := openQuestionItem(run.WorkItem)
	if err != nil {
		return out, err
	}
	defer p.Close()
	node, err := col.Get(id)
	if err != nil {
		return out, err
	}
	existing := question.FromNode(node)
	var fresh []question.Question
	for _, a := range run.Asks {
		if hasSource(existing, a.ID) {
			continue
		}
		q := askRecord(name, a, time.Now())
		fresh = append(fresh, q)
		out.Filed = append(out.Filed, q.ID)
	}
	if len(fresh) == 0 {
		return out, nil
	}
	return out, saveQuestions(col, id, question.Merge(existing, fresh))
}

func hasSource(qs []question.Question, source string) bool {
	for _, q := range qs {
		if q.SourceID == source {
			return true
		}
	}
	return false
}

// askRecord turns a worker ask into a question record. Worker options are
// bare strings: key is the 1-based index, no tradeoff. A hook ask may carry
// fewer than the two options New demands, so the record keeps what the
// worker gave.
func askRecord(name string, a session.Ask, now time.Time) question.Question {
	opts := make([]question.Option, len(a.Options))
	rec := ""
	for i, o := range a.Options {
		opts[i] = question.Option{Key: strconv.Itoa(i + 1), Label: o}
		if o == a.Recommended {
			rec = opts[i].Key
		}
	}
	pad := []question.Option{{Key: "1", Label: "-"}, {Key: "2", Label: "-"}}
	text := a.Text
	if strings.TrimSpace(text) == "" {
		text = "(no question text)"
	}
	q, _ := question.New(text, pad, "", false, "worker:"+name, a.ID, now) // text is non-empty, pad is valid
	q.Options, q.Recommended = opts, rec
	if a.Answered {
		setAnswer(q, a.Answer, now)
	}
	return *q
}

// setAnswer records the caller's answer: through RecordAnswer when it names
// an option, else as free text kept in the note.
func setAnswer(q *question.Question, text string, now time.Time) {
	t := strings.TrimSpace(text)
	for _, o := range q.Options {
		if t == o.Key || strings.EqualFold(t, o.Label) {
			_ = q.RecordAnswer(o.Key, "", now, false) // key is one of the options
			return
		}
	}
	q.Answer, q.Note, q.Status = t, t, question.StatusConfirmed
	q.AnsweredAt = now.UTC().Format(time.RFC3339)
}
