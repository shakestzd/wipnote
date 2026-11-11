package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/shakestzd/wipnote/core/models"
	"github.com/shakestzd/wipnote/core/question"
	"github.com/shakestzd/wipnote/core/workitem"
	"github.com/spf13/cobra"
)

// questionRow is one row of `wipnote question list`. Its JSON shape is read by
// later consumers (the Stop hook and the mods), so field names are stable.
type questionRow struct {
	ItemID      string            `json:"item_id"`
	ItemTitle   string            `json:"item_title"`
	QuestionID  string            `json:"question_id"`
	Text        string            `json:"text"`
	Options     []question.Option `json:"options"`
	Recommended string            `json:"recommended"`
	Blocking    bool              `json:"blocking"`
	Status      string            `json:"status"`
	Assumed     string            `json:"assumed"`
	RaisedBy    string            `json:"raised_by"`
	Age         string            `json:"age"`
	Answer      string            `json:"answer"`
	// FreeText marks a question with no options: answer it with --text.
	FreeText bool   `json:"free_text"`
	Note     string `json:"note,omitempty"`
}

func questionListCmd() *cobra.Command {
	var (
		itemID, format string
		openOnly       bool
	)
	cmd := &cobra.Command{
		Use:   "list [--item <id>] [--open] [--format json]",
		Short: "List questions across features, bugs and spikes",
		Args:  cobra.NoArgs,
		RunE: func(_ *cobra.Command, _ []string) error {
			dir, err := findWipnoteDir()
			if err != nil {
				return err
			}
			rows, err := collectQuestionRows(dir, itemID, openOnly, time.Now())
			if err != nil {
				return err
			}
			return renderQuestionRows(os.Stdout, rows, format)
		},
	}
	cmd.Flags().StringVar(&itemID, "item", "", "only questions on this feature, bug or spike")
	cmd.Flags().BoolVar(&openOnly, "open", false, "only questions still awaiting the owner (open or assumed)")
	cmd.Flags().StringVar(&format, "format", "text", "output format: text or json")
	return cmd
}

// collectQuestionRows gathers question rows from every feature, bug and spike,
// or from one item. An item that no longer resolves yields no rows.
func collectQuestionRows(dir, itemID string, openOnly bool, now time.Time) ([]questionRow, error) {
	p, err := workitem.Open(dir, "claude-code")
	if err != nil {
		return nil, fmt.Errorf("open project: %w", err)
	}
	defer p.Close()
	var nodes []*models.Node
	if itemID == "" {
		nodes = questionItems(p)
	} else if n := resolveQuestionItem(p, dir, itemID); n != nil {
		nodes = []*models.Node{n}
	}
	var rows []questionRow
	for _, n := range nodes {
		for _, q := range question.FromNode(n) {
			if openOnly && !q.IsOpen() {
				continue
			}
			rows = append(rows, newQuestionRow(n, q, now))
		}
	}
	return rows, nil
}

// resolveQuestionItem returns the feature, bug or spike named by id, or nil.
func resolveQuestionItem(p *workitem.Project, dir, id string) *models.Node {
	id, err := workitem.ResolvePartialID(dir, id)
	if err != nil {
		return nil
	}
	typeName := workitem.KindForID(id)
	if !researchTypes[typeName] {
		return nil
	}
	n, err := collectionFor(p, typeName).Get(id)
	if err != nil {
		return nil
	}
	return n
}

// questionItems returns every feature, bug and spike; a collection that fails
// to load is skipped.
func questionItems(p *workitem.Project) []*models.Node {
	var out []*models.Node
	for _, col := range []*workitem.Collection{p.Features.Collection, p.Bugs.Collection, p.Spikes.Collection} {
		nodes, err := col.List()
		if err != nil {
			continue
		}
		out = append(out, nodes...)
	}
	return out
}

func newQuestionRow(n *models.Node, q question.Question, now time.Time) questionRow {
	// Displayed text is stripped of terminal controls (finding 10). Keys are
	// kept as stored: the pane answers with them.
	opts := make([]question.Option, len(q.Options))
	for i, o := range q.Options {
		opts[i] = question.Option{Key: o.Key, Label: stripTerminalControls(o.Label), Tradeoff: stripTerminalControls(o.Tradeoff)}
	}
	return questionRow{
		ItemID: n.ID, ItemTitle: stripTerminalControls(n.Title), QuestionID: q.ID, Text: stripTerminalControls(q.Text), Options: opts,
		Recommended: q.Recommended, Blocking: q.Blocking, Status: q.Status, Assumed: q.Assumed,
		RaisedBy: stripTerminalControls(q.RaisedBy), Age: questionAge(q.RaisedAt, now), Answer: stripTerminalControls(q.Answer),
		FreeText: q.IsFreeText(), Note: stripTerminalControls(q.Note),
	}
}

// questionAge renders the time since raisedAt as "45m", "3h" or "2d".
func questionAge(raisedAt string, now time.Time) string {
	t, err := time.Parse(time.RFC3339, raisedAt)
	if err != nil {
		return ""
	}
	d := now.Sub(t)
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// renderQuestionRows writes rows as JSON (an array, [] when empty) or text.
func renderQuestionRows(w io.Writer, rows []questionRow, format string) error {
	switch format {
	case "json":
		if rows == nil {
			rows = []questionRow{}
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(rows)
	case "", "text":
	default:
		return fmt.Errorf("unknown --format %q (text or json)", format)
	}
	if len(rows) == 0 {
		fmt.Fprintln(w, "No questions.")
		return nil
	}
	for _, r := range rows {
		flags := r.Status
		if r.Blocking {
			flags += ", blocking"
		}
		fmt.Fprintf(w, "%s  %s  [%s]  %s  %s (%s)", r.QuestionID, r.ItemID, flags, r.Age, r.Text,
			optionKeys(question.Question{Options: r.Options, Recommended: r.Recommended}))
		if r.Answer != "" {
			fmt.Fprintf(w, " -> %s", r.Answer)
		}
		fmt.Fprintln(w)
	}
	return nil
}
