package herdr

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// runFake calls FakeMain in-process against a script file.
func runFake(t *testing.T, script string, args ...string) (stdout, stderr string, code int) {
	t.Helper()
	t.Setenv(FakeScriptEnv, script)
	var out, errb bytes.Buffer
	code = FakeMain(args, &out, &errb)
	return out.String(), errb.String(), code
}

func scriptCopy(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "scripts", name+".json"))
	must(t, err)
	path := filepath.Join(t.TempDir(), name+".json")
	must(t, os.WriteFile(path, data, 0o600))
	return path
}

func TestFake_UnscriptedCallFails(t *testing.T) {
	script := scriptCopy(t, "unscripted")
	if _, _, code := runFake(t, script, "pane", "get", "w1:p2"); code != 0 {
		t.Fatalf("scripted call exit %d", code)
	}
	// repeat:once is used up, so the same call is now unscripted
	_, stderr, code := runFake(t, script, "pane", "get", "w1:p2")
	if code != UnscriptedExit || !strings.Contains(stderr, "unscripted call: herdr pane get w1:p2") {
		t.Fatalf("exit %d stderr %q", code, stderr)
	}
	calls, err := ReadFakeCalls(script + ".log.ndjson")
	must(t, err)
	if len(calls) != 2 || calls[0].Unscripted || !calls[1].Unscripted || calls[1].Exit != UnscriptedExit {
		t.Fatalf("log = %+v", calls)
	}
	// through the client the call is an error, never a silent success
	f := installFakeScript(t, `{"rules":[]}`)
	f.allowUnscripted = true
	_, err = newClient(t).Get(bg, "w1:p2")
	var he *Error
	if !errors.As(err, &he) || he.Exit != UnscriptedExit {
		t.Fatalf("client err = %#v", err)
	}
	if calls := f.calls(); len(calls) != 1 || !calls[0].Unscripted {
		t.Fatalf("client call not logged as unscripted: %+v", calls)
	}
	if _, _, code := runFake(t, filepath.Join(t.TempDir(), "missing.json"), "agent", "list"); code != UnscriptedExit {
		t.Fatalf("missing script: exit %d", code)
	}
}

func TestFake_MatchAndTemplates(t *testing.T) {
	script := filepath.Join(t.TempDir(), "s.json")
	must(t, os.WriteFile(script, []byte(`{
	  "state": {"agents": {"w": {"status": "working"}}},
	  "rules": [
	    {"match": ["agent", "get", {"re": "^(?P<pane>w1:p[0-9]+)$"}], "when": {"agents.w.status": "idle"},
	     "replies": [{"shape": {"as": "agent_info", "pane_id": "${cap.pane}", "status": "${state.agents.w.status}"}}]},
	    {"match": ["agent", "get", "*"], "replies": [
	      {"set": {"agents.w.status": "idle"}, "stdout": "first\n"},
	      {"stdout": "second\n"}], "repeat": "cycle"},
	    {"match": ["pane", "**"], "replies": [{"result": {"type": "ok"}}]}
	  ]}`), 0o600))
	steps := []struct{ args, want string }{
		{"agent get w1:p4", "first\n"},
		{"agent get w1:p4", `"pane_id":"w1:p4"`},
		{"pane run w1:p4 echo hi", `{"id":"1","result":{"type":"ok"}}`},
	}
	for _, s := range steps {
		out, stderr, code := runFake(t, script, strings.Fields(s.args)...)
		if code != 0 || !strings.Contains(out, s.want) {
			t.Fatalf("%s: exit %d out %q err %q, want %q", s.args, code, out, stderr, s.want)
		}
	}
	if out, _, _ := runFake(t, script, "agent", "get", "w1:p4"); !strings.Contains(out, `"agent_status":"idle"`) {
		t.Fatalf("state template: %q", out)
	}
}

// schemaRequired lists, per reply type, the required fields of herdr's API
// schema (protocol 22) for the objects the client reads.
var schemaRequired = map[string][]string{
	"agent":        {"terminal_id", "agent_status", "workspace_id", "tab_id", "pane_id", "focused", "revision"},
	"pane":         {"pane_id", "terminal_id", "workspace_id", "tab_id", "focused", "agent_status", "revision"},
	"tab":          {"tab_id", "workspace_id", "number", "label", "focused", "pane_count", "agent_status"},
	"process_info": {"pane_id"},
	"read":         {"pane_id", "workspace_id", "tab_id", "source", "format", "text", "revision", "truncated"},
	"layout":       {"workspace_id", "tab_id", "zoomed", "area", "focused_pane_id", "panes", "splits"},
}

// missingRequired walks a reply result and names every object field the
// schema requires but the reply lacks.
func missingRequired(result map[string]any) []string {
	var missing []string
	check := func(kind string, obj any) {
		m, _ := obj.(map[string]any)
		for _, f := range schemaRequired[kind] {
			if _, ok := m[f]; !ok {
				missing = append(missing, kind+"."+f)
			}
		}
	}
	for key, v := range result {
		switch key {
		case "agent", "pane", "tab", "process_info", "read", "layout":
			check(key, v)
		case "root_pane":
			check("pane", v)
		case "agents":
			list, _ := v.([]any)
			for _, a := range list {
				check("agent", a)
			}
		}
	}
	return missing
}

func TestFake_RepliesMatchSchema(t *testing.T) {
	args := map[string]any{"agents": []any{map[string]any{"name": "a"}}, "panes": []any{map[string]any{"pane_id": "w1:p2"}}}
	for name := range fakeShapes {
		result, err := buildShape(name, args)
		must(t, err)
		m, _ := result.(map[string]any)
		if m["type"] == nil {
			t.Errorf("shape %s has no type", name)
		}
		if missing := missingRequired(m); len(missing) > 0 {
			t.Errorf("shape %s lacks %v", name, missing)
		}
		// every shape survives a JSON round trip, as the client reads it
		if _, err := json.Marshal(m); err != nil {
			t.Errorf("shape %s: %v", name, err)
		}
	}
	// the checker itself catches a reply that drops a required field
	broken, _ := buildShape("agent_info", nil)
	delete(broken.(map[string]any)["agent"].(map[string]any), "terminal_id")
	if missing := missingRequired(broken.(map[string]any)); len(missing) != 1 || missing[0] != "agent.terminal_id" {
		t.Fatalf("checker missed a dropped field: %v", missing)
	}
	if _, err := buildShape("no_such_shape", nil); err == nil {
		t.Fatal("an unknown shape must be an error")
	}
}
