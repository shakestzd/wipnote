package main

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// Owner questions (plan-10a07f8d, GitHub #336): non-blocking questions stored
// on features, bugs and spikes. The record is core/question; this file wires
// add and answer, question_list.go wires list, question_assume.go wires assume
// and the start, complete and overturn hooks.

// questionCmd is the top-level `wipnote question` command.
func questionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "question",
		Short: "Raise, list and answer owner questions stored on features, bugs and spikes",
		Long: "An owner question is a record on a work item with 2-4 options and an optional " +
			"recommended option. It does not stop work; it waits for the owner to answer.",
	}
	cmd.AddCommand(questionAddCmd(), questionListCmd(), questionAnswerCmd(), questionAssumeCmd())
	return cmd
}

func questionAddCmd() *cobra.Command {
	var (
		opts            []string
		recommended, by string
		source          string
		blocking        bool
	)
	cmd := &cobra.Command{
		Use:   `add <item-id> "<text>" --option k:label[:tradeoff] ...`,
		Short: "Store an open question on a feature, bug or spike and print its id",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			q, err := runQuestionAdd(args[0], args[1], opts, recommended, blocking, by, source)
			if err != nil {
				return err
			}
			fmt.Println(q.ID)
			return nil
		},
	}
	f := cmd.Flags()
	f.StringArrayVar(&opts, "option", nil, "an option as key:label[:tradeoff]; give 2 to 4")
	f.StringVar(&recommended, "recommended", "", "key of the recommended option")
	f.BoolVar(&blocking, "blocking", false, "mark the question as blocking the item's next step")
	f.StringVar(&by, "by", "agent", "who raised the question")
	f.StringVar(&source, "source", "", "id of what raised it (session, message or plan)")
	return cmd
}

func questionAnswerCmd() *cobra.Command {
	var (
		note, text string
		force      bool
	)
	cmd := &cobra.Command{
		Use:   `answer <question-id> <key> | answer <question-id> --text "<reply>"`,
		Short: "Record the owner's answer to a question: an option key, or --text for a free-text question",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			o, err := answerFromArgs(args, text, cmd.Flags().Changed("text"), note, force)
			if err != nil {
				return err
			}
			fmt.Println(answeredLine(o))
			if msg := o.message(); msg != "" {
				fmt.Println(msg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&text, "text", "", "the reply to a free-text question (one with no options)")
	cmd.Flags().StringVar(&note, "note", "", "a note to keep with the answer")
	cmd.Flags().BoolVar(&force, "force", false, "replace an answer recorded earlier")
	return cmd
}

// answerFromArgs picks the answer form: a key, or --text, never both.
func answerFromArgs(args []string, text string, hasText bool, note string, force bool) (answerOutcome, error) {
	switch {
	case hasText && len(args) == 2:
		return answerOutcome{}, fmt.Errorf("give an option key or --text, not both")
	case hasText:
		return answerQuestionText(args[0], text, note, force)
	case len(args) == 1:
		return answerOutcome{}, fmt.Errorf("give an option key: wipnote question answer %s <key> (or --text \"<reply>\" for a free-text question)", args[0])
	}
	return answerQuestion(args[0], args[1], note, force)
}

// runQuestionAdd validates the question and appends it to the item's record.
func runQuestionAdd(itemID, text string, rawOpts []string, recommended string, blocking bool, by, source string) (*question.Question, error) {
	opts := make([]question.Option, 0, len(rawOpts))
	for _, raw := range rawOpts {
		o, err := question.ParseOption(raw)
		if err != nil {
			return nil, err
		}
		opts = append(opts, o)
	}
	q, err := question.New(text, opts, recommended, blocking, by, source, time.Now())
	if err != nil {
		return nil, err
	}
	p, col, id, err := openQuestionItem(itemID)
	if err != nil {
		return nil, err
	}
	defer p.Close()
	if _, err := updateQuestions(col, id, func([]question.Question) ([]question.Question, error) {
		return []question.Question{*q}, nil
	}); err != nil {
		return nil, err
	}
	return q, nil
}

// questionSaveHook, when set, runs after findQuestion has located the item
// and before it saves: a test seam for a concurrent writer.
// questionAfterSaveHook runs right after the save: a seam for a writer that
// lands after it.
var questionSaveHook, questionAfterSaveHook func()

// findQuestion runs fn on question qid as currently stored and saves it. The
// item is located on a snapshot; fn then runs inside the item's write lock on
// the questions as they are now, and only qid is merged back, so a concurrent
// add or answer on another question is kept. An overturned answer on a done
// item reopens it in that same write (saveQuestions). It returns what the
// write did and the question after fn.
func findQuestion(qid string, fn func(q *question.Question) error) (questionWrite, question.Question, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return questionWrite{}, question.Question{}, err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return questionWrite{}, question.Question{}, fmt.Errorf("open project: %w", err)
	}
	defer p.Close()
	node, qid, err := locateQuestion(p, qid)
	if err != nil {
		return questionWrite{}, question.Question{}, err
	}
	if questionSaveHook != nil {
		questionSaveHook()
	}
	var got question.Question
	w, err := saveQuestions(collectionFor(p, workitem.KindForID(node.ID)), node.ID, func(_ *models.Node, cur []question.Question) ([]question.Question, error) {
		i := question.Index(cur, qid)
		if i < 0 {
			return nil, fmt.Errorf("question %s is no longer on %s", qid, node.ID)
		}
		got = cur[i]
		if err := fn(&got); err != nil {
			return nil, err
		}
		return []question.Question{got}, nil
	})
	if err != nil {
		return w, got, err
	}
	if questionAfterSaveHook != nil {
		questionAfterSaveHook()
	}
	if w.reopened {
		persistReopen(dir, workitem.KindForID(node.ID), node.ID)
	}
	return w, got, nil
}

// openQuestionItem resolves id and returns the project and the collection
// holding it. Only features, bugs and spikes carry questions.
func openQuestionItem(id string) (*workitem.Project, *workitem.Collection, string, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return nil, nil, "", err
	}
	if id, err = workitem.ResolvePartialID(dir, id); err != nil {
		return nil, nil, "", err
	}
	typeName := workitem.KindForID(id)
	if !researchTypes[typeName] {
		return nil, nil, "", fmt.Errorf("%s is a %s; questions live on features, bugs and spikes", id, typeName)
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return nil, nil, "", fmt.Errorf("open project: %w", err)
	}
	return p, collectionFor(p, typeName), id, nil
}

// refusal carries fn's own error out of updateQuestions unwrapped, apart
// from a store failure.
type refusal struct{ err error }

func (r refusal) Error() string { return r.err.Error() }

// errNoQuestionChange tells saveQuestions that fn changed nothing.
var errNoQuestionChange = errors.New("no question changed")

// questionWrite is what one locked questions write did.
type questionWrite struct {
	node *models.Node // the item as it now stands
	// reopened: an overturned answer reopened the done item in that write.
	reopened bool
}

// updateQuestions is saveQuestions for a fn that needs only the questions.
func updateQuestions(col *workitem.Collection, id string, fn func(cur []question.Question) ([]question.Question, error)) (*models.Node, error) {
	w, err := saveQuestions(col, id, func(_ *models.Node, cur []question.Question) ([]question.Question, error) {
		return fn(cur)
	})
	return w.node, err
}

// saveQuestions runs fn on the item and its questions as stored, inside the
// item's write lock, and saves the questions fn returns merged by id
// (question.Merge) into that current list. When a saved question is
// overturned and the item is done, the item is reopened in the same write,
// its status checked inside the lock, so a concurrent start or complete is
// never undone. Nothing is written when fn returns none, and a questions
// property that cannot be read is refused, never overwritten.
func saveQuestions(col *workitem.Collection, id string, fn func(n *models.Node, cur []question.Question) ([]question.Question, error)) (questionWrite, error) {
	var w questionWrite
	node, err := col.Update(id, func(n *models.Node) error {
		cur, err := question.Parse(n)
		if err != nil {
			return refusal{fmt.Errorf("%w; nothing was written: fix or remove the property in %s by hand", err, id)}
		}
		changed, err := fn(n, cur)
		if err != nil {
			return refusal{err}
		}
		if len(changed) == 0 {
			return errNoQuestionChange
		}
		n.Properties[question.PropQuestions] = question.Property(question.Merge(cur, changed))
		w.reopened = anyOverturned(changed) && col.Reopen(n)
		return nil
	})
	var r refusal
	switch {
	case errors.Is(err, errNoQuestionChange):
		node, err = col.Get(id)
		return questionWrite{node: node}, err
	case errors.As(err, &r):
		return questionWrite{}, r.err
	case err != nil:
		return questionWrite{}, fmt.Errorf("save questions on %s: %w", id, err)
	}
	w.node = node
	return w, nil
}

func anyOverturned(qs []question.Question) bool {
	for _, q := range qs {
		if q.Status == question.StatusOverturned {
			return true
		}
	}
	return false
}

// answeredLine is the line `question answer` prints first. It names the
// full question id, not the prefix the owner typed.
func answeredLine(o answerOutcome) string {
	return fmt.Sprintf("Answered %s on %s: %s", o.QuestionID, o.ItemID, stripTerminalControls(o.Answer))
}

// optionKeys renders a question's option keys, marking the recommended one,
// or "free text" for a question with no options. It is for display: terminal
// controls in a key stored before keys were restricted are stripped.
func optionKeys(q question.Question) string {
	if q.IsFreeText() {
		return "free text"
	}
	keys := make([]string, len(q.Options))
	for i, o := range q.Options {
		keys[i] = o.Key
		if o.Key == q.Recommended {
			keys[i] += "*"
		}
	}
	return stripTerminalControls(strings.Join(keys, "/"))
}
