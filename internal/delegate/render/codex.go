package render

import (
	"encoding/json"
	"strings"
)

// The Codex filter (the mod's codex.jq): `codex exec --json` events, as
// codex-cli 0.163.0 prints them, parsed once into neutral items that the
// text (clean.log) and Markdown (clean.md) formats only format. Unknown item
// kinds are shown by name rather than dropped.

// cxItem is one Codex event: say, think, call, result, err, turn or done.
type cxItem struct {
	k       string
	text    string
	pre     string
	name    string
	bare    bool    // a command: its label waits for its result
	arg     *string // nil is jq's null
	command *string
	code    string
	err     bool
	usage   obj
}

func ptr(s string) *string { return &s }

// codexEvent parses one event; false when it shows nothing.
func codexEvent(o obj) (cxItem, bool) {
	typ, _ := str(o["type"])
	switch typ {
	case "item.started", "item.completed":
		return codexItem(o, typ == "item.completed")
	case "turn.started":
		return cxItem{k: "turn"}, true
	case "turn.completed":
		return cxItem{k: "done", usage: o.sub("usage")}, true
	case "turn.failed":
		msg, ok := textOr(o.sub("error")["message"], "unknown")
		return cxItem{k: "err", pre: "turn failed: ", text: msg}, ok
	case "error":
		msg, ok := textOr(o["message"], "error")
		return cxItem{k: "err", text: msg}, ok
	}
	return cxItem{}, false
}

func codexItem(o obj, completed bool) (cxItem, bool) {
	i := o.sub("item")
	kind, _ := str(i["type"])
	switch kind {
	case "agent_message":
		text, ok := str(i["text"])
		return cxItem{k: "say", text: text}, ok
	case "error":
		msg, ok := textOr(i["message"], "error")
		return cxItem{k: "err", text: msg}, ok
	case "reasoning":
		text, ok := textOr(i["text"], "")
		return cxItem{k: "think", text: text}, ok
	case "command_execution":
		return commandItem(i, completed)
	case "web_search":
		return cxItem{k: "call", name: "web_search", arg: searchQuery(i)}, completed
	case "file_change":
		return cxItem{k: "call", name: "file_change", arg: fileChanges(i)}, completed
	}
	if !completed {
		return cxItem{}, false
	}
	name := "item"
	if truthy(i["type"]) {
		name = tostring(i["type"])
	}
	it := cxItem{k: "call", name: name}
	if truthy(i["path"]) {
		p, ok := str(i["path"])
		if !ok {
			return cxItem{}, false
		}
		it.arg = &p
	}
	return it, true
}

// commandItem: a started command is its label; a completed one its result.
func commandItem(i obj, completed bool) (cxItem, bool) {
	if !completed {
		c, ok := textOr(i["command"], "")
		return cxItem{k: "call", name: "Bash", bare: true, arg: &c}, ok
	}
	it := cxItem{k: "result", code: "?"}
	if c, ok := str(i["command"]); ok {
		it.command = &c
	}
	if exit := i["exit_code"]; truthy(exit) {
		it.code = tostring(exit)
		var n float64
		it.err = json.Unmarshal(exit, &n) != nil || n != 0
	}
	text, ok := textOr(i["aggregated_output"], "")
	it.text = text
	return it, ok
}

// searchQuery is a web search's query, in the older and newer item shapes.
func searchQuery(i obj) *string {
	a := i.sub("action")
	if q := alt(i["query"], a["query"]); q != nil {
		return ptr(tostring(q))
	}
	var qs []json.RawMessage
	if json.Unmarshal(a["queries"], &qs) != nil || qs == nil {
		return nil
	}
	parts := make([]string, len(qs))
	for n, q := range qs {
		if truthy(q) {
			parts[n] = tostring(q)
		}
	}
	return ptr(strings.Join(parts, "; "))
}

// fileChanges names each changed file with its kind (add, update, delete).
func fileChanges(i obj) *string {
	var changes []obj
	_ = json.Unmarshal(i["changes"], &changes)
	var parts []string
	for _, c := range changes {
		var words []string
		for _, k := range []string{"kind", "path"} {
			if v := c[k]; len(v) > 0 && string(v) != "null" {
				words = append(words, tostring(v))
			}
		}
		parts = append(parts, strings.Join(words, " "))
	}
	if s := strings.Join(parts, ", "); s != "" {
		return &s
	}
	return nil
}

// codexTxt formats an item for clean.log.
func codexTxt(it cxItem) (string, bool) {
	switch it.k {
	case "say":
		return it.text, true
	case "err":
		return "! " + it.pre + clip(it.text, 300), true
	case "think":
		return "(thinking) " + clip(it.text, 200), true
	case "call":
		if it.bare {
			return "> " + clip(*it.arg, 160), true
		}
		if it.arg != nil {
			return "> " + it.name + " " + *it.arg, true
		}
		return "> " + it.name, true
	case "result":
		return "  = exit " + it.code + ": " + clip(it.text, 200), true
	case "done":
		return "--- done: " + usageSplit(it.usage) + " ---", true
	}
	return "", false
}

// codexMD formats an item for clean.md.
func codexMD(it cxItem) (string, bool) {
	switch it.k {
	case "say":
		return "### Assistant\n\n" + prose(it.text) + "\n", true
	case "err":
		return errquote(it.pre+it.text) + "\n", true
	case "think":
		return "**thinking**\n\n" + fenced(clip(it.text, 200), "") + "\n", true
	case "call":
		if it.bare {
			return "", false // shown once it completes, label and result together
		}
		arg := ""
		if it.arg != nil {
			arg = *it.arg
		}
		return toolline(it.name, arg) + "\n", true
	case "result":
		return commandFold(it) + "\n", true
	case "turn":
		return "### Turn\n", true
	case "done":
		return "*" + usageSplit(it.usage) + "*\n", true
	}
	return "", false
}

// commandFold is one collapsible section per command: a summary title, then
// its label and output.
func commandFold(it cxItem) string {
	title, label := "Bash", ""
	if it.command != nil {
		c := unwrapShell(*it.command)
		title = "Bash: " + cut(collapse(c), 240)
		label = toolline("Bash", cut(c, 2000)) + "\n\n"
	}
	title += " · exit " + it.code + " · " + resultsize(it.text)
	return fold(title, it.err, label+result(it.text, 1500, it.err, "exit "+it.code))
}
