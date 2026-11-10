package hooks

import "strings"

// Structured AskUserQuestion data on delegate events (plan-10a07f8d review).
// The question event carries each question's text, header, multiSelect and
// option labels, so `delegate result` can file a record the owner can answer.
// The tool_end of a successful AskUserQuestion carries the answer the person
// picked in the worker's pane, so the record is filed as answered; a failed
// call writes summary "failed" and no answer.

// delegateAsk is one question of an AskUserQuestion call, as stored on an
// event line. Fields are bounded like the summary.
type delegateAsk struct {
	Question    string   `json:"question"`
	Header      string   `json:"header,omitempty"`
	MultiSelect bool     `json:"multi_select,omitempty"`
	Options     []string `json:"options,omitempty"`
	Answer      string   `json:"answer,omitempty"`
}

// Bounds on the structured ask data, so one call cannot bloat an events file.
const (
	delegateAskMax       = 4   // questions per call
	delegateAskOptionMax = 8   // options per question
	delegateAskLabelMax  = 200 // runes per header, label or answer item
)

// askItems reads AskUserQuestion's tool_input questions.
func askItems(input map[string]any) []delegateAsk {
	qs, _ := input["questions"].([]any)
	var out []delegateAsk
	for _, q := range qs {
		if len(out) == delegateAskMax {
			break
		}
		m, _ := q.(map[string]any)
		text, _ := m["question"].(string)
		header, _ := m["header"].(string)
		multi, _ := m["multiSelect"].(bool)
		a := delegateAsk{Question: boundSummary(text), Header: boundRunes(header, delegateAskLabelMax), MultiSelect: multi}
		opts, _ := m["options"].([]any)
		for _, o := range opts {
			if l := optionLabel(o); l != "" && len(a.Options) < delegateAskOptionMax {
				a.Options = append(a.Options, boundRunes(l, delegateAskLabelMax))
			}
		}
		out = append(out, a)
	}
	return out
}

func optionLabel(o any) string {
	switch v := o.(type) {
	case map[string]any:
		l, _ := v["label"].(string)
		return strings.TrimSpace(l)
	case string:
		return strings.TrimSpace(v)
	}
	return ""
}

// askAnswers returns the asks of a finished AskUserQuestion call with the
// person's answers, read from the tool result's "answers" map (question text
// to answer) or, failing that, the tool input's. A multi-select answer list
// is joined with ", ".
func askAnswers(event *CloudEvent) []delegateAsk {
	asks := askItems(event.ToolInput)
	answers, _ := event.ToolResult["answers"].(map[string]any)
	if answers == nil {
		answers, _ = event.ToolInput["answers"].(map[string]any)
	}
	for i := range asks {
		asks[i].Answer = answerText(answers[asks[i].Question])
	}
	return asks
}

func answerText(v any) string {
	switch a := v.(type) {
	case string:
		return boundSummary(strings.TrimSpace(a))
	case []any:
		var parts []string
		for _, p := range a {
			if s, ok := p.(string); ok && strings.TrimSpace(s) != "" {
				parts = append(parts, strings.TrimSpace(s))
			}
		}
		return boundSummary(strings.Join(parts, ", "))
	}
	return ""
}

func boundRunes(s string, max int) string {
	if r := []rune(s); len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}
