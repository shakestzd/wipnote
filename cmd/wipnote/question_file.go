package main

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/shakestzd/wipnote/internal/delegate/session"
)

// filedAsks is what filing a run's worker asks did: the new question ids,
// the ids of records filed earlier that now got their answer, and warnings
// (no work item to file on, a reply that names no option, an overturn).
type filedAsks struct {
	Filed    []string
	Answered []string
	Warning  string
}

// paneAnswerPlaceholder is the answer of an ask the person answered in the
// worker's pane when the hook did not capture the text.
const paneAnswerPlaceholder = "(answered in the worker's pane)"

// fileWorkerAsks files each ask of a run as a question record on the
// worker's work item, once per ask (matched by source_id). On a later run, a
// filed record still open or assumed whose ask now has an answer gets that
// answer through the same settle path as `question answer`. Permission
// prompts never reach here (RunAsks drops them).
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
	var settled []question.Question
	var warns []string
	w, err := saveQuestions(col, id, func(_ *models.Node, cur []question.Question) ([]question.Question, error) {
		changed, filed, answered, ws := applyWorkerAsks(name, cur, run.Asks, time.Now())
		out.Filed, out.Answered, settled, warns = filed, answered, lateAnswered(changed, answered), ws
		return changed, nil
	})
	if err != nil {
		return out, err
	}
	if w.reopened {
		persistReopen(p.ProjectDir, workitem.KindForID(id), id)
	}
	warns = append(warns, settleWorkerAnswers(w, settled)...)
	out.Warning = strings.Join(warns, "; ")
	return out, nil
}

// applyWorkerAsks works out the records a run's asks add or answer, against
// the item's current questions. It returns the changed records, the new ids,
// the ids answered late, and warnings.
func applyWorkerAsks(name string, cur []question.Question, asks []session.Ask, now time.Time) (changed []question.Question, filed, answered, warns []string) {
	for _, a := range asks {
		i := sourceIndex(cur, a.ID)
		if i < 0 {
			q := askRecord(name, a, now)
			if w := applyAskAnswer(&q, a, now); w != "" {
				warns = append(warns, w)
			}
			changed, filed = append(changed, q), append(filed, q.ID)
			continue
		}
		q := cur[i]
		if !q.IsOpen() || !a.Answered {
			continue
		}
		before := q
		w := applyAskAnswer(&q, a, now)
		if q.Status != before.Status {
			answered = append(answered, q.ID)
		}
		if q.Status != before.Status || q.Answer != before.Answer || q.Note != before.Note {
			changed = append(changed, q)
			if w != "" {
				warns = append(warns, w)
			}
		}
	}
	return changed, filed, answered, warns
}

func sourceIndex(qs []question.Question, source string) int {
	for i := range qs {
		if qs[i].SourceID == source {
			return i
		}
	}
	return -1
}

// lateAnswered picks the changed records whose ids were answered late.
func lateAnswered(changed []question.Question, ids []string) []question.Question {
	var out []question.Question
	for _, id := range ids {
		if i := question.Index(changed, id); i >= 0 {
			out = append(out, changed[i])
		}
	}
	return out
}

// settleWorkerAnswers reports overturns among late answers. The write that
// recorded them already reopened a done item (saveQuestions); the first
// overturn says so, the others name the item as in progress.
func settleWorkerAnswers(w questionWrite, qs []question.Question) []string {
	var warns []string
	reopened := w.reopened
	for _, q := range qs {
		o := outcomeOf(w.node, q, reopened && q.Status == question.StatusOverturned)
		if o.Reopened {
			reopened = false
		}
		if msg := o.message(); msg != "" {
			warns = append(warns, msg)
		}
	}
	return warns
}

// askRecord turns a worker ask into an open question record built directly
// (question.FromLabels): no options or one makes a free-text question, two
// to four an option question, more keeps the first four and notes the rest;
// repeated labels are dropped.
func askRecord(name string, a session.Ask, now time.Time) question.Question {
	return question.FromLabels(a.Text, a.Options, a.Recommended, "worker:"+name, a.ID, now)
}

// applyAskAnswer records an ask's answer on q, if it has one, and returns a
// warning when the answer cannot settle the question.
//
//   - A free-text question takes the text as its answer.
//   - A reply naming an option (key, 1-based number or label, any case) goes
//     through RecordAnswer, so assume and overturn rules apply.
//   - The person's own words in the worker's pane are their answer
//     (RecordOther); with no captured text the record is still confirmed.
//   - Any other caller reply leaves the question open for the owner, the
//     reply kept in the note.
func applyAskAnswer(q *question.Question, a session.Ask, now time.Time) string {
	if !a.Answered {
		return ""
	}
	text := strings.TrimSpace(a.Answer)
	if text == "" && a.Pane {
		text = paneAnswerPlaceholder
	}
	if q.IsFreeText() {
		if text != "" {
			_ = q.RecordText(text, q.Note, now, false) // free text, non-empty, unanswered
		}
		return ""
	}
	if key, ok := q.MatchOption(text); ok {
		_ = q.RecordAnswer(key, q.Note, now, false) // key is one of the options, unanswered
		return ""
	}
	if a.Pane {
		_ = q.RecordOther(text, now) // text is non-empty
		return ""
	}
	note := "the worker's caller replied: " + text
	if strings.Contains(q.Note, note) {
		return ""
	}
	if q.Note != "" {
		note = q.Note + "; " + note
	}
	q.Note = note
	return fmt.Sprintf("question %s: the reply %q names none of its options (%s), so it stays open; answer it with: wipnote question answer %s <key>",
		q.ID, text, optionKeys(*q), q.ID)
}
