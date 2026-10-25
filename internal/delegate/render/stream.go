package render

import (
	"encoding/json"
	"math"
	"strings"
)

// StreamFilter renders a stream-json log a line at a time: text for
// clean.log and Markdown for clean.md. A call waits for its result and both
// are written together; until then nothing is written for it, so the
// Markdown for the first N lines is a prefix of the Markdown for more lines
// and a follower only ever appends.
type StreamFilter struct {
	passText bool // a line that is not JSON goes to clean.log as it is
	pend     []clItem
}

// NewClaudeFilter is the filter for a headless claude in a pane: stderr and
// text output pass into clean.log (stream.jq's pane mode).
func NewClaudeFilter() *StreamFilter { return &StreamFilter{passText: true} }

// NewCcxFilter is the filter for ccx's run log, which holds only JSON
// (stream.jq's view mode).
func NewCcxFilter() *StreamFilter { return &StreamFilter{} }

// Line takes one complete line and returns what to append to clean.log and
// clean.md.
func (f *StreamFilter) Line(line string) (log, md string) {
	line = validText(line)
	o, ok := parseObj(line)
	if !ok {
		if f.passText {
			return line + "\n", ""
		}
		return "", ""
	}
	var lb, mb strings.Builder
	for _, it := range claudeEvents(o) {
		lb.WriteString(claudeTxt(it) + "\n")
		if out, ok := f.step(it); ok {
			mb.WriteString(out + "\n")
		}
	}
	return lb.String(), mb.String()
}

// step pairs calls with results; ok is false when nothing is written yet.
func (f *StreamFilter) step(it clItem) (string, bool) {
	if it.k == "call" && it.id != "" {
		f.pend = append(f.pend, it)
		return "", false
	}
	if it.k == "result" && it.id != "" {
		for i, c := range f.pend {
			if c.id == it.id {
				f.pend = append(f.pend[:i:i], f.pend[i+1:]...)
				return callfold(c, it) + "\n", true
			}
		}
	}
	return claudeMD(it), true
}

// Close ends the stream: a call that never got a result is written with
// "(no result)".
func (f *StreamFilter) Close() string {
	if len(f.pend) == 0 {
		return ""
	}
	parts := make([]string, len(f.pend))
	for i, c := range f.pend {
		parts[i] = toolline(c.name, c.arg) + "\n\n*(no result)*\n"
	}
	f.pend = nil
	return strings.Join(parts, "\n") + "\n"
}

func renderStream(f *StreamFilter, raw []byte) (log, md string) {
	var lb, mb strings.Builder
	for _, l := range splitLines(raw) {
		a, b := f.Line(l)
		lb.WriteString(a)
		mb.WriteString(b)
	}
	mb.WriteString(f.Close())
	return lb.String(), mb.String()
}

// Claude renders a headless claude -p run's raw.log: clean.log, clean.md,
// the usage of every result and, in last.md, the last result's text.
func Claude(raw []byte) Output {
	log, md := renderStream(NewClaudeFilter(), raw)
	usage, answer := StreamEnd(raw)
	out := Output{CleanLog: log, CleanMD: md, Usage: usage}
	if answer != "" {
		out.Last = answer + "\n"
	}
	return out
}

// Ccx renders a ccx run: its stream-json log into clean.log and clean.md,
// and its stdout (the status line, then the result) into last.md with the
// status line quoted first.
func Ccx(log, stdout []byte) Output {
	clean, md := renderStream(NewCcxFilter(), log)
	first, rest, _ := strings.Cut(string(stdout), "\n")
	return Output{CleanLog: clean, CleanMD: md, Last: "> " + first + "\n\n" + rest}
}

// StreamEnd reads a stream-json log once for its end: the tokens and cost
// of every result event as one line (empty when there is none), and the
// last result's text (empty when there is none or it is empty).
func StreamEnd(raw []byte) (usage, answer string) {
	var in, out, cost float64
	n := 0
	for _, l := range splitLines(raw) {
		o, ok := parseObj(validText(l))
		if t, _ := str(o["type"]); !ok || t != "result" {
			continue
		}
		n++
		u := o.sub("usage")
		in, out, cost = in+numOf(u["input_tokens"]), out+numOf(u["output_tokens"]), cost+numOf(o["total_cost_usd"])
		answer, _ = str(o["result"])
	}
	if n == 0 {
		return "", answer
	}
	usage = jqNumber(in) + " in / " + jqNumber(out) + " out tokens"
	if cost > 0 {
		usage += ", $" + jqNumber(math.Round(cost*10000)/10000)
	}
	return usage, answer
}

func numOf(r json.RawMessage) float64 {
	var f float64
	_ = json.Unmarshal(alt(r), &f)
	return f
}
