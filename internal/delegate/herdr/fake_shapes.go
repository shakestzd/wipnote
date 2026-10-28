package herdr

// Reply shapes, templates, matching and state files for the fake (fake.go).
// Each shape carries every field herdr's API schema (protocol 22) marks
// required for that reply; TestFake_RepliesMatchSchema checks it.

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func arg(a map[string]any, key string, def any) any {
	if v, ok := a[key]; ok && v != nil {
		return v
	}
	return def
}

// workspaceOf returns the workspace part of a pane or tab id ("w1:p2" -> "w1").
func workspaceOf(id string) string {
	ws, _, _ := strings.Cut(id, ":")
	return ws
}

func paneObj(id string) map[string]any {
	return map[string]any{
		"pane_id": id, "terminal_id": "t-" + id, "workspace_id": workspaceOf(id), "tab_id": workspaceOf(id) + ":t1",
		"focused": false, "agent_status": "idle", "revision": 1,
	}
}

// agentObj carries cwd always and foreground_cwd only when scripted: herdr
// reports it only when it can resolve the foreground process's cwd
// (https://herdr.dev/docs/cli-reference/).
func agentObj(a map[string]any) map[string]any {
	pane := fmt.Sprint(arg(a, "pane_id", "w1:p2"))
	obj := map[string]any{
		"terminal_id": arg(a, "terminal_id", "t-"+pane), "agent_status": arg(a, "status", "working"),
		"workspace_id": workspaceOf(pane), "tab_id": workspaceOf(pane) + ":t1", "pane_id": pane,
		"focused": false, "revision": 7, "agent": arg(a, "kind", "claude"), "name": arg(a, "name", "worker"),
		"completion_seq": arg(a, "completion_seq", 4), "interactive_ready": true, "cwd": arg(a, "cwd", ""),
	}
	if fg, ok := a["foreground_cwd"]; ok && fg != nil {
		obj["foreground_cwd"] = fg
	}
	return obj
}

func objects(v any) []map[string]any {
	list, _ := v.([]any)
	var out []map[string]any
	for _, item := range list {
		if m, ok := item.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

func processInfoObj(a map[string]any) map[string]any {
	shell := arg(a, "shell_pid", 900)
	fg := objects(a["fg"])
	if len(fg) == 0 {
		fg = append([]map[string]any{{"pid": shell, "name": "zsh"}}, objects(a["helpers"])...)
	}
	var procs []any
	for _, p := range fg {
		procs = append(procs, map[string]any{"pid": p["pid"], "name": p["name"], "argv0": p["name"], "cmdline": p["name"]})
	}
	info := map[string]any{
		"pane_id": arg(a, "pane_id", "w1:p2"), "shell_pid": shell,
		"foreground_process_group_id": fg[0]["pid"], "foreground_processes": procs,
	}
	for _, key := range objectsOrStrings(a["omit"]) {
		delete(info, key)
	}
	return map[string]any{"type": "pane_process_info", "process_info": info}
}

func objectsOrStrings(v any) []string {
	list, _ := v.([]any)
	var out []string
	for _, item := range list {
		out = append(out, fmt.Sprint(item))
	}
	return out
}

func layoutObj(a map[string]any) map[string]any {
	var panes []any
	first := ""
	for _, p := range objects(a["panes"]) {
		id := fmt.Sprint(p["pane_id"])
		if first == "" {
			first = id
		}
		rect := map[string]any{"x": 0, "y": 0, "width": arg(p, "width", 80), "height": arg(p, "height", 24)}
		panes = append(panes, map[string]any{"pane_id": id, "focused": false, "rect": rect})
	}
	return map[string]any{"type": "pane_layout", "layout": map[string]any{
		"workspace_id": workspaceOf(first), "tab_id": workspaceOf(first) + ":t1", "zoomed": false,
		"area": map[string]any{"x": 0, "y": 0, "width": 160, "height": 48}, "focused_pane_id": first,
		"panes": panes, "splits": []any{},
	}}
}

// fakeShapes builds each named reply result from the shape's arguments.
var fakeShapes = map[string]func(a map[string]any) any{
	"ok": func(map[string]any) any { return map[string]any{"type": "ok"} },
	"pane_info": func(a map[string]any) any {
		return map[string]any{"type": "pane_info", "pane": paneObj(fmt.Sprint(arg(a, "pane_id", "w1:p2")))}
	},
	"tab_created": func(a map[string]any) any {
		tab := fmt.Sprint(arg(a, "tab_id", "w1:t2"))
		return map[string]any{"type": "tab_created", "root_pane": paneObj(fmt.Sprint(arg(a, "pane_id", "w1:p2"))),
			"tab": map[string]any{"tab_id": tab, "workspace_id": workspaceOf(tab), "number": 2, "label": arg(a, "label", "workers"),
				"focused": false, "pane_count": 1, "agent_status": "idle"}}
	},
	"agent_info": func(a map[string]any) any { return map[string]any{"type": "agent_info", "agent": agentObj(a)} },
	"agent_list": func(a map[string]any) any {
		var agents []any
		for _, ag := range objects(a["agents"]) {
			agents = append(agents, agentObj(ag))
		}
		return map[string]any{"type": "agent_list", "agents": agents}
	},
	"process_info": func(a map[string]any) any { return processInfoObj(a) },
	"pane_read": func(a map[string]any) any {
		pane := fmt.Sprint(arg(a, "pane_id", "w1:p2"))
		return map[string]any{"type": "pane_read", "read": map[string]any{
			"pane_id": pane, "workspace_id": workspaceOf(pane), "tab_id": workspaceOf(pane) + ":t1", "source": "recent_unwrapped",
			"format": "text", "text": arg(a, "text", "line one\nline two"), "revision": 3, "truncated": false}}
	},
	"pane_layout": func(a map[string]any) any { return layoutObj(a) },
}

func buildShape(name string, a map[string]any) (any, error) {
	build, ok := fakeShapes[name]
	if !ok {
		return nil, fmt.Errorf("unknown shape %q", name)
	}
	if a == nil {
		a = map[string]any{}
	}
	return build(a), nil
}

type scope struct {
	state map[string]any
	caps  map[string]string
	argv  []string
}

var templateRe = regexp.MustCompile(`\$\{([^}]+)\}`)

func (s scope) lookup(ref string) any {
	head, rest, _ := strings.Cut(ref, ".")
	switch head {
	case "cap":
		return s.caps[rest]
	case "argv":
		if n, err := strconv.Atoi(rest); err == nil && n >= 0 && n < len(s.argv) {
			return s.argv[n]
		}
	case "state":
		return getPath(s.state, rest)
	}
	return nil
}

// resolve expands templates. A string that is only a template keeps the
// value's type; inside longer text the value is stringified.
func resolve(v any, s scope) any {
	switch t := v.(type) {
	case string:
		if m := templateRe.FindStringSubmatch(t); m != nil && m[0] == t {
			return s.lookup(m[1])
		}
		return templateRe.ReplaceAllStringFunc(t, func(m string) string {
			if v := s.lookup(m[2 : len(m)-1]); v != nil {
				return fmt.Sprint(v)
			}
			return ""
		})
	case []any:
		out := make([]any, len(t))
		for i, item := range t {
			out[i] = resolve(item, s)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, item := range t {
			out[k] = resolve(item, s)
		}
		return out
	}
	return v
}

func matchArgv(patterns []any, argv []string, caps map[string]string) bool {
	for i, p := range patterns {
		if p == "**" {
			return i == len(patterns)-1
		}
		if i >= len(argv) {
			return false
		}
		switch t := p.(type) {
		case string:
			if t != "*" && t != argv[i] {
				return false
			}
		case map[string]any:
			re, err := regexp.Compile("(?s)" + fmt.Sprint(t["re"]))
			m := []string(nil)
			if err == nil {
				m = re.FindStringSubmatch(argv[i])
			}
			if m == nil {
				return false
			}
			for j, name := range re.SubexpNames() {
				if name != "" {
					caps[name] = m[j]
				}
			}
		default:
			return false
		}
	}
	return len(patterns) == len(argv)
}

func getPath(root map[string]any, path string) any {
	var cur any = root
	for _, key := range strings.Split(path, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[key]
	}
	return cur
}

func setPath(root map[string]any, path string, v any) {
	keys := strings.Split(path, ".")
	cur := root
	for _, key := range keys[:len(keys)-1] {
		next, ok := cur[key].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[key] = next
		}
		cur = next
	}
	cur[keys[len(keys)-1]] = v
}

func sameJSON(a, b any) bool {
	x, _ := json.Marshal(a)
	y, _ := json.Marshal(b)
	return string(x) == string(y)
}

func stateMatches(values, when map[string]any) bool {
	for p, v := range when {
		if !sameJSON(getPath(values, p), v) {
			return false
		}
	}
	return true
}

func loadFakeState(path string, script *FakeScript) (*fakeState, error) {
	st := &fakeState{Counts: map[string]int{}, Values: map[string]any{}}
	data, err := os.ReadFile(path)
	if err == nil {
		err = json.Unmarshal(data, st)
	} else if os.IsNotExist(err) {
		err = nil
		for k, v := range script.State {
			st.Values[k] = v
		}
	}
	if st.Counts == nil {
		st.Counts = map[string]int{}
	}
	if st.Values == nil {
		st.Values = map[string]any{}
	}
	return st, err
}

func writeJSONAtomic(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	tmp := fmt.Sprintf("%s.%d.tmp", path, os.Getpid())
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func appendJSONLine(path string, v any) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(append(data, '\n'))
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}

// withDirLock serialises fake calls with an mkdir lock; a lock older than
// five seconds belongs to a crashed caller and is broken.
func withDirLock(dir string, fn func() error) error {
	deadline := time.Now().Add(10 * time.Second)
	for os.Mkdir(dir, 0o700) != nil {
		if info, err := os.Stat(dir); err == nil && time.Since(info.ModTime()) > 5*time.Second {
			_ = os.Remove(dir)
			continue
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("fake state lock busy: %s", dir)
		}
		time.Sleep(2 * time.Millisecond)
	}
	defer os.Remove(dir)
	return fn()
}
