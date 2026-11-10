package question

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Free-text questions and worker-raised records (plan-10a07f8d review). A
// question with fewer than MinOptions options is free text: the owner answers
// it with words, not a key. `question add` still demands 2-4 options; free
// text comes from worker asks that carried no usable options.

// IsFreeText reports whether the question takes a text answer: it has fewer
// than MinOptions options.
func (q *Question) IsFreeText() bool { return len(q.Options) < MinOptions }

// NoTextPlaceholder is the text of a worker ask that carried none.
const NoTextPlaceholder = "(no question text)"

// FromLabels builds an open record from a worker's ask, whose options are
// bare labels: keys are the 1-based positions and recommended is a label.
// Blank and repeated labels are dropped (recommended matches the first copy).
// Zero or one label left makes a free-text question, the lone label kept in
// the note; more than MaxOptions keeps the first MaxOptions and lists the
// rest in the note.
func FromLabels(text string, labels []string, recommended, raisedBy, sourceID string, now time.Time) Question {
	q := Question{
		ID: NewID(), Text: strings.TrimSpace(text), Status: StatusOpen, RaisedBy: raisedBy, SourceID: sourceID,
		RaisedAt: now.UTC().Format(time.RFC3339),
	}
	if q.Text == "" {
		q.Text = NoTextPlaceholder
	}
	uniq := uniqueLabels(labels)
	switch {
	case len(uniq) < MinOptions:
		if len(uniq) == 1 {
			q.Note = "the worker offered one option: " + uniq[0]
		}
		return q
	case len(uniq) > MaxOptions:
		q.Note = "more options the worker offered: " + strings.Join(uniq[MaxOptions:], ", ")
		uniq = uniq[:MaxOptions]
	}
	for i, l := range uniq {
		o := Option{Key: strconv.Itoa(i + 1), Label: l}
		q.Options = append(q.Options, o)
		if l == strings.TrimSpace(recommended) && q.Recommended == "" {
			q.Recommended = o.Key
		}
	}
	return q
}

func uniqueLabels(labels []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, l := range labels {
		l = strings.TrimSpace(l)
		if l == "" || seen[l] {
			continue
		}
		seen[l] = true
		out = append(out, l)
	}
	return out
}

// MatchOption finds the option a reply names: its key, its 1-based number,
// or its label (case-insensitive), in that order.
func (q *Question) MatchOption(reply string) (string, bool) {
	r := strings.TrimSpace(reply)
	if r == "" {
		return "", false
	}
	for _, o := range q.Options {
		if o.Key == r {
			return o.Key, true
		}
	}
	if n, err := strconv.Atoi(r); err == nil && n >= 1 && n <= len(q.Options) {
		return q.Options[n-1].Key, true
	}
	for _, o := range q.Options {
		if strings.EqualFold(o.Label, r) {
			return o.Key, true
		}
	}
	return "", false
}

// RecordText stores the owner's text answer to a free-text question and
// confirms it. An option question, empty text, or a question already answered
// without force is refused.
func (q *Question) RecordText(text, note string, now time.Time, force bool) error {
	t := strings.TrimSpace(text)
	switch {
	case !q.IsFreeText():
		return fmt.Errorf("%s has options (%s); answer it with an option key", q.ID, q.keyList())
	case t == "":
		return fmt.Errorf("the answer to %s is empty", q.ID)
	case q.Answer != "" && !force:
		return fmt.Errorf("%s is already answered (%s); pass --force to change it", q.ID, q.Answer)
	}
	q.Answer, q.Note = t, strings.TrimSpace(note)
	q.settle()
	q.AnsweredAt = now.UTC().Format(time.RFC3339)
	return nil
}

// RecordOther stores an answer the owner gave outside the options, as the
// harness question tool allows ("Other", or several picks of a
// multi-select). It settles like RecordAnswer: overturned when it differs
// from an assumed option.
func (q *Question) RecordOther(text string, now time.Time) error {
	t := strings.TrimSpace(text)
	if t == "" {
		return fmt.Errorf("the answer to %s is empty", q.ID)
	}
	q.Answer = t
	q.settle()
	q.AnsweredAt = now.UTC().Format(time.RFC3339)
	return nil
}
