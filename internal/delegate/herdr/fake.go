package herdr

// A scripted fake herdr for tests. Tests link their test binary into a temp
// dir as "herdr", put that dir first on PATH, and have TestMain call FakeMain
// when os.Args[0] is "herdr". FakeMain answers each call from a JSON script:
//
//	{
//	  "state": {"agents": {"w": {"status": "idle"}}},   // free-form, dotted paths
//	  "rules": [
//	    {"match": ["agent", "get", "*"],                 // argv after "herdr"
//	     "when":  {"agents.w.status": "idle"},           // optional state guard
//	     "replies": [{"shape": {"as": "agent_info", "pane_id": "${argv.2}"}}],
//	     "repeat": "last"}                               // last | cycle | once
//	  ]
//	}
//
// Match words: a literal, "*" (one word), "**" (the rest; last only) or
// {"re": "..."} whose named groups become ${cap.name}. Replies are used in
// order; "repeat" says what follows the last one. A reply prints a "shape"
// (see fake_shapes.go) or a "result" as {"id":"1","result":...}, raw
// "stdout"/"stderr" with an "exit", or an "error" {code,message} as herdr's
// JSON on stderr with exit 1. "set" changes state before the reply is built;
// "delay_ms" waits. ${state.x}, ${cap.x} and ${argv.N} work in every string.
//
// A call no rule answers prints "FAKE: unscripted call" and exits
// UnscriptedExit, and the call log marks it, so a test can fail on it.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"
)

const (
	// FakeScriptEnv names the script file the fake replays.
	FakeScriptEnv = "WIPNOTE_HERDR_FAKE_SCRIPT"
	// UnscriptedExit is the fake's exit status for a call no rule answers.
	UnscriptedExit = 97
)

// FakeScript is the fake's script file.
type FakeScript struct {
	State map[string]any `json:"state"`
	Rules []FakeRule     `json:"rules"`
}

// FakeRule answers the calls that match it, in order of its replies.
type FakeRule struct {
	Match   []any            `json:"match"`
	When    map[string]any   `json:"when"`
	Replies []map[string]any `json:"replies"`
	Repeat  string           `json:"repeat"`
}

// FakeCall is one line of the fake's call log (<script>.log.ndjson).
type FakeCall struct {
	Argv       []string `json:"argv"`
	TMS        int64    `json:"t_ms"`
	Rule       int      `json:"rule"`
	Exit       int      `json:"exit"`
	Unscripted bool     `json:"unscripted"`
}

type fakeReply struct {
	Shape   map[string]any `json:"shape"`
	Result  any            `json:"result"`
	Stdout  string         `json:"stdout"`
	Stderr  string         `json:"stderr"`
	Exit    *int           `json:"exit"`
	DelayMS int            `json:"delay_ms"`
	Error   *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

type fakeState struct {
	T0     int64          `json:"t0"`
	Counts map[string]int `json:"counts"`
	Values map[string]any `json:"values"`
}

type fakeOutcome struct {
	stdout, stderr string
	exit, rule     int
	delay          time.Duration
	unscripted     bool
}

// FakeMain runs one fake herdr call and returns its exit status.
func FakeMain(args []string, stdout, stderr io.Writer) int {
	path := os.Getenv(FakeScriptEnv)
	out, err := fakeCall(path, args)
	if err != nil {
		fmt.Fprintf(stderr, "FAKE: broken fake setup: %v\n", err)
		return UnscriptedExit
	}
	time.Sleep(out.delay)
	_, _ = io.WriteString(stdout, out.stdout)
	_, _ = io.WriteString(stderr, out.stderr)
	return out.exit
}

// fakeCall loads the script and state under the state lock, answers the call,
// and saves state and the log line before the lock is released.
func fakeCall(path string, args []string) (fakeOutcome, error) {
	if path == "" {
		return fakeOutcome{}, errors.New(FakeScriptEnv + " is not set")
	}
	var script FakeScript
	data, err := os.ReadFile(path)
	if err != nil {
		return fakeOutcome{}, err
	}
	if err := json.Unmarshal(data, &script); err != nil {
		return fakeOutcome{}, fmt.Errorf("script %s: %w", path, err)
	}
	statePath := path + ".state.json"
	var out fakeOutcome
	err = withDirLock(statePath+".lock", func() error {
		st, err := loadFakeState(statePath, &script)
		if err != nil {
			return err
		}
		now := time.Now().UnixMilli()
		if st.T0 == 0 {
			st.T0 = now
		}
		out = answer(&script, st, args)
		if err := writeJSONAtomic(statePath, st); err != nil {
			return err
		}
		call := FakeCall{Argv: args, TMS: now - st.T0, Rule: out.rule, Exit: out.exit, Unscripted: out.unscripted}
		return appendJSONLine(path+".log.ndjson", call)
	})
	return out, err
}

// answer finds the first rule that matches args and has a reply left.
func answer(script *FakeScript, st *fakeState, args []string) fakeOutcome {
	for i, rule := range script.Rules {
		caps := map[string]string{}
		if !matchArgv(rule.Match, args, caps) || !stateMatches(st.Values, rule.When) {
			continue
		}
		key := fmt.Sprint(i)
		raw := pickReply(rule, st.Counts[key])
		if raw == nil {
			continue
		}
		st.Counts[key]++
		sc := scope{state: st.Values, caps: caps, argv: args}
		if set, ok := raw["set"].(map[string]any); ok {
			for p, v := range set {
				setPath(st.Values, p, resolve(v, sc))
			}
		}
		out := renderReply(resolve(raw, sc))
		out.rule = i
		return out
	}
	return fakeOutcome{
		stderr: "FAKE: unscripted call: herdr " + strings.Join(args, " ") + "\n",
		exit:   UnscriptedExit, rule: -1, unscripted: true,
	}
}

func pickReply(rule FakeRule, n int) map[string]any {
	if len(rule.Replies) == 0 {
		return nil
	}
	switch {
	case n < len(rule.Replies):
		return rule.Replies[n]
	case rule.Repeat == "once":
		return nil
	case rule.Repeat == "cycle":
		return rule.Replies[n%len(rule.Replies)]
	}
	return rule.Replies[len(rule.Replies)-1]
}

// renderReply turns a resolved reply into what the fake prints.
func renderReply(resolved any) fakeOutcome {
	var r fakeReply
	data, _ := json.Marshal(resolved)
	if err := json.Unmarshal(data, &r); err != nil {
		return fakeOutcome{stderr: "FAKE: bad reply: " + err.Error() + "\n", exit: UnscriptedExit}
	}
	out := fakeOutcome{stdout: r.Stdout, stderr: r.Stderr, delay: time.Duration(r.DelayMS) * time.Millisecond}
	if r.Exit != nil {
		out.exit = *r.Exit
	}
	result := r.Result
	if r.Shape != nil {
		shaped, err := buildShape(fmt.Sprint(r.Shape["as"]), r.Shape)
		if err != nil {
			return fakeOutcome{stderr: "FAKE: " + err.Error() + "\n", exit: UnscriptedExit}
		}
		result = shaped
	}
	switch {
	case r.Error != nil:
		body, _ := json.Marshal(map[string]any{"id": "1", "error": r.Error})
		out.stdout, out.stderr = "", string(body)+"\n"
		if r.Exit == nil {
			out.exit = 1
		}
	case result != nil:
		body, _ := json.Marshal(map[string]any{"id": "1", "result": result})
		out.stdout = string(body) + "\n"
	}
	return out
}

// ReadFakeCalls reads a fake's call log. A missing log means no calls.
func ReadFakeCalls(logPath string) ([]FakeCall, error) {
	data, err := os.ReadFile(logPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var calls []FakeCall
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		if line == "" {
			continue
		}
		var c FakeCall
		if err := json.Unmarshal([]byte(line), &c); err != nil {
			return nil, fmt.Errorf("fake log %s: %w", logPath, err)
		}
		calls = append(calls, c)
	}
	return calls, nil
}
