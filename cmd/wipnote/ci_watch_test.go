package main

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/internal/gitwatch"
)

const ciWatchChecksArgv = "gh pr checks 7 --json name,state,bucket,startedAt,completedAt,link,workflow,event"

func ciWatchCheck(state, bucket string) gitwatch.FakeResult {
	return gitwatch.FakeResult{Exit: 8, Stdout: `[{"name":"unit","state":"` + state + `","bucket":"` + bucket +
		`","startedAt":"2026-10-20T10:00:00Z","link":"https://example.test/r/1","workflow":"CI","event":"pull_request"}]`}
}

// ciWatchFake is PR 7 whose checks read pending twice, then pass.
func ciWatchFake() *gitwatch.FakeRunner {
	f := ciStatusFake()
	pending, pass := ciWatchCheck("IN_PROGRESS", "pending"), ciWatchCheck("SUCCESS", "pass")
	f.Seq = map[string][]gitwatch.FakeResult{ciWatchChecksArgv: {pending, pending, pass}}
	return f
}

func watchOnce(t *testing.T, f *gitwatch.FakeRunner, o ciWatchOpts) (int, string) {
	t.Helper()
	var out bytes.Buffer
	clock := &gitwatch.FakeClock{T: time.Date(2026, 10, 26, 12, 0, 0, 0, time.UTC)}
	code, err := runCIWatch(context.Background(), &out, f, clock, "7", o)
	if err != nil {
		t.Fatal(err)
	}
	return code, out.String()
}

func TestCIWatch_JSONLinesOnlyOnChange(t *testing.T) {
	code, out := watchOnce(t, ciWatchFake(), ciWatchOpts{jsonLines: true})
	lines := strings.Split(strings.TrimSuffix(out, "\n"), "\n")
	if code != 0 || len(lines) != 2 {
		t.Fatalf("want exit 0 and 2 lines (pending, passed), got %d and %d:\n%s", code, len(lines), out)
	}
	var first, last gitwatch.Snapshot
	if json.Unmarshal([]byte(lines[0]), &first) != nil || json.Unmarshal([]byte(lines[1]), &last) != nil {
		t.Fatalf("each line must be one JSON snapshot:\n%s", out)
	}
	if first.Counts.Pending != 1 || last.Counts.Pass != 1 || !last.FetchedAt.After(first.FetchedAt) {
		t.Errorf("first %+v, last %+v", first.Counts, last.Counts)
	}
}

func TestCIWatch_TextRepeatsSummaryWithTimestamp(t *testing.T) {
	code, out := watchOnce(t, ciWatchFake(), ciWatchOpts{})
	if code != 0 || strings.Count(out, "PR #7 Add thing") != 2 {
		t.Fatalf("want two summaries and exit 0, got %d:\n%s", code, out)
	}
	for _, want := range []string{"--- 2026-10-26T12:00:00Z\nPR #7", "--- 2026-10-26T12:00:30Z\nPR #7", "1 pass, 0 fail, 0 pending"} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "\x1b[") {
		t.Errorf("text mode must not clear the screen:\n%q", out)
	}
}

func TestCIWatch_ExitCodes(t *testing.T) {
	failed := ciWatchFake()
	failed.Seq[ciWatchChecksArgv] = []gitwatch.FakeResult{ciWatchCheck("FAILURE", "fail")}
	pending := ciWatchFake()
	pending.Seq[ciWatchChecksArgv] = []gitwatch.FakeResult{ciWatchCheck("IN_PROGRESS", "pending")}
	cancelled := ciWatchFake()
	cancelled.Seq[ciWatchChecksArgv] = []gitwatch.FakeResult{ciWatchCheck("CANCELLED", "cancel")}
	gone := &gitwatch.FakeRunner{Results: map[string]gitwatch.FakeResult{
		"gh pr view 7 --json number,title,headRefName,baseRefName,state,url": {Err: gitwatch.ErrNotAvailable}}}
	cases := []struct {
		name string
		f    *gitwatch.FakeRunner
		want int
	}{{"passed", ciWatchFake(), 0}, {"failed", failed, 1}, {"cancelled", cancelled, 1}, {"timeout", pending, 2}, {"not available", gone, 3}}
	for _, c := range cases {
		if code, out := watchOnce(t, c.f, ciWatchOpts{timeout: time.Minute}); code != c.want {
			t.Errorf("%s: exit %d, want %d:\n%s", c.name, code, c.want, out)
		}
	}
}

// --no-checks-grace reaches the loop: zero checks for 90s ends with a note
// and exit 0 at 12:01:30, not at the 2m default.
func TestCIWatch_NoChecksGrace(t *testing.T) {
	f := ciWatchFake()
	f.Seq[ciWatchChecksArgv] = []gitwatch.FakeResult{{Stderr: "no checks reported on the 'x' branch\n", Exit: 1}}
	code, out := watchOnce(t, f, ciWatchOpts{timeout: time.Hour, noChecksGrace: 90 * time.Second})
	if code != 0 || !strings.Contains(out, "--- 2026-10-26T12:01:30Z") || !strings.Contains(out, "note: no checks reported") {
		t.Errorf("want exit 0 with the note at 12:01:30, got %d:\n%s", code, out)
	}
}

func TestCIWatch_HelpNamesExitCodesAndGrace(t *testing.T) {
	long := ciWatchCmd().Long
	for _, want := range []string{"--no-checks-grace", "1 a check failed or was cancelled", "3 failed reads in a row"} {
		if !strings.Contains(long, want) {
			t.Errorf("help is missing %q:\n%s", want, long)
		}
	}
}

func TestCIWatch_CancelledIsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	code, err := runCIWatch(ctx, &bytes.Buffer{}, ciWatchFake(), &gitwatch.FakeClock{}, "7", ciWatchOpts{})
	if err != nil || code != ciWatchInterrupted {
		t.Errorf("Ctrl-C: want exit %d and no error, got %d, %v", ciWatchInterrupted, code, err)
	}
}

func TestCIWatch_IntervalFloor(t *testing.T) {
	f := ciWatchFake()
	_, err := runCIWatch(context.Background(), &bytes.Buffer{}, f, &gitwatch.FakeClock{}, "7",
		ciWatchOpts{checksInterval: 4 * time.Second})
	if err == nil || len(f.Calls) != 0 {
		t.Errorf("an interval under 5s must fail before any read: %v, %q", err, f.Calls)
	}
}

func TestCIWatch_RegisteredWithDefaults(t *testing.T) {
	cmd, _, err := ciCmd().Find([]string{"watch"})
	if err != nil || cmd.Name() != "watch" {
		t.Fatalf("ci watch not registered: %v", err)
	}
	for flag, want := range map[string]string{"timeout": "1h0m0s", "interval-checks": "15s", "interval-slow": "1m0s", "json-lines": "false",
		"no-checks-grace": "2m0s"} {
		if f := cmd.Flags().Lookup(flag); f == nil || f.DefValue != want {
			t.Errorf("--%s: %+v, want default %s", flag, f, want)
		}
	}
}

// The watch loop is foreground only: nothing but the ci watch command may
// start it, so no hook handler can (plan slice 5: never from a hook).
func TestCIWatch_OnlyTheCommandStartsTheLoop(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil || len(files) == 0 {
		t.Fatalf("glob: %v", err)
	}
	for _, name := range files {
		if strings.HasSuffix(name, "_test.go") || name == "ci_watch.go" {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, call := range []string{"gitwatch.Watch(", "runCIWatch(", "ciWatchCmd("} {
			if strings.Contains(string(data), call) && !(name == "ci.go" && call == "ciWatchCmd(") {
				t.Errorf("%s calls %s; only the ci watch command may", name, call)
			}
		}
	}
}
