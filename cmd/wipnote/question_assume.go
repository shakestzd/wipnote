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
	node, err := findQuestion(qid, func(qs []question.Question, i int) error {
		return qs[i].Assume()
	})
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
	Reopened                     bool
}

// answerQuestion records the owner's answer. An answer that overturns an
// assumption on a done item reopens it through the shared reopen path.
func answerQuestion(qid, key, note string, force bool) (answerOutcome, error) {
	var q question.Question
	node, err := findQuestion(qid, func(qs []question.Question, i int) error {
		if err := qs[i].RecordAnswer(key, note, time.Now(), force); err != nil {
			return err
		}
		q = qs[i]
		return nil
	})
	if err != nil {
		return answerOutcome{}, err
	}
	o := answerOutcome{
		QuestionID: qid, ItemID: node.ID, ItemType: workitem.KindForID(node.ID),
		ItemStatus: node.Status, Status: q.Status, Assumed: q.Assumed, Answer: q.Answer,
	}
	if o.Status == question.StatusOverturned && node.Status == models.StatusDone {
		if err := executeReopen(o.ItemType, node.ID); err != nil {
			return o, fmt.Errorf("answer recorded, but reopening %s failed: %w", node.ID, err)
		}
		o.Reopened, o.ItemStatus = true, models.StatusInProgress
	}
	return o, nil
}

// message says what an overturn means for the item, or "" for a confirm.
func (o answerOutcome) message() string {
	if o.Status != question.StatusOverturned {
		return ""
	}
	head := fmt.Sprintf("Assumption overturned on %s: work assumed %q, the answer is %q.", o.QuestionID, o.Assumed, o.Answer)
	switch {
	case o.Reopened:
		return fmt.Sprintf("%s Reopened %s %s; redo the work that relied on %q.", head, o.ItemType, o.ItemID, o.Assumed)
	case o.ItemStatus == models.StatusInProgress:
		return fmt.Sprintf("%s %s %s is in progress; tell whoever works on it.", head, o.ItemType, o.ItemID)
	}
	return fmt.Sprintf("%s %s %s is %s.", head, o.ItemType, o.ItemID, o.ItemStatus)
}

// assumeQuestionsOnStart assumes the recommended option on the item's open
// non-blocking questions and warns on open blocking ones. It never fails the
// start: errors are printed as warnings.
func assumeQuestionsOnStart(w io.Writer, col *workitem.Collection, id string) {
	node, err := col.Get(id)
	if err != nil {
		fmt.Fprintf(w, "question warning: could not read %s: %v\n", id, err)
		return
	}
	qs := question.FromNode(node)
	r := question.AssumeOpen(qs)
	if r.Changed() {
		if err := saveQuestions(col, id, qs); err != nil {
			fmt.Fprintf(w, "question warning: %v\n", err)
			return
		}
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
