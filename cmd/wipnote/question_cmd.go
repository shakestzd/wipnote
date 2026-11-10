package main

import (
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
		note  string
		force bool
	)
	cmd := &cobra.Command{
		Use:   "answer <question-id> <key>",
		Short: "Record the owner's answer to a question",
		Args:  cobra.ExactArgs(2),
		RunE: func(_ *cobra.Command, args []string) error {
			o, err := answerQuestion(args[0], args[1], note, force)
			if err != nil {
				return err
			}
			fmt.Printf("Answered %s on %s: %s\n", args[0], o.ItemID, args[1])
			if msg := o.message(); msg != "" {
				fmt.Println(msg)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&note, "note", "", "a note to keep with the answer")
	cmd.Flags().BoolVar(&force, "force", false, "replace an answer recorded earlier")
	return cmd
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
	node, err := col.Get(id)
	if err != nil {
		return nil, err
	}
	if err := saveQuestions(col, id, question.Merge(question.FromNode(node), []question.Question{*q})); err != nil {
		return nil, err
	}
	return q, nil
}

// runQuestionAnswer records the answer (see answerQuestion) and returns the
// item id.
func runQuestionAnswer(qid, key, note string, force bool) (string, error) {
	o, err := answerQuestion(qid, key, note, force)
	return o.ItemID, err
}

// findQuestion runs fn on the item holding qid and its question list, then
// saves the list when fn succeeds. It returns the item node as it was read.
func findQuestion(qid string, fn func(qs []question.Question, i int) error) (*models.Node, error) {
	dir, err := findWipnoteDir()
	if err != nil {
		return nil, err
	}
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	defer p.Close()
	for _, node := range questionItems(p) {
		qs := question.FromNode(node)
		i := question.Index(qs, qid)
		if i < 0 {
			continue
		}
		if err := fn(qs, i); err != nil {
			return nil, err
		}
		col := collectionFor(p, workitem.KindForID(node.ID))
		return node, saveQuestions(col, node.ID, qs)
	}
	return nil, fmt.Errorf("no question %s on any feature, bug or spike", qid)
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

// saveQuestions persists the item's full question list.
func saveQuestions(col *workitem.Collection, id string, qs []question.Question) error {
	if err := col.Edit(id).SetProperty(question.PropQuestions, question.Property(qs)).Save(); err != nil {
		return fmt.Errorf("save questions on %s: %w", id, err)
	}
	return nil
}

// optionKeys renders a question's option keys, marking the recommended one.
func optionKeys(q question.Question) string {
	keys := make([]string, len(q.Options))
	for i, o := range q.Options {
		keys[i] = o.Key
		if o.Key == q.Recommended {
			keys[i] += "*"
		}
	}
	return strings.Join(keys, "/")
}
