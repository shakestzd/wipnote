package session

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

func (e *env) wait(name string, timeout time.Duration) WaitResult {
	e.t.Helper()
	res, err := Wait(context.Background(), e.deps, name, WaitOptions{Timeout: timeout, Poll: 5 * time.Millisecond})
	if err != nil {
		e.t.Fatalf("wait %s: %v", name, err)
	}
	return res
}

// --- Settle (D1, D1b) ---

func TestWait_ResultFileEndsWaitWithoutHooks(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "brief")
	task := e.task("w1")
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("settled with no signal: %+v", res)
	}
	e.writeResult("w1", task, "answer")
	res := e.wait("w1", 5*time.Second)
	if res.Outcome != OutcomeDone || res.Task != task || res.Run != s.Run || res.Turn != 1 {
		t.Fatalf("%+v", res)
	}
}

func TestWait_StopWithoutResultIsIdleNoResult(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleNoResult {
		t.Fatalf("%+v", res)
	}
	e.writeResult("w1", e.task("w1"), "answer")
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeDone {
		t.Fatalf("with a result file: %+v", res)
	}
}

func TestWait_CurrentTurnOnly(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	first := e.send("w1", "first").Task
	e.startTurn(s, "sid-1")
	e.writeResult("w1", first, "one")
	e.event(s.Run, "sid-1", runstore.KindStop)
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeDone || res.Task != first {
		t.Fatalf("%+v", res)
	}
	second := e.send("w1", "second").Task
	// The first turn's stop and result file are older-turn events.
	if res := e.wait("w1", 80*time.Millisecond); res.Outcome != OutcomeTimeout || res.Task != second || res.Turn != 2 {
		t.Fatalf("older turn settled the wait: %+v", res)
	}
	e.startTurn(s, "sid-1")
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("%+v", res)
	}
	e.event(s.Run, "sid-1", runstore.KindStop)
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleNoResult || res.Task != second {
		t.Fatalf("%+v", res)
	}
}

func TestWait_RequiresTimeout(t *testing.T) {
	e := newEnv(t)
	e.worker("w1", "")
	for _, d := range []time.Duration{0, -time.Second} {
		if _, err := Wait(context.Background(), e.deps, "w1", WaitOptions{Timeout: d}); !errors.Is(err, ErrNoTimeout) {
			t.Fatalf("timeout %s: err %v", d, err)
		}
	}
	if _, err := Wait(context.Background(), e.deps, "w1", WaitOptions{Timeout: time.Second}); err != nil {
		t.Fatalf("a timeout given: %v", err)
	}
}

func TestWait_WorkingStateResultFileReturnsResult(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1") // working, and it stays working: no stop
	go func() {
		time.Sleep(40 * time.Millisecond)
		e.writeResult("w1", e.task("w1"), "answer")
	}()
	if res := e.wait("w1", 5*time.Second); res.Outcome != OutcomeDone {
		t.Fatalf("%+v", res)
	}
}

func TestWait_StoppedWhileWaitingReturnsStoppedBeforeTimeout(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	go func() {
		time.Sleep(50 * time.Millisecond)
		_, _ = e.deps.Ledger.CloseIfOpen(s.Run, delegationledger.CloseInfo{Reason: delegationledger.EndStop})
	}()
	start := time.Now()
	res := e.wait("w1", 10*time.Second)
	if res.Outcome != OutcomeStopped || time.Since(start) > 2*time.Second {
		t.Fatalf("%+v after %s", res, time.Since(start))
	}
}

func TestWait_NoResultNoStopTimesOut(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	start := time.Now()
	res := e.wait("w1", 100*time.Millisecond)
	if res.Outcome != OutcomeTimeout || time.Since(start) < 100*time.Millisecond {
		t.Fatalf("%+v", res)
	}
	e.event(s.Run, "sid-1", runstore.KindStop) // other side: the stop settles it
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleNoResult {
		t.Fatalf("%+v", res)
	}
}

// --- Ready (D6) ---

func TestWait_FreshWorkerReadyAfter1sReturnsUnder2s(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "")
	go func() {
		time.Sleep(time.Second)
		e.event(s.Run, "sid-1", runstore.KindSessionStart)
	}()
	start := time.Now()
	// The production poll (Poll zero): at most 2 Hz still returns in time.
	res, err := Wait(context.Background(), e.deps, "w1", WaitOptions{Timeout: 10 * time.Second})
	if err != nil || res.Outcome != OutcomeReady || time.Since(start) >= 2*time.Second || time.Since(start) < time.Second {
		t.Fatalf("%+v err %v after %s", res, err, time.Since(start))
	}
}

func TestWait_NeverReadyTimesOut124(t *testing.T) {
	e := newEnv(t)
	e.unbound("w1", "")
	if res := e.wait("w1", 80*time.Millisecond); res.Outcome != OutcomeTimeout || res.ExitCode() != 124 {
		t.Fatalf("%+v exit %d", res, res.ExitCode())
	}
	if (WaitResult{Outcome: OutcomeReady}).ExitCode() != 0 {
		t.Fatal("ready is not exit 0")
	}
}

// --- States (D2) ---

func TestWait_IdleBackgroundShellIsNotDone(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindToolStart, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "Bash", "tu-1" })
	e.event(s.Run, "sid-1", runstore.KindStop)
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleShell {
		t.Fatalf("%+v", res)
	}
	e.event(s.Run, "sid-1", runstore.KindToolEnd, func(ev *runstore.Event) { ev.Tool, ev.ToolUseID = "Bash", "tu-1" })
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleNoResult {
		t.Fatalf("shell ended: %+v", res)
	}
}

func TestSendAndWait_SharePredicate(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	check := func(want State) {
		t.Helper()
		rows, _ := List(context.Background(), e.deps, false)
		if rows[0].State != want {
			t.Fatalf("list %s, want %s", rows[0].State, want)
		}
		res := e.wait("w1", 40*time.Millisecond)
		settled := res.Outcome != OutcomeTimeout
		if settled && res.Outcome != string(want) {
			t.Fatalf("wait %s but list %s", res.Outcome, want)
		}
		_, err := Send(context.Background(), e.deps, "w1", "probe")
		if (err == nil) != want.AcceptsInput() {
			t.Fatalf("send err %v but %s accepts=%v", err, want, want.AcceptsInput())
		}
	}
	e.event(s.Run, "sid-1", runstore.KindPrompt) // a person typed a prompt
	check(StateWorking)
	e.event(s.Run, "sid-1", runstore.KindStop)
	check(StateReady) // sends "probe": a task is now owed
	e.startTurn(s, "sid-1")
	check(StateWorking)
	e.event(s.Run, "sid-1", runstore.KindStop)
	check(StateIdleNoResult)
}

// --- Suggestion (D8) ---

const greyLine = "> " + dim + "Try \"fix the tests\"" + reset + "\n"

func TestWait_GreySuggestionAfterStopReturnsReady(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	e.event(s.Run, "sid-1", runstore.KindPrompt)
	e.event(s.Run, "sid-1", runstore.KindStop)
	e.herdr.pane(PaneOf(s)).styled = greyLine
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeReady {
		t.Fatalf("%+v", res)
	}
	if _, err := Send(context.Background(), e.deps, "w1", "next"); err != nil {
		t.Fatalf("a grey suggestion blocked send: %v", err)
	}
}

func TestWait_GreySuggestionWithResultFileReturnsResult(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.herdr.pane(PaneOf(s)).styled = greyLine
	e.writeResult("w1", e.task("w1"), "answer")
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeDone {
		t.Fatalf("%+v", res)
	}
}

func TestWait_GreySuggestionNoStopNoResultTimesOut(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.herdr.pane(PaneOf(s)).styled = greyLine
	if res := e.wait("w1", 80*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("%+v", res)
	}
}

// --- Do not copy (F-1) ---

func snapshot(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.Walk(dir, func(p string, fi os.FileInfo, err error) error {
		if err == nil {
			out[p] = fmt.Sprintf("%s %d %s", fi.Mode(), fi.Size(), fi.ModTime().Format(time.RFC3339Nano))
		}
		return nil
	})
	return out
}

func TestWait_NeverWritesRunState(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.herdr.agents = append(e.herdr.agents, herdr.Agent{Pane: PaneOf(s), TerminalID: "term-x"}) // list would adopt
	check := func(timeout time.Duration) {
		t.Helper()
		before := snapshot(t, e.deps.Store.Root())
		e.wait("w1", timeout)
		if after := snapshot(t, e.deps.Store.Root()); fmt.Sprint(before) != fmt.Sprint(after) {
			t.Fatalf("wait wrote run state:\nbefore %v\nafter  %v", before, after)
		}
	}
	e.startTurn(s, "sid-1")
	check(40 * time.Millisecond) // timeout
	e.event(s.Run, "sid-1", runstore.KindStop)
	check(time.Second) // idle_no_result
	e.writeResult("w1", e.task("w1"), "x")
	check(time.Second) // done
	if hasCall(e.herdr.Calls(), "agent rename") {
		t.Fatal("wait renamed the agent")
	}
	// Other side: list, which may write run state, does adopt here.
	_, _ = List(context.Background(), e.deps, false)
	if !hasCall(e.herdr.Calls(), "agent rename") {
		t.Fatal("the fixture has nothing to adopt")
	}
}

func TestWait_AfterStopStaysStopped(t *testing.T) {
	e := newEnv(t)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.setAlive(4242, "")
	stop(t, e, "w1")
	for i := 0; i < 2; i++ {
		if res := e.wait("w1", time.Second); res.Outcome != OutcomeStopped {
			t.Fatalf("%+v", res)
		}
	}
	if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndStop {
		t.Fatalf("row %+v", r)
	}
}

func TestWait_LateLedgerWriteAfterStopLeavesRowClosed(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	e.event(s.Run, "sid-2", runstore.KindSessionStart)
	if _, err := e.deps.Ledger.CloseIfOpen(s.Run, delegationledger.CloseInfo{Reason: delegationledger.EndStop}); err != nil {
		t.Fatal(err)
	}
	before := e.row(s.Run)
	addSessions(e.deps, s.Run, []string{"sid-1", "sid-2"})
	closeLostRun(e.deps, s.Run, []string{"sid-1", "sid-2"})
	after := e.row(s.Run)
	if after.IsOpen() || after.EndReason != delegationledger.EndStop || !slices.Equal(after.WorkerSessions, before.WorkerSessions) {
		t.Fatalf("a late wait write changed a stopped row: %+v -> %+v", before, after)
	}
}

func TestWait_LedgerWriteOnOpenRowApplies(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	addSessions(e.deps, s.Run, []string{"sid-1", "sid-2"})
	if r := e.row(s.Run); !r.IsOpen() || !slices.Equal(r.WorkerSessions, []string{"sid-1", "sid-2"}) {
		t.Fatalf("%+v", r)
	}
	closeLostRun(e.deps, s.Run, []string{"sid-1"})
	if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
		t.Fatalf("%+v", r)
	}
}

// --- Claims ---

func (e *env) claims() *claimledger.Store {
	if e.deps.Claims == nil {
		e.deps.Claims = claimledger.NewStore(filepath.Join(e.dir, ".wipnote"))
	}
	return e.deps.Claims
}

func (e *env) claim(sid string) {
	e.t.Helper()
	ep := claimledger.Episode{WorkItemID: "feat-0a1b2c3d", SessionID: sid, AgentID: "__root__", StartedAt: time.Now().UTC()}
	if _, _, err := e.claims().Open(sid, ep); err != nil {
		e.t.Fatal(err)
	}
}

func (e *env) openEpisodes(sid string) int {
	e.t.Helper()
	eps, err := e.claims().ReadShard(sid)
	if err != nil {
		e.t.Fatal(err)
	}
	n := 0
	for _, ep := range eps {
		if ep.IsOpen() {
			n++
		}
	}
	return n
}

func TestWait_GoneExpiresOnlyWorker(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.claim("sid-1")
	e.claim("sess-caller")
	e.startTurn(s, "sid-1")
	e.herdr.pane(PaneOf(s)).gone = true
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeGone {
		t.Fatalf("%+v", res)
	}
	if e.openEpisodes("sid-1") != 0 || e.openEpisodes("sess-caller") != 1 {
		t.Fatalf("worker open %d, orchestrator open %d", e.openEpisodes("sid-1"), e.openEpisodes("sess-caller"))
	}
	eps, _ := e.claims().ReadShard("sid-1")
	if eps[0].Outcome != claimledger.OutcomeExpired {
		t.Fatalf("%+v", eps[0])
	}
	if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
		t.Fatalf("%+v", r)
	}
}

func TestWait_AddsClearedSessionToWorkerSessions(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "")
	e.event(s.Run, "sid-1", runstore.KindPrompt)
	e.event(s.Run, "sid-1", runstore.KindStop)
	e.event(s.Run, "sid-2", runstore.KindSessionStart) // /clear
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeReady {
		t.Fatalf("%+v", res)
	}
	if r := e.row(s.Run); !slices.Equal(r.WorkerSessions, []string{"sid-1", "sid-2"}) {
		t.Fatalf("%+v", r)
	}
}

func TestWait_TimeoutLeavesClaims(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.claim("sid-1")
	e.claim("sess-caller")
	e.startTurn(s, "sid-1")
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("%+v", res)
	}
	if e.openEpisodes("sid-1") != 1 || e.openEpisodes("sess-caller") != 1 || !e.row(s.Run).IsOpen() {
		t.Fatal("a timeout changed a claim or the row")
	}
}

// --- Review fixes ---

// A worker held at a folder-trust dialog never acks its first task; wait
// reports the dialog instead of timing out behind the unacked-task guard.
func TestWait_TrustDialogBeforeAckReturnsBlocked(t *testing.T) {
	e := newEnv(t)
	s := e.unbound("w1", "brief")
	e.herdr.pane(PaneOf(s)).screen = "plain output\n"
	if res := e.wait("w1", 60*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("no dialog: %+v", res)
	}
	e.herdr.pane(PaneOf(s)).screen = "❯ No, exit\n  Yes, I trust this folder\n"
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeBlocked || res.Detail == "" {
		t.Fatalf("%+v", res)
	}
}

// The prompt hook runs (prompt event, then ack) while wait is looking: an
// older turn's Stop must never settle the new task.
func TestWait_AckRacingObserveNeverSettlesOnOlderStop(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	e.send("w1", "second")
	fired := false
	e.herdr.onGet = func() {
		if !fired {
			fired = true
			e.startTurn(s, "sid-1") // the hook: prompt event, then the ack
		}
	}
	if res := e.wait("w1", 80*time.Millisecond); res.Outcome != OutcomeTimeout {
		t.Fatalf("an older stop settled the running task: %+v", res)
	}
	e.event(s.Run, "sid-1", runstore.KindStop) // other side: its own stop settles
	if res := e.wait("w1", time.Second); res.Outcome != OutcomeIdleNoResult {
		t.Fatalf("%+v", res)
	}
}

// Slow herdr calls are bounded by the wait's deadline.
func TestWait_DeadlineBoundsHerdrCalls(t *testing.T) {
	e := newEnv(t)
	s := e.worker("w1", "brief")
	e.startTurn(s, "sid-1")
	e.herdr.getDelay = 300 * time.Millisecond
	start := time.Now()
	res := e.wait("w1", 20*time.Millisecond)
	if res.Outcome != OutcomeTimeout || res.ExitCode() != TimeoutExit || time.Since(start) > 150*time.Millisecond {
		t.Fatalf("%+v after %s", res, time.Since(start))
	}
	// Other side: a deadline long enough lets the slow look settle.
	e.event(s.Run, "sid-1", runstore.KindStop)
	if res := e.wait("w1", 2*time.Second); res.Outcome != OutcomeIdleNoResult {
		t.Fatalf("%+v", res)
	}
}

// A harness that exited in a live pane is worker loss, acked task or not:
// wait returns gone at once, closes the row as gone and expires only the
// worker's claims (as for a lost pane).
func TestWait_ExitedHarnessIsWorkerLoss(t *testing.T) {
	for _, acked := range []bool{false, true} {
		e := newEnv(t)
		s := e.worker("w1", "brief")
		e.claim("sid-1")
		e.claim("sess-caller")
		if acked {
			e.startTurn(s, "sid-1")
		}
		if res := e.wait("w1", 40*time.Millisecond); res.Outcome != OutcomeTimeout || !e.row(s.Run).IsOpen() {
			t.Fatalf("acked=%v live harness: %+v", acked, res)
		}
		e.herdr.pane(PaneOf(s)).fg = nil // only the shell is left
		res := e.wait("w1", 5*time.Second)
		if res.Outcome != OutcomeGone {
			t.Fatalf("acked=%v: %+v", acked, res)
		}
		if r := e.row(s.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
			t.Fatalf("acked=%v row %+v", acked, r)
		}
		if e.openEpisodes("sid-1") != 0 || e.openEpisodes("sess-caller") != 1 {
			t.Fatalf("acked=%v claims: worker %d orchestrator %d", acked, e.openEpisodes("sid-1"), e.openEpisodes("sess-caller"))
		}
	}
}
