// Package question holds the owner-question record stored on a work item: a
// non-blocking question with 2-4 options, an optional recommended option and,
// once settled, the answer. It mirrors the research record (core/research):
// the record lives in the item's node properties, read with FromNode and
// written with Property. Plan YAML questions (plan/planyaml.PlanQuestion) are
// the model for the field set, not the store.
package question

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/shakestzd/wipnote/core/models"
)

// PropQuestions is the models.Node.Properties key holding the item's
// questions, as a JSON list (the data-node-props escape hatch).
const PropQuestions = "questions"

// Question statuses. Add sets open; Assume (question assume, item start) sets
// assumed; an answer sets confirmed, or overturned when it differs from the
// assumed option.
const (
	StatusOpen       = "open"
	StatusAssumed    = "assumed"
	StatusConfirmed  = "confirmed"
	StatusOverturned = "overturned"
)

// Option bounds.
const (
	MinOptions = 2
	MaxOptions = 4
)

// Option is one selectable answer.
type Option struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Tradeoff string `json:"tradeoff,omitempty"`
}

// Question is one owner question on a work item.
type Question struct {
	ID          string   `json:"id"`
	Text        string   `json:"text"`
	Options     []Option `json:"options"`
	Recommended string   `json:"recommended,omitempty"`
	Blocking    bool     `json:"blocking,omitempty"`
	Status      string   `json:"status"`
	RaisedBy    string   `json:"raised_by,omitempty"`
	RaisedAt    string   `json:"raised_at,omitempty"`
	SourceID    string   `json:"source_id,omitempty"`
	Assumed     string   `json:"assumed,omitempty"`
	Answer      string   `json:"answer,omitempty"`
	Note        string   `json:"note,omitempty"`
	AnsweredAt  string   `json:"answered_at,omitempty"`
}

// ParseOption reads "key:label[:tradeoff]". The tradeoff keeps any further
// colons.
func ParseOption(s string) (Option, error) {
	parts := strings.SplitN(s, ":", 3)
	if len(parts) < 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return Option{}, fmt.Errorf("option %q must be \"key:label[:tradeoff]\"", s)
	}
	o := Option{Key: strings.TrimSpace(parts[0]), Label: strings.TrimSpace(parts[1])}
	if len(parts) == 3 {
		o.Tradeoff = strings.TrimSpace(parts[2])
	}
	return o, nil
}

// New validates and returns an open question with a fresh id.
func New(text string, opts []Option, recommended string, blocking bool, raisedBy, sourceID string, now time.Time) (*Question, error) {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, fmt.Errorf("question text is empty")
	}
	if len(opts) < MinOptions || len(opts) > MaxOptions {
		return nil, fmt.Errorf("a question needs %d to %d options, got %d", MinOptions, MaxOptions, len(opts))
	}
	seen := map[string]bool{}
	for _, o := range opts {
		if seen[o.Key] {
			return nil, fmt.Errorf("option key %q is used twice", o.Key)
		}
		seen[o.Key] = true
	}
	q := &Question{
		ID: NewID(), Text: text, Options: opts, Recommended: strings.TrimSpace(recommended),
		Blocking: blocking, Status: StatusOpen, RaisedBy: raisedBy, SourceID: sourceID,
		RaisedAt: now.UTC().Format(time.RFC3339),
	}
	if q.Recommended != "" && !q.HasKey(q.Recommended) {
		return nil, fmt.Errorf("recommended key %q is not one of %s", q.Recommended, q.keyList())
	}
	return q, nil
}

// NewID returns a fresh question id.
func NewID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "q-" + hex.EncodeToString(b)
}

// HasKey reports whether key names one of the options.
func (q *Question) HasKey(key string) bool {
	for _, o := range q.Options {
		if o.Key == key {
			return true
		}
	}
	return false
}

func (q *Question) keyList() string {
	keys := make([]string, len(q.Options))
	for i, o := range q.Options {
		keys[i] = o.Key
	}
	return strings.Join(keys, ", ")
}

// IsOpen reports whether the question still awaits the owner: open or assumed.
func (q *Question) IsOpen() bool {
	return q.Status == StatusOpen || q.Status == StatusAssumed
}

// RecordAnswer stores the owner's answer. An unknown key is refused; a
// question that already has an answer needs force. The status becomes
// confirmed, or overturned when an assumed option exists and the answer
// differs; a forced re-answer runs the same check again. A free-text question
// is refused: it takes RecordText.
func (q *Question) RecordAnswer(key, note string, now time.Time, force bool) error {
	if q.IsFreeText() {
		return fmt.Errorf("%s is a free-text question; record a text answer, not a key", q.ID)
	}
	if !q.HasKey(key) {
		return fmt.Errorf("answer %q is not one of %s", key, q.keyList())
	}
	if q.Answer != "" && !force {
		return fmt.Errorf("%s is already answered (%s); pass --force to change it", q.ID, q.Answer)
	}
	q.Answer, q.Note = key, strings.TrimSpace(note)
	q.settle()
	q.AnsweredAt = now.UTC().Format(time.RFC3339)
	return nil
}

// FromNode returns the node's questions, or nil when it has none.
func FromNode(n *models.Node) []Question {
	if n == nil {
		return nil
	}
	raw, ok := n.Properties[PropQuestions]
	if !ok || raw == nil {
		return nil
	}
	var data []byte
	if s, isStr := raw.(string); isStr {
		data = []byte(s)
	} else {
		data, _ = json.Marshal(raw)
	}
	var qs []Question
	if err := json.Unmarshal(data, &qs); err != nil {
		return nil
	}
	return qs
}

// Property returns the questions in the shape stored on Node.Properties.
func Property(qs []Question) []any {
	data, _ := json.Marshal(qs)
	var out []any
	_ = json.Unmarshal(data, &out)
	return out
}

// Merge folds src into dst by id: a question in src replaces the one with the
// same id, new ones are appended in order.
func Merge(dst, src []Question) []Question {
	out := append([]Question(nil), dst...)
	for _, q := range src {
		if i := Index(out, q.ID); i >= 0 {
			out[i] = q
			continue
		}
		out = append(out, q)
	}
	return out
}

// Index returns the position of the question with id, or -1.
func Index(qs []Question, id string) int {
	for i := range qs {
		if qs[i].ID == id {
			return i
		}
	}
	return -1
}
