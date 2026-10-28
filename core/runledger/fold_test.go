package runledger

import (
	"strings"
	"testing"
	"time"
)

var t0 = time.Date(2026, 10, 27, 16, 31, 0, 0, time.UTC)

func intp(n int) *int { return &n }

func startEvent(session, toolUse, kind, desc string) Event {
	return Event{
		RunID: RunIDFor(session, toolUse), Phase: PhaseStart, At: t0,
		ToolUseID: toolUse, TaskID: "t-" + toolUse, Kind: kind, Description: desc,
		Command: "scripts/gate.sh", Folder: ".", Commit: "abc1234def5678",
		Session: session, WorkItem: "feat-1234abcd", Harness: "claude",
		LogPath: "…/tasks/t-" + toolUse + ".output",
	}
}

func endEvent(session, toolUse, status string, exit *int, at time.Time) Event {
	return Event{
		RunID: RunIDFor(session, toolUse), Phase: PhaseEnd, At: at,
		ToolUseID: toolUse, TaskID: "t-" + toolUse, Status: status, Exit: exit,
		Session: session, LastLine: "ok", Harness: "claude",
	}
}

func never(string) bool  { return false }
func always(string) bool { return true }

func TestFold_StartPlusNotificationIsComplete(t *testing.T) {
	recs := Fold([]Event{
		startEvent("s1", "tu1", KindShell, "Rebuild and run the full gate"),
		endEvent("s1", "tu1", StatusCompleted, intp(0), t0.Add(5*time.Minute)),
	}, never)
	if len(recs) != 1 {
		t.Fatalf("got %d records, want 1", len(recs))
	}
	r := recs[0]
	if r.Status != StatusCompleted || r.Exit == nil || *r.Exit != 0 {
		t.Errorf("status/exit = %s/%v", r.Status, r.Exit)
	}
	if !r.StartedAt.Equal(t0) || !r.EndedAt.Equal(t0.Add(5*time.Minute)) {
		t.Errorf("times = %v..%v", r.StartedAt, r.EndedAt)
	}
	if r.Description != "Rebuild and run the full gate" || r.Commit == "" || r.LastLine != "ok" || r.LogPath == "" {
		t.Errorf("start fields lost: %+v", r)
	}
	if r.Session != "s1" || r.WorkItem != "feat-1234abcd" || r.TaskID != "t-tu1" {
		t.Errorf("links lost: %+v", r)
	}
}

func TestFold_NoNotificationIsRunningUntilSessionEnds(t *testing.T) {
	ev := []Event{startEvent("s1", "tu1", KindShell, "long build")}
	if r := Fold(ev, never)[0]; r.Status != StatusRunning || !r.EndedAt.IsZero() || r.Exit != nil {
		t.Errorf("live session: %+v, want running with no end", r)
	}
	if r := Fold(ev, always)[0]; r.Status != StatusUnknown {
		t.Errorf("ended session: status %s, want unknown", r.Status)
	}
}

func TestFold_NonzeroExitFails(t *testing.T) {
	end := endEvent("s1", "tu1", StatusFailed, intp(2), t0.Add(time.Minute))
	end.FirstError = "--- FAIL: TestX (0.00s)"
	r := Fold([]Event{startEvent("s1", "tu1", KindShell, "tests"), end}, never)[0]
	if r.Status != StatusFailed || r.Exit == nil || *r.Exit != 2 {
		t.Errorf("status/exit = %s/%v, want failed/2", r.Status, r.Exit)
	}
	if r.FirstError != "--- FAIL: TestX (0.00s)" {
		t.Errorf("first error = %q", r.FirstError)
	}
}

func TestFold_SubagentAndMonitorFromEndsAlone(t *testing.T) {
	agent := endEvent("s1", "tuA", StatusCompleted, nil, t0)
	agent.Kind, agent.Description = KindSubagent, "Design critic"
	mon := endEvent("s1", "tuM", StatusCompleted, nil, t0.Add(time.Second))
	mon.Kind, mon.Description = KindMonitor, "gate watch"
	recs := Fold([]Event{agent, mon}, never)
	if len(recs) != 2 {
		t.Fatalf("got %d, want 2", len(recs))
	}
	kinds := recs[0].Kind + "," + recs[1].Kind
	if kinds != "subagent,monitor" {
		t.Errorf("kinds = %s", kinds)
	}
	if recs[0].Status != StatusCompleted || !recs[0].StartedAt.IsZero() {
		t.Errorf("end-only record = %+v, want completed with unknown start", recs[0])
	}
}

func TestFold_RepeatedNotificationKeepsLatestEndOnce(t *testing.T) {
	recs := Fold([]Event{
		startEvent("s1", "tuA", KindSubagent, "worker"),
		endEvent("s1", "tuA", StatusCompleted, nil, t0.Add(time.Minute)),
		endEvent("s1", "tuA", StatusCompleted, nil, t0.Add(3*time.Minute)),
	}, never)
	if len(recs) != 1 || !recs[0].EndedAt.Equal(t0.Add(3*time.Minute)) {
		t.Fatalf("records = %+v, want one with the latest end", recs)
	}
}

func TestFold_EndMatchedByTaskIDWhenToolUseMissing(t *testing.T) {
	end := Event{RunID: RunIDFor("s1", "task:t-tu1"), Phase: PhaseEnd, At: t0.Add(time.Minute),
		TaskID: "t-tu1", Status: StatusCompleted, Session: "s1"}
	recs := Fold([]Event{startEvent("s1", "tu1", KindSubagent, "resumed"), end}, never)
	if len(recs) != 1 || recs[0].Status != StatusCompleted {
		t.Fatalf("records = %+v, want the end joined to the start by task id", recs)
	}
	// A different task id stays a separate record.
	other := end
	other.TaskID, other.RunID = "t-zz", RunIDFor("s1", "task:t-zz")
	if n := len(Fold([]Event{startEvent("s1", "tu1", KindShell, "x"), other}, never)); n != 2 {
		t.Errorf("unrelated end joined: %d records", n)
	}
}

func TestForWorkItem_UnlinkedRunsStayOnTheSessionOnly(t *testing.T) {
	linked := startEvent("s1", "tu1", KindShell, "gate")
	unlinked := startEvent("s1", "tu2", KindShell, "ls")
	unlinked.WorkItem = ""
	recs := Fold([]Event{linked, unlinked}, never)
	got := ForWorkItem(recs, "feat-1234abcd")
	if len(got) != 1 || got[0].Description != "gate" {
		t.Fatalf("ForWorkItem = %+v", got)
	}
	if len(ForWorkItem(recs, "feat-ffffffff")) != 0 {
		t.Error("another item must see no runs")
	}
	for _, r := range recs {
		if r.Session != "s1" {
			t.Errorf("run %s lost its session", r.ID)
		}
	}
}

func TestMapStatus(t *testing.T) {
	cases := []struct {
		word string
		exit *int
		want string
	}{
		{"completed", intp(0), StatusCompleted},
		{"completed", nil, StatusCompleted},
		{"completed", intp(1), StatusFailed},
		{"failed", nil, StatusFailed},
		{"killed", nil, StatusKilled},
		{"stopped", nil, StatusKilled},
		{"weird", nil, StatusUnknown},
	}
	for _, c := range cases {
		if got := MapStatus(c.word, c.exit); got != c.want {
			t.Errorf("MapStatus(%q, %v) = %s, want %s", c.word, c.exit, got, c.want)
		}
	}
}

func TestDescribe(t *testing.T) {
	recs := Fold([]Event{
		startEvent("s1", "tu1", KindShell, "gate"),
		endEvent("s1", "tu1", StatusCompleted, intp(0), t0.Add(4*time.Minute)),
	}, never)
	if got := Describe(recs[0], time.UTC); got != "gate exit 0 at 16:35 on commit abc1234" {
		t.Errorf("Describe = %q", got)
	}
	running := Fold([]Event{startEvent("s1", "tu2", KindShell, "build")}, never)[0]
	if got := Describe(running, time.UTC); got != "build running since 16:31 on commit abc1234" {
		t.Errorf("Describe running = %q", got)
	}
	lost := Fold([]Event{startEvent("s1", "tu3", KindShell, "watch")}, always)[0]
	if got := Describe(lost, time.UTC); !strings.HasPrefix(got, "watch unknown") {
		t.Errorf("Describe unknown = %q", got)
	}
}
