package main

import (
	"fmt"
	"io"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// Assumptions and overturns (feat-13e24183). Work goes ahead on an open
// non-blocking question by assuming its recommended option; an answer that
// differs from the assumption overturns it and reopens a done item. Blocking
// questions are never assumed: start warns, execute-preview lists them.

func questionAssumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "assume <question-id>",
		Short: "Record the recommended option as assumed on an open non-blocking question",
		Args:  cobra.ExactArgs(1),
		RunE: func(_ *cobra.Command, args []string) error {
			itemID, err := runQuestionAssume(args[0])
			if err != nil {
				return err
			}
			fmt.Printf("Assumed the recommended option for %s on %s\n", args[0], itemID)
			return nil
		},
	}
}

// runQuestionAssume assumes the recommended option on qid and returns the
// item id. Blocking questions and ones without a recommended option are
// refused.
func runQuestionAssume(qid string) (string, error) {
	node, _, err := findQuestion(qid, func(q *question.Question) error { return q.Assume() })
	if err != nil {
		return "", err
	}
	return node.ID, nil
}

// answerOutcome is what recording an answer did to the question and item.
type answerOutcome struct {
	QuestionID, ItemID, ItemType string
	ItemStatus                   models.NodeStatus
	Status, Assumed, Answer      string
	// AssumedLabel and AnswerLabel are the options' labels, "" for none.
	AssumedLabel, AnswerLabel string
	Reopened                  bool
}

// answerQuestion records the owner's option key. A free-text question is
// refused, naming --text. An answer that overturns an assumption on a done
// item reopens it through the shared reopen path.
func answerQuestion(qid, key, note string, force bool) (answerOutcome, error) {
	return recordOwnerAnswer(qid, func(q *question.Question) error {
		if q.IsFreeText() {
			return fmt.Errorf("%s is a free-text question; answer it with: wipnote question answer %s --text \"<reply>\"", qid, qid)
		}
		return q.RecordAnswer(key, note, time.Now(), force)
	})
}

// answerQuestionText records the owner's reply to a free-text question. An
// option question is refused, naming the key form.
func answerQuestionText(qid, text, note string, force bool) (answerOutcome, error) {
	return recordOwnerAnswer(qid, func(q *question.Question) error {
		if !q.IsFreeText() {
			return fmt.Errorf("%s has options (%s); answer it with: wipnote question answer %s <key>", qid, optionKeys(*q), qid)
		}
		return q.RecordText(text, note, time.Now(), force)
	})
}

func recordOwnerAnswer(qid string, record func(q *question.Question) error) (answerOutcome, error) {
	node, q, err := findQuestion(qid, record)
	if err != nil {
		return answerOutcome{}, err
	}
	return settleItem(node, q)
}

// settleItem reports what an answer did and reopens a done item whose
// assumption it overturned.
func settleItem(node *models.Node, q question.Question) (answerOutcome, error) {
	o := answerOutcome{
		QuestionID: q.ID, ItemID: node.ID, ItemType: workitem.KindForID(node.ID),
		ItemStatus: node.Status, Status: q.Status, Assumed: q.Assumed, Answer: q.Answer,
		AssumedLabel: optionLabel(q, q.Assumed), AnswerLabel: optionLabel(q, q.Answer),
	}
	if o.Status == question.StatusOverturned && node.Status == models.StatusDone {
		if err := executeReopen(o.ItemType, node.ID); err != nil {
			return o, fmt.Errorf("answer recorded, but reopening %s failed: %w", node.ID, err)
		}
		o.Reopened, o.ItemStatus = true, models.StatusInProgress
	}
	return o, nil
}

func optionLabel(q question.Question, key string) string {
	for _, o := range q.Options {
		if o.Key == key {
			return o.Label
		}
	}
	return ""
}

// message is the overturn warning, or "" for a confirm. It always names the
// item, the assumed answer and the new one, and says when to redo the work
// built on the assumption: a done item is reopened, an in-progress one redoes
// it now, any other status when its work resumes.
func (o answerOutcome) message() string {
	if o.Status != question.StatusOverturned {
		return ""
	}
	was, now := quoteAnswer(o.Assumed, o.AssumedLabel), quoteAnswer(o.Answer, o.AnswerLabel)
	head := fmt.Sprintf("Assumption overturned on %s: work on %s %s assumed %s, the answer is %s.", o.QuestionID, o.ItemType, o.ItemID, was, now)
	switch {
	case o.Reopened:
		return fmt.Sprintf("%s Reopened %s; redo the work built on %s to fit %s.", head, o.ItemID, was, now)
	case o.ItemStatus == models.StatusInProgress:
		return fmt.Sprintf("%s %s is in progress; redo the work built on %s to fit %s now, and tell whoever works on it.", head, o.ItemID, was, now)
	}
	return fmt.Sprintf("%s %s is %s; when it resumes, redo the work built on %s to fit %s.", head, o.ItemID, o.ItemStatus, was, now)
}

// quoteAnswer renders `"b" (HTML)`, or `"b"` when the key has no label.
func quoteAnswer(key, label string) string {
	if label == "" || label == key {
		return fmt.Sprintf("%q", key)
	}
	return fmt.Sprintf("%q (%s)", key, label)
}

// assumeQuestionsOnStart assumes the recommended option on the item's open
// non-blocking questions and warns on open blocking ones. It never fails the
// start: errors are printed as warnings.
func assumeQuestionsOnStart(w io.Writer, col *workitem.Collection, id string) {
	var r question.AssumeResult
	if _, err := updateQuestions(col, id, func(cur []question.Question) ([]question.Question, error) {
		r = question.AssumeOpen(cur)
		return r.Assumed, nil
	}); err != nil {
		fmt.Fprintf(w, "question warning: %v\n", err)
		return
	}
	for _, q := range r.Assumed {
		fmt.Fprintf(w, "question %s: assumed %q (recommended) for %q; an answer can still overturn it\n", q.ID, q.Assumed, q.Text)
	}
	for _, q := range r.Unassumed {
		fmt.Fprintf(w, "question %s stays open (no recommended option): %s\n", q.ID, q.Text)
	}
	for _, q := range r.Blocking {
		fmt.Fprintf(w, "warning: blocking question %s is open on %s: %s [%s]\n  answer it first: wipnote question answer %s <key>\n",
			q.ID, id, q.Text, optionKeys(q), q.ID)
	}
}

// warnOpenQuestionsOnComplete lists questions still open or assumed when an
// item completes. It warns only; completion goes ahead.
func warnOpenQuestionsOnComplete(w io.Writer, col *workitem.Collection, id string) {
	node, err := col.Get(id)
	if err != nil {
		return
	}
	for _, q := range question.FromNode(node) {
		if !q.IsOpen() {
			continue
		}
		fmt.Fprintf(w, "warning: question %s is still %s on %s: %s [%s]\n", q.ID, q.Status, id, q.Text, optionKeys(q))
	}
}
