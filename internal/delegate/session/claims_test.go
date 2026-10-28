package session

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/shakestzd/wipnote/core/claimledger"
	"github.com/shakestzd/wipnote/core/delegationledger"
	"github.com/shakestzd/wipnote/core/hooks"
	"github.com/shakestzd/wipnote/internal/delegate/herdr"
	"github.com/shakestzd/wipnote/internal/delegate/runstore"
)

// Slice 16: the worker claims its item, one live worker per item, and a lost
// worker's episodes close as expired. The claim gate itself is tested in
// cmd/wipnote/claim_ledger_delegate_test.go.

// withClaims wires the expired close to the real claim ledger in the env's
// temp project.
func withClaims(e *env) *claimledger.Store {
	wipnoteDir := filepath.Join(e.dir, ".wipnote")
	e.deps.ExpireClaims = func(sid string) {
		_, _ = hooks.CloseClaimEpisodesForSession(wipnoteDir, sid, claimledger.OutcomeExpired)
	}
	return claimledger.NewStore(wipnoteDir)
}

// claim opens an episode for sid on item, rooted at sid, as `feature start`
// does for a root session.
func claim(t *testing.T, s *claimledger.Store, sid, item string) {
	t.Helper()
	if _, _, err := s.Open(sid, claimledger.Episode{WorkItemID: item, SessionID: sid, RootSessionID: sid,
		AgentID: "__root__", StartedAt: time.Now().UTC().Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
}

// episodeOf returns sid's newest episode on item.
func episodeOf(t *testing.T, s *claimledger.Store, sid, item string) claimledger.Episode {
	t.Helper()
	eps, err := s.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	for i := len(eps) - 1; i >= 0; i-- {
		if eps[i].SessionID == sid && eps[i].WorkItemID == item {
			return eps[i]
		}
	}
	t.Fatalf("no episode for %s on %s", sid, item)
	return claimledger.Episode{}
}

func TestSpawn_WorkerClaimsItem(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	brief, _, err := e.runFolder(s.Run).ReadFrom(runstore.FileBrief, 0)
	if err != nil {
		t.Fatal(err)
	}
	first, _, _ := strings.Cut(string(brief), "\n")
	if !strings.Contains(first, "wipnote feature start feat-0a1b2c3d") {
		t.Fatalf("brief must begin with the claim, first line %q", first)
	}
	// Other side: the kind follows the item's prefix.
	o := e.opts("b1")
	o.WorkItem = "bug-0a1b2c3d"
	b, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	brief, _, _ = e.runFolder(b.Run).ReadFrom(runstore.FileBrief, 0)
	if first, _, _ := strings.Cut(string(brief), "\n"); !strings.Contains(first, "wipnote bug start bug-0a1b2c3d") ||
		strings.Contains(first, "feature start") {
		t.Fatalf("bug brief first line %q", first)
	}
}

func TestSpawn_RefusesSecondLiveWorkerOnItem(t *testing.T) {
	e := newEnv(t)
	a := e.spawn("w1")
	o := e.opts("w2")
	o.WorkItem = "feat-0a1b2c3d"
	before := len(e.herdr.Calls())
	_, err := Spawn(context.Background(), e.deps, o)
	if !errors.Is(err, ErrLiveWorker) || !strings.Contains(err.Error(), a.Run) {
		t.Fatalf("err %v", err)
	}
	if rows, _ := e.deps.Ledger.ReadAll(); len(rows) != 1 {
		t.Fatalf("refusal wrote a row: %+v", rows)
	}
	for _, c := range e.herdr.Calls()[before:] { // the live check observes, read-only
		if !strings.HasPrefix(c, "process-info") && !strings.HasPrefix(c, "pane read") {
			t.Fatalf("refused, but herdr was asked to %q", c)
		}
	}
	// Other side: another item is free.
	o.WorkItem = "feat-0a1b2c3e"
	if _, err := Spawn(context.Background(), e.deps, o); err != nil {
		t.Fatalf("another item: %v", err)
	}
}

func TestSpawn_AllowsAfterWorkerPaneGone(t *testing.T) {
	e := newEnv(t)
	claims := withClaims(e)
	a := e.spawn("w1")
	e.event(a.Run, "sid-w1", runstore.KindSessionStart)
	claim(t, claims, "sid-w1", "feat-0a1b2c3d")
	claim(t, claims, "sess-caller", "feat-0a1b2c3d")
	_ = e.herdr.Close(context.Background(), PaneOf(a))

	o := e.opts("w2")
	o.WorkItem = "feat-0a1b2c3d"
	b, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatalf("pane gone, spawn refused: %v", err)
	}
	if r := e.row(a.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone || b.Run == a.Run {
		t.Fatalf("lost row %+v", r)
	}
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); ep.IsOpen() || ep.Outcome != claimledger.OutcomeExpired {
		t.Fatalf("lost worker's episode %+v, want expired", ep)
	}
	// Other side: the orchestrator's episode is untouched.
	if ep := episodeOf(t, claims, "sess-caller", "feat-0a1b2c3d"); !ep.IsOpen() {
		t.Fatalf("orchestrator episode closed: %+v", ep)
	}
}

func TestSpawn_OneShotRunsDoNotCount(t *testing.T) {
	e := newEnv(t)
	if _, err := e.deps.Ledger.Open(delegationledger.Record{Mode: delegationledger.ModeOneshot, Harness: "codex",
		WorkItem: "feat-0a1b2c3d", CallerSession: "sess-caller", Started: time.Now().UTC()}); err != nil {
		t.Fatal(err)
	}
	if s := e.spawn("w1"); s.Status != StatusStarting {
		t.Fatalf("%+v", s)
	}
	// Other side: an interactive row on the item does count.
	o := e.opts("w2")
	o.WorkItem = "feat-0a1b2c3d"
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrLiveWorker) {
		t.Fatalf("err %v", err)
	}
}

func TestPaneLoss_ExpiresOnlyWorkerEpisode(t *testing.T) {
	e := newEnv(t)
	claims := withClaims(e)
	a := e.spawn("w1")
	b := e.spawn("w2")
	e.event(a.Run, "sid-w1", runstore.KindSessionStart)
	e.event(b.Run, "sid-w2", runstore.KindSessionStart)
	claim(t, claims, "sess-caller", "feat-0a1b2c3d")
	claim(t, claims, "sid-w1", "feat-0a1b2c3d")
	claim(t, claims, "sid-w2", "feat-0a1b2c3dw2")

	// A live pane: list changes no claim.
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if !episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d").IsOpen() {
		t.Fatal("list expired a live worker's episode")
	}

	_ = e.herdr.Close(context.Background(), PaneOf(a))
	rows, err := List(context.Background(), e.deps, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if r.Run == a.Run && (r.State != StateGone || r.Open) {
			t.Fatalf("lost row listed %+v", r)
		}
	}
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); ep.Outcome != claimledger.OutcomeExpired {
		t.Fatalf("lost worker episode %+v", ep)
	}
	if !episodeOf(t, claims, "sess-caller", "feat-0a1b2c3d").IsOpen() || !episodeOf(t, claims, "sid-w2", "feat-0a1b2c3dw2").IsOpen() {
		t.Fatal("pane loss closed an episode that was not the lost worker's")
	}
}

func TestPaneLoss_StopOfGonePaneExpires(t *testing.T) {
	e := newEnv(t)
	claims := withClaims(e)
	s := e.spawn("w1")
	e.event(s.Run, "sid-w1", runstore.KindSessionStart)
	claim(t, claims, "sid-w1", "feat-0a1b2c3d")
	_ = e.herdr.Close(context.Background(), PaneOf(s))
	res, err := Stop(context.Background(), e.deps, "w1", fastStop())
	if err != nil || !strings.Contains(res.Why, "gone") {
		t.Fatalf("stop %+v err %v", res, err)
	}
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); ep.Outcome != claimledger.OutcomeExpired {
		t.Fatalf("episode %+v", ep)
	}
}

func TestStop_WorkerEpisodeAbandonedBySessionEnd(t *testing.T) {
	e := newEnv(t)
	claims := withClaims(e)
	e.herdr.exitOnEnter = true
	s := runningWorker(t, e, "w1")
	e.event(s.Run, "sid-w1", runstore.KindSessionStart)
	claim(t, claims, "sid-w1", "feat-0a1b2c3d")
	e.setAlive(4242, "")
	if res := stop(t, e, "w1"); !res.PaneClosed {
		t.Fatalf("stop %+v", res)
	}
	// The caller never writes the worker's shard on a stop: the worker's own
	// SessionEnd does, as abandoned.
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); !ep.IsOpen() {
		t.Fatalf("stop closed the worker's episode itself: %+v", ep)
	}
	if r := e.row(s.Run); r.EndReason != delegationledger.EndStop {
		t.Fatalf("row %+v", r)
	}
	if _, err := hooks.CloseClaimEpisodesForSession(filepath.Join(e.dir, ".wipnote"), "sid-w1", claimledger.OutcomeAbandoned); err != nil {
		t.Fatal(err)
	}
	if ep := episodeOf(t, claims, "sid-w1", "feat-0a1b2c3d"); ep.Outcome != claimledger.OutcomeAbandoned {
		t.Fatalf("episode %+v", ep)
	}
}

// workerPanes returns the panes in the caller's workers record.
func workerPanes(t *testing.T, e *env) []herdr.PaneID {
	t.Helper()
	var ids []herdr.PaneID
	if err := withWorkers(e.deps.Store, "w1:p1", func(w *Workers) error {
		ids = w.paneIDs()
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return ids
}

func hasPane(ids []herdr.PaneID, p herdr.PaneID) bool {
	for _, id := range ids {
		if id == p {
			return true
		}
	}
	return false
}

// pin records the pane's current terminal as the worker's, as adopt does.
func pin(t *testing.T, e *env, s Spawned) {
	t.Helper()
	rf := e.runFolder(s.Run)
	info, err := ReadInfo(rf)
	if err != nil {
		t.Fatal(err)
	}
	info.TerminalID, info.Renamed = e.herdr.pane(PaneOf(s)).terminal, true
	if err := WriteInfo(rf, info); err != nil {
		t.Fatal(err)
	}
}

// A lost worker's run folder finishes, so readers stop waiting for its exit
// and the sweep can age it out.
func TestPaneLoss_ListFinishesRunFolder(t *testing.T) {
	e := newEnv(t)
	s := e.spawn("w1")
	// Other side: a live pane leaves the run unfinished.
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if ri, _ := e.runFolder(s.Run).Read(); ri.Finished {
		t.Fatalf("list finished a live worker's run: %+v", ri)
	}
	_ = e.herdr.Close(context.Background(), PaneOf(s))
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	if ri, _ := e.runFolder(s.Run).Read(); !ri.Finished || ri.End.IsZero() {
		t.Fatalf("lost worker's run not finished: %+v", ri)
	}
}

// A pinned pane that now holds another terminal leaves the caller's workers
// record, and the reused terminal is not touched.
func TestPaneLoss_ListForgetsReusedPane(t *testing.T) {
	e := newEnv(t)
	a := e.spawn("w1")
	b := e.spawn("w2")
	pin(t, e, a)
	pin(t, e, b)
	e.herdr.pane(PaneOf(a)).terminal = "term-someone-else"
	mark := len(e.herdr.Calls())
	if _, err := List(context.Background(), e.deps, false); err != nil {
		t.Fatal(err)
	}
	for _, c := range e.herdr.Calls()[mark:] {
		if strings.HasPrefix(c, "send-") || strings.HasPrefix(c, "close ") || strings.HasPrefix(c, "tab close") {
			t.Fatalf("list touched a reused pane: %s", c)
		}
	}
	panes := workerPanes(t, e)
	if hasPane(panes, PaneOf(a)) {
		t.Fatalf("reused pane %s still in the workers record %v", PaneOf(a), panes)
	}
	if r := e.row(a.Run); r.IsOpen() || r.EndReason != delegationledger.EndGone {
		t.Fatalf("row %+v", r)
	}
	if ri, _ := e.runFolder(a.Run).Read(); !ri.Finished {
		t.Fatalf("reused worker's run not finished: %+v", ri)
	}
	// Other side: the worker on its own terminal stays placed and open.
	if !hasPane(panes, PaneOf(b)) || !e.row(b.Run).IsOpen() {
		t.Fatalf("live worker %s dropped: %v", PaneOf(b), panes)
	}
}

// The spawn path loses a reused worker the same way: its run leaves the
// workers record (never split) and its run folder finishes.
func TestSpawn_LosesReusedWorkerOnItem(t *testing.T) {
	e := newEnv(t)
	a := e.spawn("w1")
	pin(t, e, a)
	e.herdr.pane(PaneOf(a)).terminal = "term-someone-else"
	o := e.opts("w2")
	o.WorkItem = "feat-0a1b2c3d"
	b, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatalf("reused pane, spawn refused: %v", err)
	}
	if hasCall(e.herdr.Calls(), "split "+string(PaneOf(a))) {
		t.Fatalf("spawn split the reused pane: %v", e.herdr.Calls())
	}
	panes := workerPanes(t, e)
	if hasPane(panes, PaneOf(a)) || !hasPane(panes, PaneOf(b)) {
		t.Fatalf("workers record %v, want only %s", panes, PaneOf(b))
	}
	if ri, _ := e.runFolder(a.Run).Read(); !ri.Finished {
		t.Fatalf("lost run not finished: %+v", ri)
	}
	// Other side: the new worker's run is live.
	if ri, _ := e.runFolder(b.Run).Read(); ri.Finished {
		t.Fatalf("new run finished: %+v", ri)
	}
}

// A worker spawned with no brief gets its first task through send, so the
// sent task carries the claim of the worker's own item.
func TestSend_NoBriefFirstTaskCarriesClaim(t *testing.T) {
	e := newEnv(t)
	e.worker("w1", "")
	e.send("w1", "Count the files.")
	msgs, _ := e.mailbox("w1").Read(runstore.Inbox)
	if len(msgs) != 1 || !strings.HasPrefix(msgs[0].Body, ClaimLine("feat-0a1b2c3d")) {
		t.Fatalf("first sent task lacks the claim: %+v", msgs)
	}
	// Other side: the claim names this worker's item and kind, not another's.
	o := e.opts("b1")
	o.WorkItem, o.Brief = "bug-0a1b2c3d", ""
	b, err := Spawn(context.Background(), e.deps, o)
	if err != nil {
		t.Fatal(err)
	}
	e.herdr.pane(PaneOf(b)).styled = "> \n"
	e.event(b.Run, "sid-b1", runstore.KindSessionStart)
	e.send("b1", "Fix it.")
	bm, _ := e.mailbox("b1").Read(runstore.Inbox)
	if len(bm) != 1 || !strings.HasPrefix(bm[0].Body, ClaimLine("bug-0a1b2c3d")) || strings.Contains(bm[0].Body, "feat-0a1b2c3d") {
		t.Fatalf("bug worker's task %q", bm[0].Body)
	}
}

// After /clear the old session's SessionEnd closed its claim; the next sent
// task carries the claim again, so the new session reclaims the item.
func TestSend_AfterClosedClaimCarriesClaim(t *testing.T) {
	e := newEnv(t)
	claims := withClaims(e)
	s := e.worker("w1", "Do the thing.")
	claim(t, claims, "sid-1", "feat-0a1b2c3d")
	e.startTurn(s, "sid-1")
	e.event(s.Run, "sid-1", runstore.KindStop)
	if _, err := hooks.CloseClaimEpisodesForSession(filepath.Join(e.dir, ".wipnote"), "sid-1", claimledger.OutcomeAbandoned); err != nil {
		t.Fatal(err)
	}
	e.event(s.Run, "sid-2", runstore.KindSessionStart)
	sent := e.send("w1", "Next task.")
	msgs, _ := e.mailbox("w1").Read(runstore.Inbox)
	last := msgs[len(msgs)-1]
	if last.ID != sent.Task || !strings.HasPrefix(last.Body, ClaimLine("feat-0a1b2c3d")) {
		t.Fatalf("task after a closed claim lacks the claim: %q", last.Body)
	}
	// Other side: the claim is said once, before the result preamble and task.
	if n := strings.Count(last.Body, "wipnote feature start"); n != 1 || !strings.HasSuffix(strings.TrimSpace(last.Body), "Next task.") {
		t.Fatalf("claim said %d times in %q", n, last.Body)
	}
}

// Spawn resolves the work item through Deps.ResolveWorkItem before the
// one-worker-per-item check, and after a resume has defaulted it to the old
// run's item.
func TestSpawn_ResolvesWorkItemAfterResumeDefault(t *testing.T) {
	e := newEnv(t)
	stoppedWorker(t, e, e.opts("w1"), "sid-a")
	var saw []string
	e.deps.ResolveWorkItem = func(id string) (string, error) {
		saw = append(saw, id)
		if id == "feat-0a1b" {
			return "feat-0a1b2c3d", nil
		}
		return id, nil
	}
	if _, info := resume(t, e, e.resumeOpts("w2"), "w1"); info.WorkItem != "feat-0a1b2c3d" ||
		!slices.Contains(saw, "feat-0a1b2c3d") {
		t.Fatalf("resume item %q, resolver saw %v: the old run's item was not resolved", info.WorkItem, saw)
	}
	// A prefix resolves before the live-worker check: w2 holds the item.
	o := e.opts("w3")
	o.WorkItem = "feat-0a1b"
	if _, err := Spawn(context.Background(), e.deps, o); !errors.Is(err, ErrLiveWorker) {
		t.Fatalf("prefix of a held item: err %v, want the live-worker refusal", err)
	}
	// Other side: a resolver refusal spawns nothing.
	e.deps.ResolveWorkItem = func(id string) (string, error) { return "", errors.New("no such item") }
	before, _ := e.deps.Ledger.ReadAll()
	o = e.opts("w4")
	o.WorkItem = "feat-ffff"
	if _, err := Spawn(context.Background(), e.deps, o); err == nil || !strings.Contains(err.Error(), "no such item") {
		t.Fatalf("err %v", err)
	}
	if after, _ := e.deps.Ledger.ReadAll(); len(after) != len(before) {
		t.Fatal("a refused item wrote a row")
	}
}
