package render

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// Output is a run rendered to its run-folder files.
type Output struct {
	CleanLog string // clean.log
	CleanMD  string // clean.md
	Last     string // last.md
	Usage    string // the usage line, no newline; empty when there is none
}

// CodexFilter renders a Codex stream a line at a time, as the mod's pane
// script does with two consumers of one tee: codex.jq's text then the noise
// rules into clean.log; the noise rules, codex.jq's Markdown and joinfence
// into clean.md.
type CodexFilter struct {
	logRules cleaner
	mdRules  cleaner
	join     joinFence
}

// NewCodexFilter returns a filter for one Codex stream.
func NewCodexFilter() *CodexFilter {
	return &CodexFilter{logRules: cleaner{review: true}, mdRules: cleaner{review: true}}
}

// Line takes one raw line (without its newline) and returns what to append
// to clean.log and to clean.md.
func (f *CodexFilter) Line(raw string) (log, md string) {
	raw = validText(raw)
	o, isObj := parseObj(raw)
	var txt []string
	switch {
	case !isObj:
		txt = []string{raw}
	default:
		if it, ok := codexEvent(o); ok {
			if t, ok := codexTxt(it); ok {
				txt = strings.Split(t, "\n")
			}
		}
	}
	var lb strings.Builder
	for _, l := range txt {
		if kept, ok := f.logRules.line(l); ok {
			lb.WriteString(kept + "\n")
		}
	}
	return lb.String(), f.markdown(raw)
}

func (f *CodexFilter) markdown(raw string) string {
	kept, ok := f.mdRules.line(raw)
	if !ok {
		return ""
	}
	var out string
	if o, isObj := parseObj(kept); isObj {
		it, ok := codexEvent(o)
		if !ok {
			return ""
		}
		if out, ok = codexMD(it); !ok {
			return ""
		}
	} else if out, ok = mdline(kept); !ok {
		return ""
	} else {
		out += "\n"
	}
	var b strings.Builder // jq -r ends each output with a newline
	for _, l := range splitLines([]byte(out + "\n")) {
		b.WriteString(f.join.line(l))
	}
	return b.String()
}

// Close ends the stream: a fence joinfence held back is written.
func (f *CodexFilter) Close() (log, md string) { return "", f.join.flush() }

// Codex renders a whole Codex raw.log.
func Codex(raw []byte) Output {
	f := NewCodexFilter()
	var log, md strings.Builder
	for _, l := range splitLines(raw) {
		a, b := f.Line(l)
		log.WriteString(a)
		md.WriteString(b)
	}
	_, tail := f.Close()
	md.WriteString(tail)
	out := Output{CleanLog: log.String(), CleanMD: md.String(), Last: CodexLast(raw)}
	out.Usage = CodexUsage(raw, out.CleanLog)
	return out
}

// CodexLast is the final assistant message, as codex --output-last-message
// writes it to last.md: the text of the last agent message, as it is.
func CodexLast(raw []byte) string {
	last := ""
	for _, l := range splitLines(raw) {
		o, ok := parseObj(validText(l))
		if t, _ := str(o["type"]); !ok || t != "item.completed" {
			continue
		}
		i := o.sub("item")
		if k, _ := str(i["type"]); k == "agent_message" {
			if text, ok := str(i["text"]); ok {
				last = text
			}
		}
	}
	return last
}

// CodexUsage is the last turn's token split, else the figure on the line
// after codex review's "tokens used" in clean.log.
func CodexUsage(raw []byte, cleanLog string) string {
	usage := ""
	for _, l := range splitLines(raw) {
		o, ok := parseObj(validText(l))
		if t, _ := str(o["type"]); ok && t == "turn.completed" {
			usage = usageSplit(o.sub("usage"))
		}
	}
	if usage != "" {
		return usage
	}
	lines := splitLines([]byte(cleanLog))
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(lines[i], "tokens used") {
			k := lines[min(i+1, len(lines)-1)]
			if k != "" {
				return k + " tokens"
			}
			return ""
		}
	}
	return ""
}

// usageSplit is "input N (cached N), output N[, reasoning N]".
func usageSplit(u obj) string {
	n := func(k string) json.RawMessage {
		if v := alt(u[k]); v != nil {
			return v
		}
		return json.RawMessage("0")
	}
	s := "input " + commas(n("input_tokens")) + " (cached " + commas(n("cached_input_tokens")) +
		"), output " + commas(n("output_tokens"))
	var r float64
	if json.Unmarshal(n("reasoning_output_tokens"), &r) == nil && r > 0 {
		s += ", reasoning " + commas(n("reasoning_output_tokens"))
	}
	return s
}

// commas groups a token count by thousands.
func commas(r json.RawMessage) string {
	var f float64
	if json.Unmarshal(r, &f) != nil || f < 1000 {
		return tostring(r)
	}
	n := int64(math.Floor(f))
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	return b.String()
}
